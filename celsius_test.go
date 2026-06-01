package celsius_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/amkarkhi/celsius"
	celsiushttp "github.com/amkarkhi/celsius/transport/nethttp"
	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type Out struct {
	Message string `mapstructure:"message"`
}

type Input struct{ UID int }

func (i *Input) Map() map[string]any { return map[string]any{"uid": i.UID} }
func (i *Input) Parse(c celsius.Ctx) (map[string]any, error) {
	v, _ := strconv.Atoi(c.Header("X-UID", "-1"))
	i.UID = v
	return i.Map(), nil
}

const sampleRules = `
rules:
  greeting:
    - script: 'uid > 100'
      tag:    "vip"
      out:
        message: "vip"
    - script: 'true'
      tag:    "default"
      out:
        message: "default"
variables: {}
`

func newEngine(t *testing.T, body string) (*celsius.Engine[Out], string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	e, err := celsius.New[Out](celsius.Config{
		Source: celsius.FileSource(path),
		Env:    celsius.DefaultEnv().With(celsius.Variable("uid", cel.IntType)),
		Input:  &Input{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })
	return e, path
}

func TestMatch(t *testing.T) {
	e, _ := newEngine(t, sampleRules)

	r, err := e.Match("greeting", map[string]any{"uid": 200})
	require.NoError(t, err)
	assert.Equal(t, "vip", r.Out.Message)

	r, err = e.Match("greeting", map[string]any{"uid": 5})
	require.NoError(t, err)
	assert.Equal(t, "default", r.Out.Message)

	_, err = e.Match("missing", map[string]any{"uid": 1})
	assert.ErrorIs(t, err, celsius.ErrNoRuleGroup)
}

func TestEval(t *testing.T) {
	e, _ := newEngine(t, sampleRules)
	v, err := e.Eval(`uid * 2`, map[string]any{"uid": 21})
	require.NoError(t, err)
	assert.EqualValues(t, 42, v)
}

func TestNetHTTPMiddleware(t *testing.T) {
	e, _ := newEngine(t, sampleRules)

	h := celsiushttp.Middleware(e, "greeting")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rule, ok := celsius.ResultFrom[Out](r.Context())
		if !ok {
			w.Write([]byte("none"))
			return
		}
		w.Write([]byte(rule.Out.Message))
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("X-UID", "200")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	buf := make([]byte, 32)
	n, _ := resp.Body.Read(buf)
	assert.Equal(t, "vip", string(buf[:n]))
}

func TestConcurrentMatchDuringReload(t *testing.T) {
	// Smoke test that the RWMutex/snapshot swap doesn't crash under load.
	e, path := newEngine(t, sampleRules)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = e.Match("greeting", map[string]any{"uid": 200})
				}
			}
		}()
	}
	// Rewrite the file a few times.
	for i := 0; i < 5; i++ {
		require.NoError(t, os.WriteFile(path, []byte(sampleRules), 0o644))
		time.Sleep(20 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
}

func TestVarExpansion(t *testing.T) {
	body := `
rules:
  g:
    - script: 'uid > var.threshold'
      tag:    "over"
      out:    {message: "over"}
variables:
  threshold: "50"
`
	e, _ := newEngine(t, body)
	r, err := e.Match("g", map[string]any{"uid": 100})
	require.NoError(t, err)
	assert.Equal(t, "over", r.Out.Message)
}

func TestVarExpansionCycle(t *testing.T) {
	body := `
rules:
  g:
    - script: 'var.a == "x"'
      tag:    "t"
      out:    {message: ""}
variables:
  a: "var.b"
  b: "var.a"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	_, err := celsius.New[Out](celsius.Config{
		Source: celsius.FileSource(path),
		Input:  &Input{},
	})
	assert.ErrorContains(t, err, "cycle")
}

func TestAllowMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nope.yaml")
	e, err := celsius.New[Out](celsius.Config{
		Source:           celsius.FileSource(path),
		Input:            &Input{},
		AllowMissingFile: true,
	})
	require.NoError(t, err)
	defer e.Close()
	_, err = e.Match("anything", map[string]any{})
	assert.ErrorIs(t, err, celsius.ErrNoRuleGroup)
}

func TestResultFromNilCtx(t *testing.T) {
	_, ok := celsius.ResultFrom[Out](context.Background())
	assert.False(t, ok)
}
