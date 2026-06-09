package celsius_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amkarkhi/celsius"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubArbiter mimics the production call_arbiter helper: uid -> {sub_id}.
// Fixed mapping so tests can assert deterministically.
func stubArbiter() cel.FunctionOpt {
	return cel.Overload("stub_arbiter",
		[]*cel.Type{cel.IntType},
		cel.MapType(cel.StringType, cel.StringType),
		cel.FunctionBinding(func(args ...ref.Val) ref.Val {
			uid := args[0].Value().(int64)
			subID := "300"
			switch uid {
			case 1:
				subID = "331"
			case 2:
				subID = "302"
			}
			return types.NewStringStringMap(types.DefaultTypeAdapter, map[string]string{
				"sub_id": subID,
			})
		}),
	)
}

// TestCompute_DrivesRuleMatching proves a top-level `compute:` block is
// evaluated once per Match, the result is exposed to rule scripts as an
// input variable, and scripts can branch on fields of the result.
// Mirrors the production "call_arbiter → pick flow by sub_id" pattern.
func TestCompute_DrivesRuleMatching(t *testing.T) {
	yaml := `
compute:
  arbiter: call_arbiter(uid)
rules:
  pick:
    - script: 'arbiter.sub_id == "331"'
      tag: "hybrid"
      out:
        message: "hybrid"
    - script: 'arbiter.sub_id == "302"'
      tag: "semantic"
      out:
        message: "semantic"
    - script: 'true'
      tag: "default"
      out:
        message: "default"
variables: {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	env := celsius.DefaultEnv().
		WithVariable("uid", cel.IntType).
		WithFunction("call_arbiter", stubArbiter())

	e, err := celsius.New[Out](celsius.Config{
		Source: celsius.FileSource(path),
		Env:    env,
		Input:  &Input{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	cases := []struct {
		uid  int64
		want string
	}{
		{uid: 1, want: "hybrid"},
		{uid: 2, want: "semantic"},
		{uid: 999, want: "default"},
	}
	for _, tc := range cases {
		r, err := e.Match("pick", map[string]any{"uid": tc.uid})
		require.NoError(t, err, "uid=%d", tc.uid)
		assert.Equal(t, tc.want, r.Out.Message, "uid=%d", tc.uid)
	}
}

// TestCompute_EvaluatedOncePerMatch proves the compute script is called
// exactly once per Match call, regardless of how many rules reference
// its output — the whole point of the feature (vs. invoking the helper
// from each rule's script).
func TestCompute_EvaluatedOncePerMatch(t *testing.T) {
	var calls int
	counter := cel.Overload("stub_counter",
		[]*cel.Type{},
		cel.IntType,
		cel.FunctionBinding(func(args ...ref.Val) ref.Val {
			calls++
			return types.Int(calls)
		}),
	)

	yaml := `
compute:
  n: counter()
rules:
  pick:
    - script: 'n > 10'
      tag: "big"
      out: { message: "big" }
    - script: 'n > 5'
      tag: "med"
      out: { message: "med" }
    - script: 'true'
      tag: "default"
      out: { message: "default" }
variables: {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	env := celsius.NewEnv().WithFunction("counter", counter)

	e, err := celsius.New[Out](celsius.Config{
		Source: celsius.FileSource(path),
		Env:    env,
		Input:  &Input{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	calls = 0
	_, err = e.Match("pick", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "compute should run exactly once per Match")
}
