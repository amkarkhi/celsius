// Package celsius is a CEL-driven rule engine for runtime decisions.
//
// Celsius compiles a YAML rule file at startup, hot-reloads it on change,
// and exposes a typed [Engine] whose [Engine.Match] method evaluates a
// named rule group against a map of inputs. Each rule's payload is decoded
// into a user-supplied generic parameter T.
//
// Transport adapters (net/http, Gin, Fiber) live in separate sub-packages
// so the core has no web-framework dependencies.
//
// # Custom environments
//
// Extend [DefaultEnv] with your own variables and functions via
// [EnvBuilder.WithVariable] / [EnvBuilder.WithFunction]. The same
// [EnvBuilder] should be passed to both [New] (in your server) and to
// clikit.Run (in a thin validation CLI) so the CLI accepts exactly the
// identifiers your production engine accepts. See docs/custom-env.md and
// the runnable example at examples/customenv.
//
// # Validation tooling
//
// [ValidateBytes] / [ValidateFile] check a rules file against an
// [EnvBuilder] — drop them in a Go test, or use the shipped CLI at
// cmd/celsius. [EvalExpr] compiles and runs a single expression against
// an [EnvBuilder] and is the recommended unit-test entry point for
// custom CEL functions.
//
// See https://github.com/amkarkhi/celsius for the complete README and
// design document.
package celsius
