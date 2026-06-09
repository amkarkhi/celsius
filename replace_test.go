package celsius_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amkarkhi/celsius"
	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// replaceOut mirrors a realistic dispatcher payload: flow is the field
// the host overlays from the arbiter result; message stays static.
type replaceOut struct {
	Flow    string `mapstructure:"flow"`
	Message string `mapstructure:"message"`
}

// TestReplace_OverridesOutField proves the `replace:` block evaluates a
// CEL script at Match time and overlays the result onto the static `out:`
// block. The arbiter stub returns sub_id by uid; replace.flow uses it to
// pick a flow without enumerating every branch as a separate rule.
func TestReplace_OverridesOutField(t *testing.T) {
	yaml := `
compute:
  arbiter: call_arbiter(uid)
rules:
  pick:
    - script: 'true'
      tag: "arbiter_driven"
      out:
        flow: "es8_search_flow"
        message: "default"
      replace:
        flow: 'arbiter.sub_id == "331" ? "hybrid_search_flow" : (arbiter.sub_id == "302" ? "semantic_search_flow" : "es8_search_flow")'
variables: {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	env := celsius.DefaultEnv().
		WithVariable("uid", cel.IntType).
		WithFunction("call_arbiter", stubArbiter())

	e, err := celsius.New[replaceOut](celsius.Config{
		Source: celsius.FileSource(path),
		Env:    env,
		Input:  &Input{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	cases := []struct {
		uid      int64
		wantFlow string
	}{
		{uid: 1, wantFlow: "hybrid_search_flow"},
		{uid: 2, wantFlow: "semantic_search_flow"},
		{uid: 999, wantFlow: "es8_search_flow"},
	}
	for _, tc := range cases {
		r, err := e.Match("pick", map[string]any{"uid": tc.uid})
		require.NoError(t, err, "uid=%d", tc.uid)
		assert.Equal(t, tc.wantFlow, r.Out.Flow, "uid=%d", tc.uid)
		// Static field passes through untouched.
		assert.Equal(t, "default", r.Out.Message, "uid=%d", tc.uid)
	}
}

// TestReplace_WrapperFormSupported confirms the {type: script, value: ...}
// wrapper is accepted for back-compat with rule files that use it.
func TestReplace_WrapperFormSupported(t *testing.T) {
	yaml := `
rules:
  pick:
    - script: 'true'
      tag: "static_default"
      out:
        flow: "fallback"
      replace:
        flow:
          type: script
          value: '"overlaid"'
variables: {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	e, err := celsius.New[replaceOut](celsius.Config{
		Source: celsius.FileSource(path),
		Env:    celsius.DefaultEnv(),
		Input:  &Input{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	r, err := e.Match("pick", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "overlaid", r.Out.Flow)
}

// TestReplace_StaticOutPreservedOnEvalError proves that a replace script
// raising at Match time falls back to the static out value instead of
// failing the whole match — important for arbiter outages.
func TestReplace_StaticOutPreservedOnEvalError(t *testing.T) {
	yaml := `
rules:
  pick:
    - script: 'true'
      tag: "with_bad_replace"
      out:
        flow: "fallback"
      replace:
        flow: '1 / 0 == 0 ? "x" : "y"'
variables: {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	e, err := celsius.New[replaceOut](celsius.Config{
		Source: celsius.FileSource(path),
		Env:    celsius.DefaultEnv(),
		Input:  &Input{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	r, err := e.Match("pick", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "fallback", r.Out.Flow, "static Out should survive replace error")
}
