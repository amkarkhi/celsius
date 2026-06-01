package celsius

import (
	"context"
	"net/http"
)

// Ctx is the abstraction the [Input.Parse] hook uses to read request data.
// Transport adapters provide concrete implementations.
type Ctx interface {
	// Header returns the value of the named request header, or defaultVal
	// if the header is absent or empty.
	Header(key, defaultVal string) string

	// Value returns the value associated with key in the underlying
	// request context (e.g. a value placed there by upstream middleware),
	// or nil if no such value exists.
	Value(key any) any
}

// Input parses a request into the variable map a CEL program will see.
//
// Map should return the *current* state of the input — it is called by the
// engine after Parse to build the variable map. Parse is allowed (and
// expected) to mutate the receiver.
type Input interface {
	Map() map[string]any
	Parse(ctx Ctx) (map[string]any, error)
}

// resultKey is the unexported context key under which a matched rule is
// stored. Using an unexported type prevents collision with keys from other
// packages.
type resultKey struct{}

// WithResult stores the matched rule on ctx. Transport adapters call this
// after a successful Match; user code rarely needs to.
func WithResult[T any](ctx context.Context, rule *Rule[T]) context.Context {
	return context.WithValue(ctx, resultKey{}, rule)
}

// ResultFrom retrieves a rule previously stored by [WithResult].
// The second return value is false when no rule was stored.
func ResultFrom[T any](ctx context.Context) (*Rule[T], bool) {
	if ctx == nil {
		return nil, false
	}
	r, ok := ctx.Value(resultKey{}).(*Rule[T])
	if !ok || r == nil {
		return nil, false
	}
	return r, true
}

// httpCtx is the [Ctx] implementation for net/http requests.
type httpCtx struct{ r *http.Request }

// NewHTTPCtx wraps an *http.Request as a celsius.Ctx.
func NewHTTPCtx(r *http.Request) Ctx { return &httpCtx{r: r} }

func (c *httpCtx) Header(key, defaultVal string) string {
	if v := c.r.Header.Get(key); v != "" {
		return v
	}
	return defaultVal
}

func (c *httpCtx) Value(key any) any { return c.r.Context().Value(key) }
