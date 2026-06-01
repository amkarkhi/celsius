package celsius

import (
	"errors"
	"fmt"
	"maps"
	"os"

	"github.com/google/cel-go/cel"

	"github.com/amkarkhi/celsius/internal/varexpand"
)

// Issue describes a single problem found by [ValidateBytes] or [ValidateFile].
//
// Phase is one of:
//   - "parse"  — the YAML could not be decoded.
//   - "expand" — a var.NAME expansion error (e.g. cycle).
//   - "script" — the CEL expression failed to parse or type-check.
type Issue struct {
	Group string
	Index int
	Tag   string
	Phase string
	Err   error
}

func (i Issue) Error() string {
	if i.Group == "" {
		return fmt.Sprintf("%s: %v", i.Phase, i.Err)
	}
	return fmt.Sprintf("group %q rule %d (tag=%q) %s: %v", i.Group, i.Index, i.Tag, i.Phase, i.Err)
}

// ValidateOptions controls validator behavior.
type ValidateOptions struct {
	// Env is the CEL environment to use for compilation. If nil, DefaultEnv()
	// is used. The validator will additionally declare every name listed in
	// Variables as cel.DynType so unknown identifiers don't fail type-check.
	Env *EnvBuilder

	// Variables is an optional list of variable names to declare as
	// cel.DynType for the purpose of validation. Useful for pipeline
	// type-checking when the production code declares concrete types.
	Variables []string

	// SyntaxOnly, when true, only parses each script (no type-check). This
	// catches grammar errors but does not require variable declarations.
	// Use this mode for the simplest "is this YAML well-formed and is each
	// CEL expression syntactically valid" check.
	SyntaxOnly bool
}

// ValidateFile reads path and runs [ValidateBytes] on its contents.
func ValidateFile(path string, opts ValidateOptions) ([]Issue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ValidateBytes(data, opts)
}

// ValidateBytes parses a Celsius YAML config and validates every rule's CEL
// script. It returns a slice of [Issue]; an empty slice means the config is
// valid. The second return value is a hard error (e.g. YAML decode failure)
// that prevented validation from running.
func ValidateBytes(data []byte, opts ValidateOptions) ([]Issue, error) {
	cfg, err := parseConfig[any](data)
	if err != nil {
		return []Issue{{Phase: "parse", Err: err}}, nil
	}

	envBuilder := opts.Env
	if envBuilder == nil {
		envBuilder = DefaultEnv()
	}
	// Clone so we don't mutate caller's builder.
	clone := NewEnv()
	maps.Copy(clone.vars, envBuilder.vars)
	maps.Copy(clone.funcs, envBuilder.funcs)
	for _, name := range opts.Variables {
		if _, ok := clone.vars[name]; !ok {
			clone.vars[name] = cel.DynType
		}
	}
	env, err := clone.build()
	if err != nil {
		return []Issue{{Phase: "env", Err: err}}, nil
	}

	var issues []Issue
	for group, rules := range cfg.Rules {
		for i, r := range rules {
			expanded, err := varexpand.Expand(r.Script, cfg.Variables)
			if err != nil {
				issues = append(issues, Issue{Group: group, Index: i, Tag: r.Tag, Phase: "expand", Err: err})
				continue
			}
			var celIssues *cel.Issues
			if opts.SyntaxOnly {
				_, celIssues = env.Parse(expanded)
			} else {
				_, celIssues = env.Compile(expanded)
			}
			if celIssues != nil && celIssues.Err() != nil {
				issues = append(issues, Issue{Group: group, Index: i, Tag: r.Tag, Phase: "script", Err: celIssues.Err()})
			}
		}
	}
	return issues, nil
}

// MatchOnce parses bytes, builds a one-shot engine, and matches the named
// group against inputs. Variable names in inputs are auto-declared as
// cel.DynType. It is intended for ad-hoc testing (CLI, REPL); production
// code should use [New] + [Engine.Match].
//
// The matched rule's Out is returned as a map[string]any. tag is the rule's
// tag, matched is false when no rule matched.
func MatchOnce(data []byte, group string, inputs map[string]any, opts ValidateOptions) (tag string, out map[string]any, matched bool, err error) {
	envBuilder := opts.Env
	if envBuilder == nil {
		envBuilder = DefaultEnv()
	}
	clone := NewEnv()
	maps.Copy(clone.vars, envBuilder.vars)
	maps.Copy(clone.funcs, envBuilder.funcs)
	for name := range inputs {
		if _, ok := clone.vars[name]; !ok {
			clone.vars[name] = cel.DynType
		}
	}
	for _, name := range opts.Variables {
		if _, ok := clone.vars[name]; !ok {
			clone.vars[name] = cel.DynType
		}
	}

	e := &Engine[map[string]any]{cfg: Config{Env: clone, Input: noopInput{}, Source: bytesSource(data)}, input: noopInput{}, closed: make(chan struct{})}
	if err := e.reload(); err != nil {
		return "", nil, false, err
	}
	rule, err := e.Match(group, inputs)
	if errors.Is(err, ErrNoMatch) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	return rule.Tag, rule.Out, true, nil
}

// EvalExpr compiles and evaluates a single CEL expression against env. Every
// key in inputs is auto-declared as cel.DynType if env doesn't already
// declare it, so callers don't need to mirror their production declarations
// in tests.
//
// Intended for unit-testing custom functions registered on an EnvBuilder:
//
//	env := celsius.DefaultEnv().WithFunction("myhash", myHashFn())
//	got, err := celsius.EvalExpr(env, `myhash("abc") % 10`, nil)
//	require.NoError(t, err)
//	require.EqualValues(t, 7, got)
//
// If env is nil, [DefaultEnv] is used.
func EvalExpr(env *EnvBuilder, expr string, inputs map[string]any) (any, error) {
	if env == nil {
		env = DefaultEnv()
	}
	clone := NewEnv()
	maps.Copy(clone.vars, env.vars)
	maps.Copy(clone.funcs, env.funcs)
	for name := range inputs {
		if _, ok := clone.vars[name]; !ok {
			clone.vars[name] = cel.DynType
		}
	}
	celEnv, err := clone.build()
	if err != nil {
		return nil, fmt.Errorf("env: %w", err)
	}
	ast, issues := celEnv.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	prog, err := celEnv.Program(ast)
	if err != nil {
		return nil, err
	}
	val, _, err := prog.Eval(inputs)
	if err != nil {
		return nil, err
	}
	return val.Value(), nil
}

type noopInput struct{}

func (noopInput) Map() map[string]any               { return nil }
func (noopInput) Parse(Ctx) (map[string]any, error) { return nil, nil }

type bytesSource []byte

func (b bytesSource) Read() ([]byte, error) { return []byte(b), nil }
func (bytesSource) Watch(func()) error      { return nil }
func (bytesSource) Close() error            { return nil }
