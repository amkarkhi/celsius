package celsius

import (
	"github.com/amkarkhi/celsius/internal/celfn"
	"github.com/google/cel-go/cel"
)

// Type and FunctionOpt are re-exported from cel-go so users do not need to
// import cel-go directly for the common case of declaring variables.
type (
	Type        = cel.Type
	FunctionOpt = cel.FunctionOpt
)

// EnvBuilder accumulates CEL environment options. Use [NewEnv] for an empty
// environment or [DefaultEnv] to start with the built-in helper functions
// (hash, md5, contains, rand, replace, to_str).
type EnvBuilder struct {
	vars  map[string]*cel.Type
	funcs map[string][]cel.FunctionOpt
}

// NewEnv returns an empty environment.
func NewEnv() *EnvBuilder {
	return &EnvBuilder{
		vars:  map[string]*cel.Type{},
		funcs: map[string][]cel.FunctionOpt{},
	}
}

// DefaultEnv returns an environment pre-populated with the built-in helper
// functions. It declares no variables — callers add their own.
func DefaultEnv() *EnvBuilder {
	e := NewEnv()
	e.funcs["hash"] = []cel.FunctionOpt{celfn.HashString(), celfn.HashInt(), celfn.HashUint()}
	e.funcs["md5"] = []cel.FunctionOpt{celfn.MD5()}
	e.funcs["contains"] = []cel.FunctionOpt{celfn.Contains()}
	e.funcs["rand"] = []cel.FunctionOpt{celfn.Rand()}
	e.funcs["replace"] = []cel.FunctionOpt{celfn.Replace()}
	e.funcs["to_str"] = []cel.FunctionOpt{celfn.ToStr()}
	return e
}

// EnvOpt is a single declaration applied to an [EnvBuilder].
type EnvOpt func(*EnvBuilder)

// Variable declares a CEL variable.
func Variable(name string, t *cel.Type) EnvOpt {
	return func(e *EnvBuilder) { e.vars[name] = t }
}

// Function declares a CEL function with one or more overloads.
func Function(name string, opts ...cel.FunctionOpt) EnvOpt {
	return func(e *EnvBuilder) { e.funcs[name] = append(e.funcs[name], opts...) }
}

// With applies one or more declarations. It returns the receiver so that
// declarations can be chained.
func (e *EnvBuilder) With(opts ...EnvOpt) *EnvBuilder {
	for _, o := range opts {
		o(e)
	}
	return e
}

// WithVariable is a convenience equivalent to With(Variable(name, t)).
func (e *EnvBuilder) WithVariable(name string, t *cel.Type) *EnvBuilder {
	e.vars[name] = t
	return e
}

// WithFunction is a convenience equivalent to With(Function(name, opts...)).
func (e *EnvBuilder) WithFunction(name string, opts ...cel.FunctionOpt) *EnvBuilder {
	e.funcs[name] = append(e.funcs[name], opts...)
	return e
}

// build materializes the cel.Env.
func (e *EnvBuilder) build() (*cel.Env, error) {
	opts := make([]cel.EnvOption, 0, len(e.vars)+len(e.funcs))
	for name, t := range e.vars {
		opts = append(opts, cel.Variable(name, t))
	}
	for name, fns := range e.funcs {
		opts = append(opts, cel.Function(name, fns...))
	}
	return cel.NewEnv(opts...)
}
