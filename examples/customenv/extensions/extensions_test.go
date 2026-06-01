// Unit tests for the custom CEL functions. This is the recommended pattern:
// use celsius.EvalExpr to compile + run a single expression against your
// real Env(), so the test exercises the exact same code path as production.
package extensions_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/amkarkhi/celsius"
	"github.com/amkarkhi/celsius/examples/customenv/extensions"
)

func TestIsInternal(t *testing.T) {
	cases := []struct {
		uid  int
		want bool
	}{
		{1, true},
		{42, true},
		{99, false},
	}
	for _, tc := range cases {
		v, err := celsius.EvalExpr(extensions.Env(), `is_internal(uid)`, map[string]any{"uid": tc.uid})
		require.NoError(t, err)
		assert.Equal(t, tc.want, v, "uid=%d", tc.uid)
	}
}

func TestTier(t *testing.T) {
	v, err := celsius.EvalExpr(extensions.Env(), `tier(uid)`, map[string]any{"uid": 200})
	require.NoError(t, err)
	assert.Equal(t, "vip", v)

	v, err = celsius.EvalExpr(extensions.Env(), `tier(uid)`, map[string]any{"uid": 5})
	require.NoError(t, err)
	assert.Equal(t, "regular", v)
}

func TestTier_RejectsWrongType(t *testing.T) {
	// Compile-time type check: tier expects int, passing a string fails.
	_, err := celsius.EvalExpr(extensions.Env(), `tier("not an int")`, nil)
	assert.Error(t, err)
}

func TestRulesFileValidatesAgainstEnv(t *testing.T) {
	// This is the validation a CI pipeline would run on the production
	// rules.yaml — using the SAME env the server registers.
	const body = `
rules:
  homepage:
    - script: 'is_internal(uid)'
      tag:    "internal"
      out:    {banner: "internal-only beta"}
    - script: 'tier(uid) == "vip"'
      tag:    "vip"
      out:    {banner: "welcome, VIP"}
    - script: 'true'
      tag:    "default"
      out:    {banner: "hello"}
variables: {}
`
	issues, err := celsius.ValidateBytes([]byte(body), celsius.ValidateOptions{
		Env: extensions.Env(),
	})
	require.NoError(t, err)
	assert.Empty(t, issues, "issues: %v", issues)
}
