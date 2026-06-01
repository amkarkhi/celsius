// Fiber Celsius example.
//
//	go run ./examples/fiber --rules examples/basic/rules.yaml
//	curl -H 'X-UID: 200' http://localhost:8080/hello
package main

import (
	"flag"
	"os"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/cel-go/cel"
	"github.com/rs/zerolog"

	"github.com/amkarkhi/celsius"
	celsiusfiber "github.com/amkarkhi/celsius/transport/fiber"
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

	app := fiber.New()
	app.Use(celsiusfiber.Middleware(engine, "greeting"))
	app.Get("/hello", func(c *fiber.Ctx) error {
		rule, ok := celsius.ResultFrom[Out](c.UserContext())
		if !ok {
			return c.SendString("no rule matched\n")
		}
		return c.SendString(rule.Out.Message + " (tag=" + rule.Tag + ")\n")
	})

	logger.Info().Str("addr", *addr).Msg("listening")
	_ = app.Listen(*addr)
}
