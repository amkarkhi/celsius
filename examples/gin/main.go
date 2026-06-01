// Gin Celsius example.
//
//	go run ./examples/gin --rules examples/basic/rules.yaml
//	curl -H 'X-UID: 200' http://localhost:8080/hello
package main

import (
	"flag"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/cel-go/cel"
	"github.com/rs/zerolog"

	"github.com/amkarkhi/celsius"
	celsiusgin "github.com/amkarkhi/celsius/transport/gin"
)

type Out struct {
	Message string `mapstructure:"message"`
}

type Input struct{ UID int }

func (i *Input) Map() map[string]any { return map[string]any{"uid": i.UID} }

func (i *Input) Parse(ctx celsius.Ctx) (map[string]any, error) {
	v, _ := strconv.Atoi(ctx.Header("X-UID", "-1"))
	i.UID = v
	return i.Map(), nil
}

func main() {
	rulesPath := flag.String("rules", "examples/basic/rules.yaml", "path to rules YAML")
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	engine, err := celsius.New[Out](celsius.Config{
		Source: celsius.FileSource(*rulesPath),
		Env:    celsius.DefaultEnv().With(celsius.Variable("uid", cel.IntType)),
		Input:  &Input{},
		Logger: &logger,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("init failed")
	}
	defer engine.Close()

	r := gin.Default()
	r.Use(celsiusgin.Middleware(engine, "greeting"))
	r.GET("/hello", func(c *gin.Context) {
		rule, ok := celsius.ResultFrom[Out](c.Request.Context())
		if !ok {
			c.String(http.StatusOK, "no rule matched\n")
			return
		}
		c.String(http.StatusOK, "%s (tag=%s)\n", rule.Out.Message, rule.Tag)
	})

	logger.Info().Str("addr", *addr).Msg("listening")
	_ = r.Run(*addr)
}
