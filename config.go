package celsius

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/go-viper/mapstructure/v2"
	"github.com/google/cel-go/cel"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

// Config is the input to [New].
type Config struct {
	// Source provides the raw rule-file bytes and notifies on change.
	// FileSource(path) is the default implementation.
	Source Source

	// Env declares the variables and functions visible to rule scripts.
	// Defaults to DefaultEnv() if nil.
	Env *EnvBuilder

	// Input parses incoming requests into the variable map. Required.
	Input Input

	// Logger receives operational logs (compile errors, reload events).
	// If nil, a no-op logger is used.
	Logger *zerolog.Logger

	// AllowMissingFile, when true, lets New succeed even if the rule file
	// does not yet exist. The engine starts with zero rules; the watcher
	// will pick the file up if it appears later.
	AllowMissingFile bool
}

// Rule is a single rule. T is the user-defined payload type.
//
// `Replace` declares per-field overlays for `Out`. Each key is a
// top-level field name in T (matched by its `mapstructure:` tag); each
// value is a CEL script evaluated at Match time against the same input
// map the rule script saw, plus any `compute:` results. The evaluated
// value replaces the static `Out.<key>` for the returned rule.
//
// Two value shapes are accepted (kept for back-compat with rule files
// that already use the wrapper form):
//
//	# Plain script string — preferred for new rules:
//	replace:
//	  flow: 'call_arbiter(uid).sub_id == "331" ? "hybrid_search_flow" : "es8_search_flow"'
//
//	# Wrapper form — equivalent, older convention:
//	replace:
//	  flow:
//	    type: script
//	    value: 'call_arbiter(uid).sub_id == "331" ? "hybrid_search_flow" : "es8_search_flow"'
//
// A replace script that errors at Match time is logged at debug and the
// static `Out.<key>` is kept (graceful fallback).
type Rule[T any] struct {
	Script  string         `yaml:"script"  mapstructure:"script"`
	Tag     string         `yaml:"tag"     mapstructure:"tag"`
	Out     T              `yaml:"out"     mapstructure:"out"`
	Replace map[string]any `yaml:"replace" mapstructure:"replace"`

	prog         cel.Program
	outRaw       map[string]any
	replaceProgs map[string]cel.Program
}

// configFile is the raw shape of the YAML rule file.
//
// `Compute` declares scripts whose results become CEL input variables
// for every rule in this file. Each script is evaluated once per Match
// call, in undefined order, against the host-supplied inputs; the
// result is bound under the map key for the rule loop. Use this for
// per-request side-effecting helpers (e.g. external arbiter lookups)
// that need to feed multiple rules without each rule re-running them.
type configFile[T any] struct {
	Rules     map[string][]Rule[T] `yaml:"rules"     mapstructure:"rules"`
	Variables map[string]string    `yaml:"variables" mapstructure:"variables"`
	Compute   map[string]string    `yaml:"compute"   mapstructure:"compute"`
}

// Source supplies the rule-file bytes and notifies the engine when they
// change. Implementations must be safe for concurrent reads.
type Source interface {
	// Read returns the current rule bytes. Returning os.ErrNotExist signals
	// that the source is empty; the engine treats that as "no rules" if
	// Config.AllowMissingFile is true.
	Read() ([]byte, error)

	// Watch installs a callback invoked when the source changes. Calling
	// Watch more than once replaces the previous callback. A Source that
	// does not support change notifications may return nil without
	// installing a callback.
	Watch(onChange func()) error

	// Close releases any resources held by the source.
	Close() error
}

// FileSource reads rules from a file on disk and watches it via fsnotify.
func FileSource(path string) Source {
	return &fileSource{path: path}
}

type fileSource struct {
	path string

	mu       sync.Mutex
	watcher  *fsnotify.Watcher
	stopCh   chan struct{}
	onChange func()
}

func (f *fileSource) Read() ([]byte, error) {
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	return b, err
}

func (f *fileSource) Watch(onChange func()) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.watcher != nil {
		f.onChange = onChange
		return nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	// Watch the parent directory so we still get events if the file is
	// replaced (common with atomic writes / config maps).
	dir := filepath.Dir(f.path)
	if dir == "" {
		dir = "."
	}
	if err := w.Add(dir); err != nil {
		w.Close()
		return fmt.Errorf("fsnotify add %s: %w", dir, err)
	}
	f.watcher = w
	f.stopCh = make(chan struct{})
	f.onChange = onChange

	go func() {
		target, _ := filepath.Abs(f.path)
		for {
			select {
			case <-f.stopCh:
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				abs, _ := filepath.Abs(ev.Name)
				if abs != target {
					continue
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					f.mu.Lock()
					cb := f.onChange
					f.mu.Unlock()
					if cb != nil {
						cb()
					}
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return nil
}

func (f *fileSource) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.watcher == nil {
		return nil
	}
	close(f.stopCh)
	err := f.watcher.Close()
	f.watcher = nil
	return err
}

// parseConfig decodes raw YAML bytes into configFile[T].
func parseConfig[T any](data []byte) (*configFile[T], error) {
	if len(data) == 0 {
		return &configFile[T]{}, nil
	}
	// First decode into a generic map so mapstructure can populate T.
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	out := &configFile[T]{}
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           out,
		WeaklyTypedInput: true,
		TagName:          "mapstructure",
	})
	if err != nil {
		return nil, err
	}
	if err := dec.Decode(raw); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	attachRawOut(out, raw)
	return out, nil
}

// attachRawOut walks the raw YAML map and stashes the unstructured
// `out:` block on each Rule, parallel to the typed `Out` field. Replace
// uses this to clone-then-overlay at Match time without round-tripping
// the typed T back to a map.
func attachRawOut[T any](cfg *configFile[T], raw map[string]any) {
	rulesRaw, _ := raw["rules"].(map[string]any)
	if rulesRaw == nil {
		return
	}
	for group, groupRaw := range rulesRaw {
		list, _ := groupRaw.([]any)
		compiled := cfg.Rules[group]
		for i := 0; i < len(list) && i < len(compiled); i++ {
			ruleMap, _ := list[i].(map[string]any)
			if ruleMap == nil {
				continue
			}
			if outRaw, ok := ruleMap["out"].(map[string]any); ok {
				compiled[i].outRaw = outRaw
			}
		}
	}
}
