# Celsius — Design Document

This document describes the internal architecture of Celsius. If you are just trying to **use** Celsius, read the [README](README.md) first.

## 1. Goals & non-goals

### Goals
- **Externalize runtime decisions.** Move "should this request go to backend A or B, see banner X or Y, get rate-limit Z" out of Go code and into a hot-reloadable file.
- **Safety by construction.** Use CEL so user-supplied expressions cannot crash the host, loop forever, or perform I/O.
- **Type-safe outputs.** Each rule produces a strongly-typed payload `Rule[T].Out`, decoded once at config-load time, not at request time.
- **Framework-neutral core.** The core package depends only on `cel-go`, `viper`, `fsnotify`, `zerolog`, and `mapstructure`. Web frameworks live in opt-in sub-packages.
- **Cheap evaluation.** Each rule is compiled to a `cel.Program` once on load. Request-time work is a variable map build + `Program.Eval` per rule until first match.

### Non-goals
- **Persistent storage of rules.** Celsius reads files (or any `Loader`); it does not run a database.
- **Stateful experiments.** Bucket stickiness is the caller's job (e.g., hash on `uid`). Celsius is a pure function of inputs.
- **Distributed coordination.** A multi-replica deployment with file-watching gets eventual consistency per replica. There is no broadcast/quorum step.
- **Policy / authorization.** This is a *rule engine*, not OPA. CEL is the language; we don't ship a policy bundle format or a decision-log shape.

## 2. High-level architecture

```
                ┌─────────────────────────────────────────┐
                │              Engine[T]                  │
                │                                         │
   Loader  ───▶ │  parse YAML  ─▶  compile CEL  ─▶  rules │ ◀── RWMutex
   (file,       │                                         │
   fsnotify,    │  CEL Env (variables, fns, helpers)      │
   custom)      └─────────────────────────────────────────┘
                                  ▲
                                  │ Match(key, inputs)
                                  │
   ┌──────────────┐    ┌──────────┴──────────┐    ┌──────────────┐
   │ net/http mw  │    │      gin mw         │    │   fiber mw   │
   └──────┬───────┘    └──────────┬──────────┘    └──────┬───────┘
          │                       │                      │
          └───────────────────────┴──────────────────────┘
                                  │
                          context.Value injection
                                  │
                                  ▼
                         celsius.ResultFrom[T](ctx)
```

### Components

| Layer            | Package                          | Responsibility                                                  |
|------------------|----------------------------------|-----------------------------------------------------------------|
| Engine           | `celsius`                        | Compile rules, expose `Match` / `Eval` / `Compile`.             |
| Loader           | `celsius` (interface)            | Source of rule bytes + change notifications.                    |
| CEL env          | `celsius`                        | Default variables, default functions, user extensions.          |
| Var expansion    | `internal/varexpand`             | Substitute `var.NAME` references, detect cycles.                |
| CEL helpers      | `internal/celfn`                 | `hash`, `md5`, `contains`, `rand`, `replace`, `to_str`.         |
| Transport: http  | `transport/nethttp`              | `func(Handler) Handler` middleware factory.                     |
| Transport: gin   | `transport/gin`                  | `gin.HandlerFunc` factory.                                      |
| Transport: fiber | `transport/fiber`                | `fiber.Handler` factory.                                        |

## 3. Lifecycle

1. **Construction (`New`)**
   - Validate config (`Source` and `Input` are required).
   - Build CEL env (`DefaultEnv` + user extensions).
   - Ask the `Loader` for initial bytes.
   - Parse YAML → `ConfigFile[T]` (uses `mapstructure` for `Out`).
   - Expand `var.NAME` references (with cycle detection).
   - Compile each rule's `script` to a `cel.Program`, attach to the rule.
   - Subscribe to the `Loader`'s change channel; spawn a watcher goroutine.

2. **Request evaluation (`Match`)**
   - Acquire `RLock` on engine state.
   - Look up the rule group by name. Missing key → return `ErrNoRuleGroup`.
   - Iterate rules in declaration order. For each:
     - `Program.Eval(inputs)`. If the result is `bool(true)`, return that rule.
     - Eval errors are *logged and skipped* (a runtime type error in one rule must not kill the group).
   - No match → return `ErrNoMatch`.

3. **Hot reload**
   - Watcher goroutine receives change event.
   - Re-parse + re-compile *into a new, local snapshot*.
   - If the new snapshot fails to compile, log the error and keep the current snapshot.
   - On success, take `Lock`, swap the snapshot pointer, release.
   - In-flight `Match` calls either complete on the old snapshot (they hold `RLock`) or block briefly until the swap lands.

4. **Shutdown**
   - `engine.Close()` stops the watcher goroutine and drains the file watcher.

## 4. Threading model

Mutable engine state lives behind a single `sync.RWMutex`:

```go
type snapshot[T any] struct {
    rules     map[string][]Rule[T]
    variables map[string]string
    env       *cel.Env
}

type Engine[T any] struct {
    mu   sync.RWMutex
    snap *snapshot[T]   // swapped wholesale on reload
    // ... loader, logger, etc.
}
```

- **Readers** (`Match`, `Eval`) take `RLock` for the duration of the call.
- **Writer** (reload) constructs the new snapshot *without* the lock, then briefly takes `Lock` only for the pointer swap. This keeps reload-time tail latency on readers bounded by the swap itself, not by the recompile.

Compiled `cel.Program`s are safe for concurrent use by `cel-go`'s contract, so multiple goroutines can evaluate the same rule simultaneously.

## 5. Rule model

```go
type ConfigFile[T any] struct {
    Rules     map[string][]Rule[T] `mapstructure:"rules,omitempty"`
    Variables map[string]string    `mapstructure:"variables,omitempty"`
}

type Rule[T any] struct {
    Script string       `mapstructure:"script,omitempty"`
    Tag    string       `mapstructure:"tag,omitempty"`
    Out    T            `mapstructure:"out,omitempty"`
    prog   cel.Program  // private — set during load
}
```

- `T` is whatever payload your application wants on a matched rule. It is decoded once at load time using `mapstructure`, so request-time access is just a struct read.
- `Rule[T]` exposes `Tag` so handlers can branch on a stable label without inspecting the payload.

## 6. CEL environment

`DefaultEnv()` declares the built-in helper functions (`hash`, `md5`, `contains`, `rand`, `replace`, `to_str`) and nothing else. Users add variables and functions via `Env.With(...)`. There are *no* default variables in core — those belong in presets, so users get explicit, audit-able variable lists.

```go
e := celsius.DefaultEnv().
    With(celsius.Variable("uid", cel.IntType)).
    WithFunction("starts_with_test", myFn())
```

## 7. Variable expansion

Top-level `variables:` entries are substituted into scripts before compilation:

```yaml
variables:
  staff_uids: "[1, 2, 3]"
rules:
  show_admin:
    - script: "uid in var.staff_uids"
```

Expansion runs until a fixed point is reached. A cycle (`A → B → A`) is detected and reported as a compile-time error rather than looping forever.

## 8. Transport adapters

Each adapter is a thin shim:

```go
// transport/nethttp
func Middleware[T any](e *celsius.Engine[T], key string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            inputs, _ := e.Input().Parse(celsius.NewHTTPCtx(r))
            if rule, err := e.Match(key, inputs); err == nil && rule != nil {
                r = r.WithContext(celsius.WithResult(r.Context(), rule))
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

Gin and Fiber adapters follow the same shape with framework-specific `Ctx` constructors. **All transports use the same `context.Value` key**, so `celsius.ResultFrom[T](ctx)` works uniformly downstream.

## 9. Extension points

- **`Loader` interface** — bring your own source (S3, Consul, etcd). The default `FileSource` is one implementation.
- **`Input` interface** — control how request attributes become CEL variables.
- **CEL `Env`** — add any variables and functions you want.
- **Preset packages** — wrap a recurring `(Env, Input, headers)` triple for your team.

## 10. Failure modes & error semantics

| Failure                              | Behavior                                                                  |
|--------------------------------------|---------------------------------------------------------------------------|
| Config file missing at `New`         | `New` returns an error (override with `Config.AllowMissingFile = true`).  |
| Reload fails to parse/compile        | Logged; previous good snapshot stays active.                              |
| Single rule fails to evaluate        | Logged; evaluation continues with the next rule in the group.             |
| Rule script evaluates to non-bool    | Logged; treated as "no match" for that rule.                              |
| `Match` called with unknown group    | Returns `ErrNoRuleGroup`.                                                 |
| No rule in a group matches           | Returns `ErrNoMatch`.                                                     |
| Middleware: any of the above         | Falls through to `next`. The handler sees no result; that's the signal.   |

Middleware is **never** allowed to abort a request — the design assumes the override layer is auxiliary, and a misconfigured rule should not take production down.

## 11. Open questions / future work

- **Metrics:** expose Prometheus counters for match rate, eval errors, reload count.
- **Decision log:** optional ring-buffer of recent decisions for debugging.
- **Remote loaders:** ship `S3Source`, `HTTPSource` as separate sub-packages.
- **Rule validation CLI:** `celsius validate rules.yaml` for CI.
- **Schema export:** generate JSON Schema for `T` to validate `out:` payloads at config-author time.
