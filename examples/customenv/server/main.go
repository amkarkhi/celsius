// HTTP server example using a custom Celsius env (custom variables + custom
// CEL functions). The same Env() is consumed by the CLI wrapper in
// ../cli, so what the CLI validates is what the server runs.
//
//	go run ./examples/customenv/server --rules examples/customenv/rules.yaml
//	curl -H 'X-UID: 42'  http://localhost:8080/hello   # is_internal hit
//	curl -H 'X-UID: 200' http://localhost:8080/hello   # tier == vip
//	curl -H 'X-UID: 5' -H 'X-Country: IR' http://localhost:8080/hello
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/rs/zerolog"

	"github.com/amkarkhi/celsius"
	"github.com/amkarkhi/celsius/examples/customenv/extensions"
	celsiushttp "github.com/amkarkhi/celsius/transport/nethttp"
)

type Out struct {
	Banner string `mapstructure:"banner"`
}

type Input struct {
	UID     int
	Country string
}

func (i *Input) Map() map[string]any {
	return map[string]any{"uid": i.UID, "country": i.Country}
}

func (i *Input) Parse(ctx celsius.Ctx) (map[string]any, error) {
	v, _ := strconv.Atoi(ctx.Header("X-UID", "-1"))
	i.UID = v
	i.Country = ctx.Header("X-Country", "")
	return i.Map(), nil
}

func main() {
	rulesPath := flag.String("rules", "examples/customenv/rules.yaml", "path to rules YAML")
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	engine, err := celsius.New[Out](celsius.Config{
		Source: celsius.FileSource(*rulesPath),
		Env:    extensions.Env(), // ← the shared env
		Input:  &Input{},
		Logger: &logger,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("init failed")
	}
	defer engine.Close()

	mux := http.NewServeMux()
	mux.Handle("/hello", celsiushttp.Middleware(engine, "homepage")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rule, ok := celsius.ResultFrom[Out](r.Context())
			if !ok {
				fmt.Fprintln(w, "no rule matched")
				return
			}
			fmt.Fprintf(w, "%s (tag=%s)\n", rule.Out.Banner, rule.Tag)
		}),
	))

	logger.Info().Str("addr", *addr).Msg("listening")
	_ = http.ListenAndServe(*addr, mux)
}
