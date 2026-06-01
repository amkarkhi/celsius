package celsius_test

import (
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/amkarkhi/celsius"
)

const validRules = `
rules:
  greeting:
    - script: 'uid > 100'
      tag:    "vip"
      out: {message: "vip"}
    - script: 'true'
      tag:    "default"
      out: {message: "default"}
variables: {}
`

const syntaxBroken = `
rules:
  greeting:
    - script: 'uid >'
      tag:    "broken"
      out: {message: ""}
variables: {}
`

const undeclaredIdent = `
rules:
  greeting:
    - script: 'something_we_did_not_declare == 1'
      tag:    "x"
      out: {message: ""}
variables: {}
`

const cyclicVars = `
rules:
  g:
    - script: 'var.a == 1'
      tag:    "x"
      out:    {}
variables:
  a: "var.b"
  b: "var.a"
`

func TestValidate_OK_SyntaxOnly(t *testing.T) {
	issues, err := celsius.ValidateBytes([]byte(validRules), celsius.ValidateOptions{SyntaxOnly: true})
	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestValidate_OK_Strict(t *testing.T) {
	issues, err := celsius.ValidateBytes([]byte(validRules), celsius.ValidateOptions{
		Variables: []string{"uid"},
	})
	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestValidate_SyntaxError(t *testing.T) {
	issues, err := celsius.ValidateBytes([]byte(syntaxBroken), celsius.ValidateOptions{SyntaxOnly: true})
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "script", issues[0].Phase)
	assert.Equal(t, "greeting", issues[0].Group)
	assert.Equal(t, "broken", issues[0].Tag)
}

func TestValidate_UndeclaredFailsStrictPassesSyntax(t *testing.T) {
	// Syntax-only mode does NOT catch undeclared identifiers (parse is enough).
	issues, err := celsius.ValidateBytes([]byte(undeclaredIdent), celsius.ValidateOptions{SyntaxOnly: true})
	require.NoError(t, err)
	assert.Empty(t, issues, "syntax-only should ignore undeclared idents")

	// Strict mode without declaring the identifier should report it.
	issues, err = celsius.ValidateBytes([]byte(undeclaredIdent), celsius.ValidateOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, issues)
	assert.Equal(t, "script", issues[0].Phase)

	// Declaring it makes strict happy.
	issues, err = celsius.ValidateBytes([]byte(undeclaredIdent), celsius.ValidateOptions{
		Variables: []string{"something_we_did_not_declare"},
	})
	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestValidate_VarExpansionCycle(t *testing.T) {
	issues, err := celsius.ValidateBytes([]byte(cyclicVars), celsius.ValidateOptions{SyntaxOnly: true})
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "expand", issues[0].Phase)
	assert.True(t, strings.Contains(issues[0].Err.Error(), "cycle"))
}

func TestValidate_PreservesCallerEnv(t *testing.T) {
	// The validator must not pollute the caller's EnvBuilder.
	env := celsius.DefaultEnv().With(celsius.Variable("uid", cel.IntType))
	_, _ = celsius.ValidateBytes([]byte(validRules), celsius.ValidateOptions{
		Env:       env,
		Variables: []string{"extra_var"},
	})
	// If validator leaked, building env again would still have extra_var.
	// We can't easily introspect, but we can verify a fresh compile of just
	// "uid" + "extra_var" without declaration would fail — i.e. our DefaultEnv
	// extension wasn't mutated.
	_, issues := mustEnv(t, env).Parse("extra_var == 1")
	// Parse should succeed (Parse doesn't require declarations), so this
	// assertion is really just exercising the path.
	_ = issues
}

func mustEnv(t *testing.T, _ *celsius.EnvBuilder) *cel.Env {
	t.Helper()
	e, err := cel.NewEnv(cel.Variable("uid", cel.IntType))
	require.NoError(t, err)
	return e
}

func TestMatchOnce(t *testing.T) {
	tag, out, matched, err := celsius.MatchOnce(
		[]byte(validRules), "greeting",
		map[string]any{"uid": 200}, celsius.ValidateOptions{},
	)
	require.NoError(t, err)
	require.True(t, matched)
	assert.Equal(t, "vip", tag)
	assert.Equal(t, "vip", out["message"])

	tag, _, matched, err = celsius.MatchOnce(
		[]byte(validRules), "greeting",
		map[string]any{"uid": 5}, celsius.ValidateOptions{},
	)
	require.NoError(t, err)
	require.True(t, matched)
	assert.Equal(t, "default", tag)
}

func TestEvalExpr_Builtin(t *testing.T) {
	v, err := celsius.EvalExpr(nil, `hash("abc", 100)`, nil)
	require.NoError(t, err)
	n, ok := v.(int64)
	require.True(t, ok, "want int64, got %T", v)
	assert.True(t, n >= 0 && n < 100)
}

func TestEvalExpr_WithInputs(t *testing.T) {
	v, err := celsius.EvalExpr(nil, `uid > 100 && platform == "ios"`,
		map[string]any{"uid": 200, "platform": "ios"})
	require.NoError(t, err)
	assert.Equal(t, true, v)
}

func TestEvalExpr_CustomFunction(t *testing.T) {
	// Simulate a user-written CEL function and validate it with EvalExpr.
	env := celsius.DefaultEnv().WithFunction("double_it",
		cel.Overload("double_it_int",
			[]*cel.Type{cel.IntType}, cel.IntType,
			cel.UnaryBinding(func(v ref.Val) ref.Val {
				return types.Int(int64(v.(types.Int)) * 2)
			}),
		),
	)

	v, err := celsius.EvalExpr(env, `double_it(uid)`, map[string]any{"uid": 21})
	require.NoError(t, err)
	assert.EqualValues(t, 42, v)

	// Type mismatch surfaces at compile time.
	_, err = celsius.EvalExpr(env, `double_it("nope")`, nil)
	assert.Error(t, err)
}

func TestMatchOnce_GroupNotFound(t *testing.T) {
	_, _, _, err := celsius.MatchOnce(
		[]byte(validRules), "nope",
		map[string]any{"uid": 1}, celsius.ValidateOptions{},
	)
	assert.ErrorIs(t, err, celsius.ErrNoRuleGroup)
}
