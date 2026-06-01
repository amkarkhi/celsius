// Package clikit is the reusable implementation of the `celsius` CLI.
//
// The binary in cmd/celsius is a thin wrapper that calls [Run] with
// [celsius.DefaultEnv]. If your service registers extra CEL variables or
// functions, build your own thin wrapper that hands clikit your real
// EnvBuilder so validate/test/eval/repl all see the same environment your
// production code uses:
//
//	// cmd/myapp-rules/main.go
//	package main
//
//	import (
//	    "os"
//
//	    "github.com/amkarkhi/celsius"
//	    "github.com/amkarkhi/celsius/clikit"
//	    "github.com/google/cel-go/cel"
//
//	    "myapp/rules/extensions"
//	)
//
//	func main() {
//	    env := celsius.DefaultEnv().
//	        WithVariable("uid", cel.IntType).
//	        WithVariable("platform", cel.StringType).
//	        WithFunction("myhash", extensions.MyHash())
//
//	    os.Exit(clikit.Run(clikit.Options{
//	        Name: "myapp-rules",
//	        Env:  env,
//	    }, os.Args[1:]))
//	}
//
// Then `myapp-rules validate rules.yaml --strict` actually understands your
// custom identifiers.
package clikit

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/amkarkhi/celsius"
)

// Options configures the CLI.
type Options struct {
	// Name is the binary name shown in usage messages. Defaults to "celsius".
	Name string

	// Env is the CEL environment used for validate/test/eval/repl. If nil,
	// celsius.DefaultEnv() is used. Pre-populate it with the variables and
	// functions your production code registers so the CLI accepts the same
	// expressions.
	Env *celsius.EnvBuilder

	// Stdout / Stderr / Stdin can be overridden for testing. Defaults map
	// to the real os streams.
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
}

func (o *Options) defaults() {
	if o.Name == "" {
		o.Name = "celsius"
	}
	if o.Env == nil {
		o.Env = celsius.DefaultEnv()
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
}

// Run dispatches a subcommand. It returns the desired process exit code:
// 0 success, 1 usage error, 2 validation/match failure.
func Run(opts Options, args []string) int {
	opts.defaults()
	usage := fmt.Sprintf(`%[1]s — Celsius rule-file CLI

usage:
  %[1]s validate [--strict] [--var NAME ...] <file>
  %[1]s test     --group GROUP [--input K=V ...] [--var NAME ...] <file>
  %[1]s eval     [--input K=V ...] <expression>
  %[1]s repl     <file>

The <file>/<expression> argument must come AFTER any flags.
`, opts.Name)

	if len(args) < 1 {
		fmt.Fprint(opts.Stderr, usage)
		return 1
	}
	switch args[0] {
	case "validate":
		return cmdValidate(opts, args[1:])
	case "test":
		return cmdTest(opts, args[1:])
	case "eval":
		return cmdEval(opts, args[1:])
	case "repl":
		return cmdRepl(opts, args[1:])
	case "-h", "--help", "help":
		fmt.Fprint(opts.Stdout, usage)
		return 0
	default:
		fmt.Fprintf(opts.Stderr, "unknown subcommand %q\n\n%s", args[0], usage)
		return 1
	}
}

type stringSliceFlag []string

func (s *stringSliceFlag) String() string     { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error { *s = append(*s, v); return nil }

func cmdValidate(opts Options, args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(opts.Stderr)
	var strict bool
	var vars stringSliceFlag
	fs.BoolVar(&strict, "strict", false, "also type-check scripts (requires --var for unknown identifiers)")
	fs.Var(&vars, "var", "declare an additional CEL variable as DynType (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(opts.Stderr, "validate: missing <file>")
		return 1
	}
	path := fs.Arg(0)

	issues, err := celsius.ValidateFile(path, celsius.ValidateOptions{
		Env:        opts.Env,
		Variables:  []string(vars),
		SyntaxOnly: !strict,
	})
	if err != nil {
		fmt.Fprintf(opts.Stderr, "validate: %v\n", err)
		return 2
	}
	if len(issues) == 0 {
		fmt.Fprintf(opts.Stdout, "✓ %s — OK\n", path)
		return 0
	}
	fmt.Fprintf(opts.Stderr, "✗ %s — %d issue(s):\n", path, len(issues))
	for _, iss := range issues {
		fmt.Fprintf(opts.Stderr, "  - %s\n", iss.Error())
	}
	return 2
}

func cmdTest(opts Options, args []string) int {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(opts.Stderr)
	var group string
	var inputs stringSliceFlag
	var vars stringSliceFlag
	fs.StringVar(&group, "group", "", "rule group to evaluate")
	fs.StringVar(&group, "g", "", "rule group to evaluate (shorthand)")
	fs.Var(&inputs, "input", "input as KEY=VALUE (repeatable). Values are auto-typed (int/float/bool/string).")
	fs.Var(&inputs, "i", "alias of --input")
	fs.Var(&vars, "var", "declare an extra CEL variable as DynType (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(opts.Stderr, "test: missing <file>")
		return 1
	}
	if group == "" {
		fmt.Fprintln(opts.Stderr, "test: --group is required")
		return 1
	}
	path := fs.Arg(0)
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "test: %v\n", err)
		return 2
	}
	in, err := parseInputs([]string(inputs))
	if err != nil {
		fmt.Fprintf(opts.Stderr, "test: %v\n", err)
		return 1
	}
	tag, out, matched, err := celsius.MatchOnce(data, group, in, celsius.ValidateOptions{
		Env:       opts.Env,
		Variables: []string(vars),
	})
	if err != nil {
		fmt.Fprintf(opts.Stderr, "test: %v\n", err)
		return 2
	}
	if !matched {
		fmt.Fprintln(opts.Stdout, "no rule matched")
		return 0
	}
	fmt.Fprintf(opts.Stdout, "matched: tag=%s\n", tag)
	if len(out) > 0 {
		keys := make([]string, 0, len(out))
		for k := range out {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintln(opts.Stdout, "out:")
		for _, k := range keys {
			fmt.Fprintf(opts.Stdout, "  %s: %v\n", k, out[k])
		}
	}
	return 0
}

func cmdEval(opts Options, args []string) int {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(opts.Stderr)
	var inputs stringSliceFlag
	fs.Var(&inputs, "input", "input as KEY=VALUE (repeatable)")
	fs.Var(&inputs, "i", "alias of --input")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(opts.Stderr, "eval: missing <expression>")
		return 1
	}
	expr := strings.Join(fs.Args(), " ")
	in, err := parseInputs([]string(inputs))
	if err != nil {
		fmt.Fprintf(opts.Stderr, "eval: %v\n", err)
		return 1
	}
	v, err := celsius.EvalExpr(opts.Env, expr, in)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "eval: %v\n", err)
		return 2
	}
	fmt.Fprintf(opts.Stdout, "%v\n", v)
	return 0
}

func cmdRepl(opts Options, args []string) int {
	fs := flag.NewFlagSet("repl", flag.ContinueOnError)
	fs.SetOutput(opts.Stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(opts.Stderr, "repl: missing <file>")
		return 1
	}
	path := fs.Arg(0)

	fmt.Fprintf(opts.Stdout, "%s repl — type 'help' for commands, Ctrl-D to exit\n", opts.Name)
	fmt.Fprintf(opts.Stdout, "rules: %s\n", path)
	sc := bufio.NewScanner(opts.Stdin)
	for {
		fmt.Fprint(opts.Stdout, "> ")
		if !sc.Scan() {
			break
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		switch {
		case line == "help":
			fmt.Fprintln(opts.Stdout, "commands:")
			fmt.Fprintln(opts.Stdout, "  match <group> [key=value ...]   run a match")
			fmt.Fprintln(opts.Stdout, "  validate                         re-validate the rule file (syntax)")
			fmt.Fprintln(opts.Stdout, "  quit                             exit")
		case line == "quit", line == "exit":
			return 0
		case line == "validate":
			issues, err := celsius.ValidateFile(path, celsius.ValidateOptions{Env: opts.Env, SyntaxOnly: true})
			if err != nil {
				fmt.Fprintf(opts.Stdout, "error: %v\n", err)
				continue
			}
			if len(issues) == 0 {
				fmt.Fprintln(opts.Stdout, "OK")
			} else {
				for _, iss := range issues {
					fmt.Fprintf(opts.Stdout, "  - %s\n", iss.Error())
				}
			}
		case strings.HasPrefix(line, "match "):
			rest := strings.TrimSpace(strings.TrimPrefix(line, "match "))
			parts := strings.Fields(rest)
			if len(parts) == 0 {
				fmt.Fprintln(opts.Stdout, "usage: match <group> [key=value ...]")
				continue
			}
			runMatch(opts, path, parts[0], parts[1:])
		default:
			fmt.Fprintln(opts.Stdout, "unknown command; try 'help'")
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(opts.Stderr, "repl: read input: %v\n", err)
		return 2
	}
	return 0
}

func runMatch(opts Options, path, group string, kvs []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(opts.Stdout, "error: %v\n", err)
		return
	}
	in, err := parseInputs(kvs)
	if err != nil {
		fmt.Fprintf(opts.Stdout, "error: %v\n", err)
		return
	}
	tag, out, matched, err := celsius.MatchOnce(data, group, in, celsius.ValidateOptions{Env: opts.Env})
	if err != nil {
		fmt.Fprintf(opts.Stdout, "error: %v\n", err)
		return
	}
	if !matched {
		fmt.Fprintln(opts.Stdout, "no match")
		return
	}
	fmt.Fprintf(opts.Stdout, "matched tag=%s out=%v\n", tag, out)
}

func parseInputs(kvs []string) (map[string]any, error) {
	out := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			return nil, fmt.Errorf("bad input %q (want KEY=VALUE)", kv)
		}
		out[kv[:idx]] = coerce(kv[idx+1:])
	}
	return out, nil
}

func coerce(s string) any {
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}
