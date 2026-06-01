# Changelog

All notable changes to **Celsius** are documented in this file. The project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial open-source release of Celsius.
- Generic `Engine[T]` with type-safe rule payloads.
- CEL-based rule evaluation with hot-reloadable YAML configuration.
- Pluggable `Source` interface — file-based source (`celsius.FileSource`) ships in core; users can plug in remote sources.
- Transport adapters as separate sub-packages:
  - `transport/nethttp` — standard library middleware.
  - `transport/fiber` — Fiber v2 middleware.
  - `transport/gin` — Gin middleware.
- Default CEL helpers: `hash`, `md5`, `contains`, `rand`, `replace`, `to_str`.
- Variable expansion (`var.NAME`) inside rule scripts, with cycle detection.
- Runnable examples for each transport under `examples/basic`, `examples/gin`, `examples/fiber`.
- `examples/customenv` end-to-end demo: a shared `extensions` package with two custom CEL functions (`is_internal`, `tier`), a server consuming it, a `clikit`-based CLI wrapper using the same env, and unit tests showing how to validate the functions and the production rules YAML with `celsius.EvalExpr` / `celsius.ValidateBytes`.
- Unit tests for `internal/varexpand`, `internal/celfn`, and the Gin/Fiber middleware adapters.
- GitHub Actions CI: `go test -race`, `go vet`, `staticcheck`, and `go mod tidy` verification across Go 1.23 and 1.24.
- `CONTRIBUTING.md`.
- `celsius.ValidateBytes` / `celsius.ValidateFile` — programmatic rule-file validation (syntax-only or full type-check).
- `celsius.MatchOnce` — one-shot match helper used by the CLI; handy for tests and custom tooling.
- `cmd/celsius` CLI with `validate`, `test`, `eval`, and `repl` subcommands for pipeline integration and interactive rule debugging.
- `clikit` package exposing the CLI as a library so downstream services can build their own thin binary that injects their custom `EnvBuilder` (variables + functions) — the only way to validate configs that reference identifiers your service registered itself.
- `celsius.EvalExpr` — compile-and-evaluate a single CEL expression against an `EnvBuilder`; intended for unit-testing custom functions.
- `docs/custom-env.md` — standalone how-to for downstream services consuming Celsius: structuring the shared env package, wiring server + CLI from one `Env()`, unit-testing custom CEL functions, and gating CI on `celsius.ValidateBytes`. Linked from the README.
- `examples/customenv/README.md` — directory map and copy-paste instructions for adapting the example to a downstream repo.

### Changed
- Module path is `github.com/amkarkhi/celsius`.
- **Removed** Fiber and Gin imports from the core package; they are now opt-in sub-packages.
- Replaced `IFace[T]` with a concrete `*Engine[T]` (the previous interface mixed public and unexported methods and could not be implemented externally).
- Unified context storage: matched rules are stored as `*Rule[T]` for all transports (previously Fiber/Gin disagreed).
- Renamed `CheckRules` → `Match`, `CheckCustomScript` → `Eval`, `CompileCustomScript` → `Compile`.

### Fixed
- Race condition between hot reload and rule evaluation (now protected by `sync.RWMutex`).
- Panic in `CheckCustomScript` when CEL compilation returned `nil` issues.
- Gin middleware previously stored `*Rule[T]` while `GetResultFromCtx` only unwrapped `**Rule[T]`, breaking Gin handlers.
- `var.NAME` expansion no longer loops indefinitely on cyclic variable references.
- Stray `log.Print` debug call removed from `Match`.
- `CONTEXT_KEY` zero-value collision: explicit context-key configuration is now respected even when the user passes the literal zero key.
- `New` no longer silently returns a healthy engine when the config file is missing; it now returns an error (configurable via `AllowMissingFile`).
- `validate.go` formatting (gofmt alignment in internal `noopInput` / `bytesSource` method sets).
