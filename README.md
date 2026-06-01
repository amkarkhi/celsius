# Celsius

> **A CEL-driven rule engine and request-override middleware for Go.**
>
> *Define rules in YAML, evaluate them at request time with Google's [Common Expression Language](https://github.com/google/cel-spec), attach a typed result to the request context, and let your handlers branch on it.*

[![Go Reference](https://pkg.go.dev/badge/github.com/amkarkhi/celsius.svg)](https://pkg.go.dev/github.com/amkarkhi/celsius)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

> ⚠️ **Status — v0.x.** API is stabilizing. Expect breaking changes until v1.0.

> 🔥 **Battle-tested at the largest e-commerce platform in the Middle East.**

---

## What is it?

Celsius lets you push *runtime decisions* — A/B splits, feature flags, response mocks, canary routing, rate-shaping, tenant overrides — out of your handler code and into a hot-reloadable YAML file. Rules are expressed in [CEL](https://github.com/google/cel-spec), so they are sandboxed, type-checked, and side-effect free.

A typical request flow:

```
HTTP request ──▶ middleware
                  │
                  │ 1. build inputs (headers, claims, anything)
                  │ 2. evaluate the named rule group
                  │ 3. attach matched Rule[T] to ctx (if any)
                  ▼
              your handler  ──▶  celsius.ResultFrom(ctx) → branch on Rule.Out
```

Use it for whatever fits a rule-engine shape:

- **A/B testing & gradual rollouts** — bucket users by `hash(uid) % 100 < 10`.
- **Feature flags** — turn behavior on by tenant, platform, environment.
- **Response overrides / mocking** — return canned payloads when an upstream is down.
- **Routing** — pick which backend to call based on request attributes.
- **Tenant overrides** — let SaaS customers customize per-tenant behavior without redeploys.

## Why CEL?

- **Safe:** no I/O, no loops, no `eval` — expressions are pure functions.
- **Fast:** compiled once at load time, evaluated as bytecode.
- **Familiar:** C-like syntax (`uid > 100 && platform == "ios"`).
- **Typed:** compile errors surface on hot-reload, not at request time.

## Install

```sh
go get github.com/amkarkhi/celsius
```

The **core package** has no web-framework dependencies. Pick the transport adapter you need:

```sh
go get github.com/amkarkhi/celsius/transport/nethttp   # net/http
go get github.com/amkarkhi/celsius/transport/fiber     # Fiber v2
go get github.com/amkarkhi/celsius/transport/gin       # Gin
```

## Quick start

### 1. Define rules in YAML

```yaml
# rules.yaml
rules:
  checkout_experiment:
    - script: 'hash(uid, 100) < 10'
      tag:    "variant_b"
      out:
        backend: "checkout-v2"
        banner:  "Try our new checkout!"
    - script: 'true'
      tag:    "control"
      out:
        backend: "checkout-v1"
        banner:  ""
variables: {}
```

Rules are evaluated **top-down, first match wins**. The last `script: 'true'` is the conventional default branch.

### 2. Describe your typed output

```go
type CheckoutOut struct {
    Backend string `mapstructure:"backend"`
    Banner  string `mapstructure:"banner"`
}
```

### 3. Describe your inputs

Inputs implement the `celsius.Input` interface — they tell Celsius how to turn the incoming request into the variable map that CEL will see.

```go
type CheckoutInput struct {
    Uid      int
    Platform string
}

func (i *CheckoutInput) Map() map[string]any {
    return map[string]any{"uid": i.Uid, "platform": i.Platform}
}

func (i *CheckoutInput) Parse(ctx celsius.Ctx) (map[string]any, error) {
    uid, _ := strconv.Atoi(ctx.Header("X-UID", "-1"))
    i.Uid = uid
    i.Platform = ctx.Header("X-Platform", "unknown")
    return i.Map(), nil
}
```

### 4. Build the engine

```go
import (
    "github.com/amkarkhi/celsius"
    "github.com/google/cel-go/cel"
)

engine, err := celsius.New[CheckoutOut](celsius.Config{
    Source: celsius.FileSource("rules.yaml"),
    Env: celsius.DefaultEnv().With(
        celsius.Variable("uid",      cel.IntType),
        celsius.Variable("platform", cel.StringType),
    ),
    Input:  &CheckoutInput{},
    Logger: &log.Logger,
})
if err != nil { panic(err) }
```

### 5. Wire it as middleware

#### net/http

```go
import celsiushttp "github.com/amkarkhi/celsius/transport/nethttp"

mux := http.NewServeMux()
mux.Handle("/checkout", celsiushttp.Middleware(engine, "checkout_experiment")(handler))
```

#### Gin

```go
import celsiusgin "github.com/amkarkhi/celsius/transport/gin"

r := gin.Default()
r.Use(celsiusgin.Middleware(engine, "checkout_experiment"))
r.GET("/checkout", func(c *gin.Context) {
    rule, ok := celsius.ResultFrom[CheckoutOut](c.Request.Context())
    if !ok {
        c.String(200, "no rule matched")
        return
    }
    c.String(200, "backend=%s", rule.Out.Backend)
})
```

#### Fiber

```go
import celsiusfiber "github.com/amkarkhi/celsius/transport/fiber"

app := fiber.New()
app.Use(celsiusfiber.Middleware(engine, "checkout_experiment"))
app.Get("/checkout", func(c *fiber.Ctx) error {
    rule, ok := celsius.ResultFrom[CheckoutOut](c.UserContext())
    if !ok {
        return c.SendString("no rule matched")
    }
    return c.SendString("backend=" + rule.Out.Backend)
})
```

### 6. Evaluate directly (no middleware)

The engine is usable as a plain library outside of HTTP:

```go
rule, err := engine.Match("checkout_experiment", map[string]any{
    "uid":      42,
    "platform": "ios",
})
```

## Rule file reference

```yaml
variables:                       # optional — `var.NAME` substitution in scripts
  internal_uids: "[1, 2, 3]"

rules:
  rule_group_name:               # the "key" passed to the middleware
    - script: "uid in var.internal_uids"
      tag:    "internal_user"    # free-form label, surfaced on Rule.Tag
      out:                       # decoded into your Rule[T].Out
        message: "hi, employee"
    - script: "true"
      tag:    "default"
      out:
        message: "hi, guest"
```

- **`script`** — CEL expression that must evaluate to `bool`.
- **`tag`** — opaque label your handler can read off `rule.Tag`.
- **`out`** — arbitrary payload, decoded into your `T` via [`mapstructure`](https://pkg.go.dev/github.com/go-viper/mapstructure/v2).
- **`var.NAME`** in any `script` is substituted with the matching entry from the top-level `variables` block. Cycles are detected and reported.

## Built-in CEL helpers

| Function     | Signature                                                  | Example                          |
|--------------|------------------------------------------------------------|----------------------------------|
| `hash(s, n)` | `(string\|int\|uint, int) → int`                           | `hash(uid, 100) < 10`            |
| `md5(s)`     | `(string) → string`                                        | `md5(guid) == "..."`             |
| `contains`   | `(string, string) → bool`                                  | `contains(platform, "ios")`      |
| `rand()`     | `() → double`                                              | `rand() < 0.05`                  |
| `replace`    | `(string, string, string) → string`                        | `replace(sid, "-", "")`          |
| `to_str(v)`  | `(any) → string`                                           | `to_str(uid)`                    |

## Extending the environment

Bring your own variables and functions:

```go
engine, _ := celsius.New[Out](celsius.Config{
    Env: celsius.DefaultEnv().
        With(celsius.Variable("region", cel.StringType)).
        WithFunction("starts_with_test", startsWithTest()),
    // ...
})
```

## Hot reload

Celsius watches the rule file via [`fsnotify`](https://github.com/fsnotify/fsnotify) and recompiles on change. Reads are guarded by an `RWMutex`, so in-flight requests during a reload see either the old set or the new set — never a half-applied state. A reload that fails to compile is logged and the previous good set stays active.

## Why "Celsius"?

**CEL** — the [Common Expression Language](https://github.com/google/cel-spec) that every rule is written in — plus **sieving**, because that's literally what the engine does: it sieves an incoming stream of requests through an ordered set of predicates and routes each one to the first rule it falls through. The fact that the result also reads as the temperature scale is a happy coincidence.

## Design notes

See [DESIGN.md](DESIGN.md) for architecture, lifecycle, threading model, and extension points.

## Validating & testing rule files

Celsius ships a small CLI for validating rule files and running ad-hoc matches without standing up a service.

> **Important:** the stock `celsius` binary only knows about [`celsius.DefaultEnv()`](env.go). If your service registers extra variables or custom functions, that binary cannot validate your real configs — it'll reject every identifier it doesn't recognize.
>
> Use it as-is for trivial configs, **or build a 5-line wrapper in your own repo** that hands [`clikit`](clikit/) your real `EnvBuilder` (see [Wrapping the CLI for your own env](#wrapping-the-cli-for-your-own-env) below). The wrapper inherits every variable and function you've registered, so `validate`, `test`, `eval`, and `repl` all behave exactly like production.

Install the stock binary with:

```sh
go install github.com/amkarkhi/celsius/cmd/celsius@latest
```

Or run from a checkout:

```sh
go run ./cmd/celsius <subcommand> ...
```

### Validate (pipeline-friendly)

```sh
# Syntax check — fast, no variable declarations needed.
celsius validate rules.yaml

# Strict mode — also type-checks every script. Declare each free identifier
# as DynType so the validator doesn't reject it.
celsius validate --strict --var uid --var platform rules.yaml
```

Exit codes: `0` clean, `1` usage error, `2` issues found — drop the command straight into a CI job.

### Test a single match

```sh
celsius test --group checkout_experiment \
  --input uid=42 --input platform=ios \
  rules.yaml
```

Output names the matched rule's tag and decoded `out` payload. Inputs are auto-typed: `42` → int, `1.5` → double, `true`/`false` → bool, anything else → string.

### Eval a single expression

Quickly try out a CEL expression — including any custom function you've registered via `WithFunction`:

```sh
celsius eval --input uid=42 'hash(to_str(uid), 10)'
```

### Wrapping the CLI for your own env

If your service registers extra variables or custom functions, drop a 5-line binary in your repo that hands `clikit` your real `EnvBuilder`:

```go
// cmd/myapp-rules/main.go
package main

import (
    "os"

    "github.com/amkarkhi/celsius/clikit"
    "myapp/internal/celenv" // your Env() lives here
)

func main() {
    os.Exit(clikit.Run(clikit.Options{
        Name: "myapp-rules",
        Env:  celenv.Env(),
    }, os.Args[1:]))
}
```

Then `myapp-rules validate --strict rules.yaml` understands your custom identifiers, and anything that passes the CLI will run in production — same compilation paths, same env.

**See [`docs/custom-env.md`](docs/custom-env.md)** for the full walkthrough: structuring the env package, typing variables properly, unit-testing custom CEL functions with `celsius.EvalExpr`, gating CI on `celsius.ValidateBytes`, and anti-patterns to avoid. A runnable end-to-end example lives at [`examples/customenv`](examples/customenv/).

### Interactive REPL

```sh
celsius repl rules.yaml
> match greeting uid=200
matched tag=vip out=map[message:hello, big spender]
> match greeting uid=5
matched tag=default out=map[message:hello]
> validate
OK
> quit
```

Programmatic access is also available via [`celsius.ValidateBytes`](validate.go) and [`celsius.MatchOnce`](validate.go) — useful for unit tests, admin endpoints, or custom tooling.

## Examples

Runnable examples live under `examples/`:

| Example | What it shows |
|---|---|
| [`examples/basic`](examples/basic/)         | Minimal `net/http` server + a single rule group. |
| [`examples/gin`](examples/gin/)             | Same flow wired through Gin. |
| [`examples/fiber`](examples/fiber/)         | Same flow wired through Fiber. |
| [`examples/customenv`](examples/customenv/) | **Custom CEL functions + variables**, with a shared `extensions` package consumed by both the server and a thin CLI wrapper. Includes a `*_test.go` showing how to unit-test custom functions with `celsius.EvalExpr` and how to validate the production rules YAML against the production env in CI. This is the pattern you'll want to copy. |

```sh
go run ./examples/basic     --rules examples/basic/rules.yaml
go run ./examples/gin       --rules examples/basic/rules.yaml
go run ./examples/fiber     --rules examples/basic/rules.yaml
go run ./examples/customenv/server --rules examples/customenv/rules.yaml
go run ./examples/customenv/cli    validate --strict examples/customenv/rules.yaml
```

Then:

```sh
curl -H 'X-UID: 200' http://localhost:8080/hello
curl -H 'X-UID: 42'  http://localhost:8080/hello   # customenv: hits is_internal()
curl -H 'X-UID: 5' -H 'X-Country: IR' http://localhost:8080/hello
```

## Contributing

Issues and PRs welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). No CLA.

## License

[MIT](LICENSE) © Amin Karkhi.
