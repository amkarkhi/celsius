// Thin CLI wrapper that gives `celsius` validate/test/eval/repl visibility
// into this service's custom CEL functions and variables. Build & install:
//
//	go install ./examples/customenv/cli
//	customenv-cli validate --strict examples/customenv/rules.yaml
//	customenv-cli test --group homepage --input uid=42 examples/customenv/rules.yaml
//	customenv-cli eval --input uid=200 'tier(uid)'
//
// Drop this same shape into your own repo, swap in your env, and you have a
// pipeline-ready validator for your real configs.
package main

import (
	"os"

	"github.com/amkarkhi/celsius/clikit"
	"github.com/amkarkhi/celsius/examples/customenv/extensions"
)

func main() {
	os.Exit(clikit.Run(clikit.Options{
		Name: "customenv-cli",
		Env:  extensions.Env(), // ← same env the server uses
	}, os.Args[1:]))
}
