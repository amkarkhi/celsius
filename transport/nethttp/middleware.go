// Package nethttp provides a net/http middleware adapter for Celsius.
package nethttp

import (
	"net/http"

	"github.com/amkarkhi/celsius"
)

// Middleware returns a net/http middleware that evaluates the named rule
// group against the incoming request and, on match, attaches the rule to
// the request context. Handlers retrieve it via celsius.ResultFrom[T].
//
// Evaluation errors never abort the request — the middleware always
// invokes next.
func Middleware[T any](e *celsius.Engine[T], group string) func(http.Handler) http.Handler {
	input := e.Input()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			inputs, err := input.Parse(celsius.NewHTTPCtx(r))
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			rule, err := e.Match(group, inputs)
			if err == nil && rule != nil {
				r = r.WithContext(celsius.WithResult(r.Context(), rule))
			}
			next.ServeHTTP(w, r)
		})
	}
}
