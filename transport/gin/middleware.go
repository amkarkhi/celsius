// Package gin provides a Gin middleware adapter for Celsius.
package gin

import (
	"github.com/amkarkhi/celsius"
	"github.com/gin-gonic/gin"
)

// ginCtx adapts a *gin.Context to celsius.Ctx.
type ginCtx struct{ c *gin.Context }

func (g *ginCtx) Header(key, defaultVal string) string {
	if v := g.c.GetHeader(key); v != "" {
		return v
	}
	return defaultVal
}

func (g *ginCtx) Value(key any) any { return g.c.Request.Context().Value(key) }

// Middleware evaluates the named rule group on each request and attaches
// the matched rule to the request context. Handlers retrieve it via
// celsius.ResultFrom[T](c.Request.Context()).
func Middleware[T any](e *celsius.Engine[T], group string) gin.HandlerFunc {
	input := e.Input()
	return func(c *gin.Context) {
		inputs, err := input.Parse(&ginCtx{c: c})
		if err != nil {
			c.Next()
			return
		}
		rule, err := e.Match(group, inputs)
		if err == nil && rule != nil {
			c.Request = c.Request.WithContext(celsius.WithResult(c.Request.Context(), rule))
		}
		c.Next()
	}
}
