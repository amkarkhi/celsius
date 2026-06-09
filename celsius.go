package celsius

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/go-viper/mapstructure/v2"
	"github.com/google/cel-go/cel"
	"github.com/rs/zerolog"

	"github.com/amkarkhi/celsius/internal/varexpand"
)

// Engine is the compiled rule set. It is safe for concurrent use; reads
// are guarded by a sync.RWMutex and hot reloads swap the internal snapshot
// atomically.
type Engine[T any] struct {
	mu   sync.RWMutex
	snap *snapshot[T]

	cfg   Config
	log   zerolog.Logger
	input Input

	closeOnce sync.Once
	closed    chan struct{}
}

// snapshot is the immutable per-load state.
type snapshot[T any] struct {
	rules     map[string][]Rule[T]
	variables map[string]string
	compute   []computedVar
	env       *cel.Env
}

// computedVar is a per-Match precomputation: evaluate `prog` against the
// host inputs, bind the result under `name` in the input map handed to
// the rule loop. Compiled once per snapshot reload.
type computedVar struct {
	name string
	prog cel.Program
}

// New constructs an engine from the given config. It performs the initial
// load synchronously; any compile error in the rule file is returned.
//
// If cfg.Source returns os.ErrNotExist and cfg.AllowMissingFile is true,
// the engine starts with an empty rule set and the watcher (if any) will
// pick up the file when it appears.
func New[T any](cfg Config) (*Engine[T], error) {
	if cfg.Source == nil {
		return nil, errors.New("celsius: Config.Source is required")
	}
	if cfg.Input == nil {
		return nil, errors.New("celsius: Config.Input is required")
	}
	if cfg.Env == nil {
		cfg.Env = DefaultEnv()
	}
	var logger zerolog.Logger
	if cfg.Logger != nil {
		logger = *cfg.Logger
	} else {
		logger = zerolog.Nop()
	}

	e := &Engine[T]{
		cfg:    cfg,
		log:    logger,
		input:  cfg.Input,
		closed: make(chan struct{}),
	}

	if err := e.reload(); err != nil {
		if errors.Is(err, os.ErrNotExist) && cfg.AllowMissingFile {
			e.log.Warn().Msg("celsius: rule file missing, starting empty")
			snap, _ := e.compileSnapshot(&configFile[T]{})
			e.snap = snap
		} else {
			return nil, err
		}
	}

	if err := cfg.Source.Watch(func() {
		if err := e.reload(); err != nil {
			e.log.Error().Err(err).Msg("celsius: reload failed, keeping previous rules")
		} else {
			e.log.Info().Msg("celsius: rules reloaded")
		}
	}); err != nil {
		e.log.Warn().Err(err).Msg("celsius: watcher disabled")
	}
	return e, nil
}

// Input returns the engine's configured input parser. Transport adapters
// call this; user code rarely needs it.
func (e *Engine[T]) Input() Input { return e.input }

// Close stops the file watcher and releases resources. Subsequent calls
// to Match return ErrClosed.
func (e *Engine[T]) Close() error {
	var err error
	e.closeOnce.Do(func() {
		close(e.closed)
		err = e.cfg.Source.Close()
	})
	return err
}

// Match evaluates the named rule group against inputs. The first rule
// whose script returns true is returned. A non-matching rule whose script
// errors at evaluation time is logged and skipped.
//
// When the rule file declares a top-level `compute:` block, each entry
// is evaluated once against `inputs` before the rule loop runs and its
// result is added to the input map under the entry's name. A compute
// script that errors is logged and treated as nil; rules can guard with
// `has()` or default checks.
func (e *Engine[T]) Match(group string, inputs map[string]any) (*Rule[T], error) {
	select {
	case <-e.closed:
		return nil, ErrClosed
	default:
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.snap == nil {
		return nil, ErrNoRuleGroup
	}
	rules, ok := e.snap.rules[group]
	if !ok {
		return nil, ErrNoRuleGroup
	}
	if len(e.snap.compute) > 0 {
		augmented := make(map[string]any, len(inputs)+len(e.snap.compute))
		for k, v := range inputs {
			augmented[k] = v
		}
		for _, c := range e.snap.compute {
			val, _, err := c.prog.Eval(inputs)
			if err != nil {
				e.log.Debug().Err(err).Str("compute", c.name).Msg("celsius: compute eval error")
				augmented[c.name] = nil
				continue
			}
			augmented[c.name] = val.Value()
		}
		inputs = augmented
	}
	for i := range rules {
		r := &rules[i]
		if r.prog == nil {
			continue
		}
		val, _, err := r.prog.Eval(inputs)
		if err != nil {
			e.log.Debug().Err(err).Str("group", group).Str("tag", r.Tag).Msg("celsius: rule eval error")
			continue
		}
		matched, ok := val.Value().(bool)
		if !ok {
			e.log.Debug().Str("group", group).Str("tag", r.Tag).Msg("celsius: rule did not return bool")
			continue
		}
		if matched {
			return e.applyReplace(r, inputs)
		}
	}
	return nil, ErrNoMatch
}

// applyReplace overlays a matched rule's `replace:` results onto its
// static `out:` block and returns a fresh Rule[T] with the merged Out.
// Replace failures (script error, decode error) log at debug and fall
// back to the static value — never failing the whole match.
func (e *Engine[T]) applyReplace(r *Rule[T], inputs map[string]any) (*Rule[T], error) {
	if len(r.replaceProgs) == 0 {
		return r, nil
	}
	merged := make(map[string]any, len(r.outRaw)+len(r.replaceProgs))
	for k, v := range r.outRaw {
		merged[k] = v
	}
	for key, prog := range r.replaceProgs {
		val, _, err := prog.Eval(inputs)
		if err != nil {
			e.log.Debug().Err(err).Str("tag", r.Tag).Str("replace", key).Msg("celsius: replace eval error")
			continue
		}
		merged[key] = val.Value()
	}
	var newOut T
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           &newOut,
		WeaklyTypedInput: true,
		TagName:          "mapstructure",
	})
	if err != nil {
		e.log.Debug().Err(err).Str("tag", r.Tag).Msg("celsius: replace decoder init failed")
		return r, nil
	}
	if err := dec.Decode(merged); err != nil {
		e.log.Debug().Err(err).Str("tag", r.Tag).Msg("celsius: replace decode failed, keeping static Out")
		return r, nil
	}
	newRule := *r
	newRule.Out = newOut
	return &newRule, nil
}

// Eval compiles and evaluates an ad-hoc script against inputs. The result
// type is whatever the script returns.
func (e *Engine[T]) Eval(script string, inputs map[string]any) (any, error) {
	e.mu.RLock()
	env := e.snap.env
	vars := e.snap.variables
	e.mu.RUnlock()

	expanded, err := varexpand.Expand(script, vars)
	if err != nil {
		return nil, err
	}
	ast, issues := env.Compile(expanded)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	prog, err := env.Program(ast)
	if err != nil {
		return nil, err
	}
	val, _, err := prog.Eval(inputs)
	if err != nil {
		return nil, err
	}
	return val.Value(), nil
}

// Compile compiles an ad-hoc script against the engine's environment and
// returns the AST. Useful for validating user input before storing it.
func (e *Engine[T]) Compile(script string) (*cel.Ast, error) {
	e.mu.RLock()
	env := e.snap.env
	vars := e.snap.variables
	e.mu.RUnlock()

	expanded, err := varexpand.Expand(script, vars)
	if err != nil {
		return nil, err
	}
	ast, issues := env.Compile(expanded)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	return ast, nil
}

// reload reads the source, parses it, compiles into a new snapshot, and
// swaps it in. On any error, the previous snapshot is left intact.
func (e *Engine[T]) reload() error {
	data, err := e.cfg.Source.Read()
	if err != nil {
		return err
	}
	cfg, err := parseConfig[T](data)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	snap, err := e.compileSnapshot(cfg)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.snap = snap
	e.mu.Unlock()
	return nil
}

func (e *Engine[T]) compileSnapshot(cfg *configFile[T]) (*snapshot[T], error) {
	// Compute variables become CEL inputs at Match time; declare them on
	// the env so rule scripts can reference them by name and typecheck.
	// DynType keeps the result shape open — call_arbiter returns a map,
	// other helpers may return strings, ints, etc.
	if len(cfg.Compute) > 0 {
		for name := range cfg.Compute {
			e.cfg.Env.WithVariable(name, cel.DynType)
		}
	}
	env, err := e.cfg.Env.build()
	if err != nil {
		return nil, fmt.Errorf("env: %w", err)
	}
	out := &snapshot[T]{
		rules:     make(map[string][]Rule[T], len(cfg.Rules)),
		variables: cfg.Variables,
		env:       env,
	}
	for name, script := range cfg.Compute {
		expanded, err := varexpand.Expand(script, cfg.Variables)
		if err != nil {
			return nil, fmt.Errorf("compute %q: %w", name, err)
		}
		ast, issues := env.Compile(expanded)
		if issues != nil && issues.Err() != nil {
			return nil, fmt.Errorf("compute %q: %w", name, issues.Err())
		}
		prog, err := env.Program(ast)
		if err != nil {
			return nil, fmt.Errorf("compute %q: %w", name, err)
		}
		out.compute = append(out.compute, computedVar{name: name, prog: prog})
	}
	for group, rs := range cfg.Rules {
		compiled := make([]Rule[T], len(rs))
		for i, r := range rs {
			compiled[i] = r
			expanded, err := varexpand.Expand(r.Script, cfg.Variables)
			if err != nil {
				return nil, fmt.Errorf("group %q rule %d: %w", group, i, err)
			}
			ast, issues := env.Compile(expanded)
			if issues != nil && issues.Err() != nil {
				return nil, fmt.Errorf("group %q rule %d: %w", group, i, issues.Err())
			}
			prog, err := env.Program(ast)
			if err != nil {
				return nil, fmt.Errorf("group %q rule %d: %w", group, i, err)
			}
			compiled[i].prog = prog
			if len(r.Replace) > 0 {
				progs, err := compileReplace(env, cfg.Variables, r.Replace)
				if err != nil {
					return nil, fmt.Errorf("group %q rule %d: %w", group, i, err)
				}
				compiled[i].replaceProgs = progs
			}
		}
		out.rules[group] = compiled
	}
	return out, nil
}

// compileReplace turns a rule's raw `replace:` map into compiled CEL
// programs keyed by field name. Each value may be a bare script string
// or the wrapper form {type: script, value: <string>}.
func compileReplace(env *cel.Env, vars map[string]string, raw map[string]any) (map[string]cel.Program, error) {
	progs := make(map[string]cel.Program, len(raw))
	for key, val := range raw {
		script, err := extractReplaceScript(val)
		if err != nil {
			return nil, fmt.Errorf("replace %q: %w", key, err)
		}
		expanded, err := varexpand.Expand(script, vars)
		if err != nil {
			return nil, fmt.Errorf("replace %q: %w", key, err)
		}
		ast, issues := env.Compile(expanded)
		if issues != nil && issues.Err() != nil {
			return nil, fmt.Errorf("replace %q: %w", key, issues.Err())
		}
		prog, err := env.Program(ast)
		if err != nil {
			return nil, fmt.Errorf("replace %q: %w", key, err)
		}
		progs[key] = prog
	}
	return progs, nil
}

func extractReplaceScript(val any) (string, error) {
	switch v := val.(type) {
	case string:
		return v, nil
	case map[string]any:
		typ, _ := v["type"].(string)
		if typ != "script" {
			return "", fmt.Errorf("type must be \"script\", got %q", typ)
		}
		s, ok := v["value"].(string)
		if !ok || s == "" {
			return "", fmt.Errorf("missing or non-string value")
		}
		return s, nil
	default:
		return "", fmt.Errorf("unsupported shape %T (use a script string or {type: script, value: ...})", val)
	}
}
