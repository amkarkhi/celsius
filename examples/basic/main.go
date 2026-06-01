// Basic Celsius example: stdlib net/http + a single rule group.
//
//	go run ./examples/basic --rules examples/basic/rules.yaml
//	curl -H 'X-UID: 200' http://localhost:8080/hello
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/amkarkhi/celsius"
	celsiushttp "github.com/amkarkhi/celsius/transport/nethttp"
	"github.com/google/cel-go/cel"
	"github.com/rs/zerolog"
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

	mux := http.NewServeMux()
	mux.Handle("/hello", celsiushttp.Middleware(engine, "greeting")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rule, ok := celsius.ResultFrom[Out](r.Context())
			if !ok {
				fmt.Fprintln(w, "no rule matched")
				return
			}
			fmt.Fprintf(w, "%s (tag=%s)\n", rule.Out.Message, rule.Tag)
		}),
	))
	logger.Info().Str("addr", *addr).Msg("listening")
	_ = http.ListenAndServe(*addr, mux)
}
