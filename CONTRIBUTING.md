# Contributing to Celsius

Thanks for your interest. Celsius is small, focused, and unopinionated about what you build on top of it — contributions that keep it that way are very welcome.

## Ground rules

- **No CLA.** By submitting a PR you agree your contribution is MIT-licensed under the project license.
- **Discuss large changes first.** For anything bigger than a bug fix or a small feature, open an issue before sending a PR.
- **Keep the core transport-agnostic.** The root package must not import Gin, Fiber, or any other web framework. Transport adapters live under `transport/`.

## Development setup

Requires Go 1.23 or later.

```sh
git clone https://github.com/amkarkhi/celsius
cd celsius
go build ./...
go test -race ./...
```

## Before you push

Run the same checks CI does:

```sh
go mod tidy
go build ./...
go vet ./...
go test -race ./...
```

Optional but recommended:

```sh
go install honnef.co/go/tools/cmd/staticcheck@latest
staticcheck ./...
```

## What we care about in reviews

- **Race safety.** Hot reload runs concurrently with `Match`. New code paths that touch shared state must be covered by a `-race` test.
- **No surprise dependencies in core.** If your change adds an import to a transport-specific library from the root package, it will get pushed back.
- **Tests over assertions.** Add a test case for the behavior you're adding or the bug you're fixing.
- **Comments only when the *why* is non-obvious.** Don't restate the code.
- **Public API changes** require a `CHANGELOG.md` entry under `[Unreleased]`.

## Adding a new transport adapter

1. Create `transport/<name>/middleware.go` in its own package.
2. Wrap the framework's request type in a small `celsiusCtx` adapter that satisfies `celsius.Ctx`.
3. After a successful `engine.Match`, store the matched rule using `celsius.WithResult` — **always as `*Rule[T]` (single pointer)**. All transports must agree on this so `ResultFrom[T]` works regardless of which adapter ran.
4. Add tests that round-trip a request through the middleware and assert `ResultFrom[T]` returns the expected rule.

## Filing bugs

Please include:
- Go version (`go version`)
- A minimal reproducer (rules YAML + the smallest Go file that triggers the issue)
- Expected vs. actual behavior

## Releases

Maintainers tag releases. `CHANGELOG.md` follows [Keep a Changelog](https://keepachangelog.com/) loosely; versions follow [SemVer](https://semver.org/).
