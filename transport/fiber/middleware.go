// Package fiber provides a Fiber v2 middleware adapter for Celsius.
package fiber

import (
	"github.com/amkarkhi/celsius"
	"github.com/gofiber/fiber/v2"
)

// fiberCtx adapts a *fiber.Ctx to celsius.Ctx.
type fiberCtx struct{ c *fiber.Ctx }

func (f *fiberCtx) Header(key, defaultVal string) string {
	return f.c.Get(key, defaultVal)
}

func (f *fiberCtx) Value(key any) any { return f.c.UserContext().Value(key) }

// Middleware evaluates the named rule group on each request and attaches
// the matched rule to the request UserContext. Handlers retrieve it via
// celsius.ResultFrom[T](c.UserContext()).
func Middleware[T any](e *celsius.Engine[T], group string) fiber.Handler {
	input := e.Input()
	return func(c *fiber.Ctx) error {
		inputs, err := input.Parse(&fiberCtx{c: c})
		if err != nil {
			return c.Next()
		}
		rule, err := e.Match(group, inputs)
		if err == nil && rule != nil {
			c.SetUserContext(celsius.WithResult(c.UserContext(), rule))
		}
		return c.Next()
	}
}
