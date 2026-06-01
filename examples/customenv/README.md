# examples/customenv

End-to-end demonstration of the pattern you'll want when consuming Celsius
from your own service: a **shared CEL environment** with custom variables and
custom functions, used by both your production code and your CI validation
tooling.

> See [`docs/custom-env.md`](../../docs/custom-env.md) for the full how-to.

## Layout

```
examples/customenv/
├── extensions/      Shared env: custom vars + custom CEL functions.
│   ├── extensions.go       Env() — the single source of truth.
│   └── extensions_test.go  Unit-tests the custom fns via celsius.EvalExpr,
│                           and validates rules.yaml against the production env.
├── server/          HTTP server consuming extensions.Env().
├── cli/             Thin clikit binary consuming extensions.Env().
└── rules.yaml       Sample rules that reference the custom env.
```

The key invariant: `extensions.Env()` is imported in exactly one place each
by `server/` and `cli/`. Anything that passes the CLI's `validate` /
`test` / `eval` will also load and run in the server — and vice versa.

## Run it

```sh
# Validate the rules file against the production env (great for CI).
go run ./examples/customenv/cli validate --strict examples/customenv/rules.yaml

# Try a one-shot match.
go run ./examples/customenv/cli test \
  --group homepage --input uid=42 \
  examples/customenv/rules.yaml

# Eval an arbitrary expression using your custom functions.
go run ./examples/customenv/cli eval --input uid=200 'tier(uid)'

# Or start the HTTP server.
go run ./examples/customenv/server --rules examples/customenv/rules.yaml
curl -H 'X-UID: 42'  http://localhost:8080/hello   # is_internal hit
curl -H 'X-UID: 200' http://localhost:8080/hello   # tier == vip
```

## Adapting it to your repo

Copy the directory shape into your own module, swap `extensions` for your
own package name, and register your real variables and functions in `Env()`.
The `server/` and `cli/` wiring is mechanical — most teams paste it
unchanged.
