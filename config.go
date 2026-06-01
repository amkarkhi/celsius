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
type Rule[T any] struct {
	Script string `yaml:"script" mapstructure:"script"`
	Tag    string `yaml:"tag"    mapstructure:"tag"`
	Out    T      `yaml:"out"    mapstructure:"out"`

	prog cel.Program
}

// configFile is the raw shape of the YAML rule file.
type configFile[T any] struct {
	Rules     map[string][]Rule[T] `yaml:"rules"     mapstructure:"rules"`
	Variables map[string]string    `yaml:"variables" mapstructure:"variables"`
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
	return out, nil
}
