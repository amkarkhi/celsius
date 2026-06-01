// celsius is the default CLI for validating and exercising Celsius rule
// files. It runs against [celsius.DefaultEnv] only — so it cannot validate
// configs that reference variables or functions registered elsewhere.
//
// If your service ships its own CEL extensions, build a thin binary in your
// repo that calls [clikit.Run] with your real EnvBuilder. See the package
// docs for [github.com/amkarkhi/celsius/clikit] for an example.
package main

import (
	"os"

	"github.com/amkarkhi/celsius"
	"github.com/amkarkhi/celsius/clikit"
)

func main() {
	os.Exit(clikit.Run(clikit.Options{
		Name: "celsius",
		Env:  celsius.DefaultEnv(),
	}, os.Args[1:]))
}
