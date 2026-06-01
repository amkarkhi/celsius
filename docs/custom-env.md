# Custom CEL environments — variables, functions, and validation

This guide is for teams **consuming Celsius from their own service**. It
walks through the recommended pattern: declare your CEL environment once,
share it between your server and a thin validation CLI, and unit-test your
custom functions without standing up a rule file.

If you're new to Celsius, read the [README](../README.md) first.

> **TL;DR.** Put your variables and custom functions in a small package
> (e.g. `internal/celenv`). Build `*celsius.EnvBuilder` there. Import it
> from your server (passed to `celsius.New`) **and** from a tiny CLI built
> on `clikit` (passed to `clikit.Run`). What the CLI validates is what the
> server runs.

A complete runnable example lives at
[`examples/customenv`](../examples/customenv/) — this document explains the
*why* and the *what*.

---

## 1. Why you (almost certainly) need this

The stock `celsius` binary in `cmd/celsius/` uses [`celsius.DefaultEnv()`][defaultenv]
and only knows the built-in helpers (`hash`, `md5`, `contains`, `rand`,
`replace`, `to_str`).

If your rules reference anything else — your own variables, your own
functions — running `celsius validate rules.yaml` will reject them as
unknown identifiers. The fix is not to weaken validation; it's to give
the CLI the same environment your server uses.

That environment lives in **one place**, owned by your service, and is
imported by both your runtime and your tooling.

[defaultenv]: https://pkg.go.dev/github.com/amkarkhi/celsius#DefaultEnv

## 2. The shape

```
myservice/
├── internal/celenv/
│   ├── env.go            ← one Env() function, used everywhere
│   └── env_test.go       ← unit-tests for your custom CEL fns
├── cmd/
│   ├── myservice/        ← your HTTP/gRPC server
│   │   └── main.go       → calls celsius.New(... Env: celenv.Env() ...)
│   └── myservice-rules/  ← your validation CLI (5 lines)
│       └── main.go       → calls clikit.Run(... Env: celenv.Env() ...)
└── configs/
    └── rules.yaml        ← what both load
```

That's it. Two `main` packages, one env package, one rules file.

## 3. Building the env package

Three things live in `env.go`:

1. **Custom CEL functions** — each a `func MyFn() cel.FunctionOpt` returning
   a `cel.Overload(...)`.
2. **Optional helpers** that those functions close over (allow-lists,
   feature-flag clients, cached lookups, etc.).
3. **`Env()`** — a constructor that returns a `*celsius.EnvBuilder`
   pre-populated with `DefaultEnv()`, your variables, and your functions.

```go
// internal/celenv/env.go
package celenv

import (
    "github.com/amkarkhi/celsius"
    "github.com/google/cel-go/cel"
    "github.com/google/cel-go/common/types"
    "github.com/google/cel-go/common/types/ref"
)

// IsInternal: is_internal(uid int) -> bool
// Real code would inject the allow-list — this is illustrative.
var internalUIDs = map[int64]struct{}{1: {}, 2: {}, 42: {}}

func IsInternal() cel.FunctionOpt {
    return cel.Overload("is_internal_int",
        []*cel.Type{cel.IntType}, cel.BoolType,
        cel.UnaryBinding(func(v ref.Val) ref.Val {
            n, ok := v.(types.Int)
            if !ok {
                return types.NewErr("is_internal: expected int, got %v", v.Type())
            }
            _, ok = internalUIDs[int64(n)]
            return types.Bool(ok)
        }),
    )
}

// Env is THE environment used by both production and tooling.
func Env() *celsius.EnvBuilder {
    return celsius.DefaultEnv().
        WithVariable("uid",      cel.IntType).
        WithVariable("country",  cel.StringType).
        WithFunction("is_internal", IsInternal())
}
```

A few conventions worth following:

- **Type your variables**. `WithVariable("uid", cel.IntType)` is better than
  letting the validator fall back to `DynType` — you'll catch
  `uid == "42"` (string vs. int) at compile time, not at request time.
- **Name the overload after its signature** (`is_internal_int`). cel-go uses
  the name to disambiguate overloads; collisions will surface as build
  errors.
- **Return `types.NewErr`, not a Go `error`**. CEL handles evaluation
  errors itself; the engine logs and skips the rule.
- **Avoid side effects.** A CEL function should be pure. I/O, mutation, or
  randomness make rules non-deterministic and tests flaky. (`rand()` in
  `celfn` is the rare exception, scoped to A/B-style use.)

## 4. Wiring your server

```go
// cmd/myservice/main.go
engine, err := celsius.New[Out](celsius.Config{
    Source: celsius.FileSource("configs/rules.yaml"),
    Env:    celenv.Env(),       // ← THE env
    Input:  &Input{},
    Logger: &log,
})
```

No special treatment — `celsius.New` accepts any `*EnvBuilder`. See
[`examples/customenv/server/main.go`](../examples/customenv/server/main.go).

## 5. Wiring the validation CLI

A 5-line binary gives you `validate`, `test`, `eval`, and `repl` against
your real env:

```go
// cmd/myservice-rules/main.go
package main

import (
    "os"

    "github.com/amkarkhi/celsius/clikit"
    "myservice/internal/celenv"
)

func main() {
    os.Exit(clikit.Run(clikit.Options{
        Name: "myservice-rules",
        Env:  celenv.Env(),     // ← same env
    }, os.Args[1:]))
}
```

Install and use:

```sh
go install ./cmd/myservice-rules

# Type-check the rules file against the production env. Use this in CI.
myservice-rules validate --strict configs/rules.yaml

# Trial a single match without standing up the server.
myservice-rules test --group homepage --input uid=42 configs/rules.yaml

# Eval a single expression — handy when iterating on a function.
myservice-rules eval --input uid=200 'is_internal(uid)'

# Interactive REPL.
myservice-rules repl configs/rules.yaml
```

Exit codes: `0` success, `1` usage error, `2` validation/match failure.
Drop `validate --strict` straight into a CI job.

See [`examples/customenv/cli/main.go`](../examples/customenv/cli/main.go).

## 6. Unit-testing custom functions

The fastest test path is [`celsius.EvalExpr`][evalexpr]: it compiles and
evaluates a single expression against an `EnvBuilder`. Every key in
`inputs` is auto-declared as `DynType` if your env doesn't already declare
it, so you don't have to mirror declarations from production.

```go
// internal/celenv/env_test.go
package celenv_test

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/amkarkhi/celsius"
    "myservice/internal/celenv"
)

func TestIsInternal(t *testing.T) {
    cases := []struct {
        uid  int
        want bool
    }{
        {1, true}, {42, true}, {99, false},
    }
    for _, tc := range cases {
        got, err := celsius.EvalExpr(
            celenv.Env(),
            `is_internal(uid)`,
            map[string]any{"uid": tc.uid},
        )
        require.NoError(t, err)
        assert.Equal(t, tc.want, got, "uid=%d", tc.uid)
    }
}

func TestIsInternal_RejectsWrongType(t *testing.T) {
    // Compile-time type check: passing a string should fail to compile.
    _, err := celsius.EvalExpr(celenv.Env(), `is_internal("forty-two")`, nil)
    assert.Error(t, err)
}
```

[evalexpr]: https://pkg.go.dev/github.com/amkarkhi/celsius#EvalExpr

## 7. Validating the rules file from Go

Same env, plus [`celsius.ValidateBytes`][validatebytes] — run this as a
regular Go test if you want the build to fail on a broken `rules.yaml`:

```go
func TestRulesFileValidates(t *testing.T) {
    data, err := os.ReadFile("../../configs/rules.yaml")
    require.NoError(t, err)
    issues, err := celsius.ValidateBytes(data, celsius.ValidateOptions{
        Env: celenv.Env(),
    })
    require.NoError(t, err)
    assert.Empty(t, issues, "%v", issues)
}
```

This is functionally what `myservice-rules validate --strict` does, just
inside `go test`.

[validatebytes]: https://pkg.go.dev/github.com/amkarkhi/celsius#ValidateBytes

## 8. CI sketch

A minimal GitHub Actions job that fails the build on a bad config:

```yaml
- name: Validate Celsius rules
  run: |
    go install ./cmd/myservice-rules
    myservice-rules validate --strict configs/rules.yaml
```

Pair with `go test ./internal/celenv/...` so the function tests and the
config validation gate the same PR.

## 9. Anti-patterns

- **Don't** maintain two `Env()` constructors — production and "for tests."
  They will drift, and the bug will surface only after a deploy. There is
  one env; pass test-specific inputs to `EvalExpr` instead.
- **Don't** validate with the stock `celsius` binary if you've registered
  any of your own variables or functions. It cannot know about them, so
  every "unknown identifier" issue it reports is noise — and you may stop
  reading the output.
- **Don't** declare variables only on the validator side (via `--var`)
  when the production engine has them typed. `--var` adds `DynType`
  fallbacks, which mask real type mismatches. Type them properly in
  `Env()` and drop `--var`.
- **Don't** put I/O or mutable state in a CEL function. If you need
  dynamic data, expose it as a *variable* (set by `Input.Parse`), not a
  function.

## 10. See also

- [`examples/customenv`](../examples/customenv/) — runnable end-to-end.
- [`DESIGN.md`](../DESIGN.md) — architecture and lifecycle.
- [`pkg.go.dev`](https://pkg.go.dev/github.com/amkarkhi/celsius) — full API reference.
