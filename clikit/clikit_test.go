package clikit_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/amkarkhi/celsius"
	"github.com/amkarkhi/celsius/clikit"
)

// doubleIt returns a custom CEL function that doubles an int.
func doubleIt() cel.FunctionOpt {
	return cel.Overload("double_it_int",
		[]*cel.Type{cel.IntType}, cel.IntType,
		cel.UnaryBinding(func(v ref.Val) ref.Val {
			return types.Int(int64(v.(types.Int)) * 2)
		}),
	)
}

// customEnv mimics what a downstream service would set up: its own
// variables and a custom function.
func customEnv() *celsius.EnvBuilder {
	return celsius.DefaultEnv().
		WithVariable("uid", cel.IntType).
		WithFunction("double_it", doubleIt())
}

func writeRules(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

func run(t *testing.T, env *celsius.EnvBuilder, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := clikit.Run(clikit.Options{
		Name:   "test",
		Env:    env,
		Stdout: &stdout,
		Stderr: &stderr,
		Stdin:  strings.NewReader(""),
	}, args)
	return code, stdout.String(), stderr.String()
}

func TestValidate_AcceptsConfigUsingCustomFunction(t *testing.T) {
	body := `
rules:
  g:
    - script: 'double_it(uid) > 10'
      tag:    "big"
      out:    {n: 1}
variables: {}
`
	path := writeRules(t, body)

	// With the default env, strict validation must FAIL — double_it is
	// undeclared.
	code, _, stderr := run(t, celsius.DefaultEnv(), "validate", "--strict", path)
	assert.Equal(t, 2, code, "expected failure with default env, got %d (stderr=%s)", code, stderr)
	assert.Contains(t, stderr, "double_it")

	// With the caller's custom env, strict validation must PASS.
	code, stdout, stderr := run(t, customEnv(), "validate", "--strict", path)
	assert.Equal(t, 0, code, "expected success with custom env, stderr=%s", stderr)
	assert.Contains(t, stdout, "OK")
}

func TestTest_UsesCustomFunction(t *testing.T) {
	body := `
rules:
  g:
    - script: 'double_it(uid) > 100'
      tag:    "big"
      out:    {label: "big"}
    - script: 'true'
      tag:    "small"
      out:    {label: "small"}
variables: {}
`
	path := writeRules(t, body)

	code, stdout, stderr := run(t, customEnv(), "test", "--group", "g", "--input", "uid=60", path)
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "tag=big")

	code, stdout, stderr = run(t, customEnv(), "test", "--group", "g", "--input", "uid=10", path)
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "tag=small")
}

func TestEval_UsesCustomFunction(t *testing.T) {
	code, stdout, stderr := run(t, customEnv(), "eval", "--input", "uid=21", "double_it(uid)")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "42")
}

func TestValidate_SyntaxOnlyDefault(t *testing.T) {
	// Even with an undeclared identifier, default (non-strict) mode passes.
	body := `
rules:
  g:
    - script: 'not_declared == 1'
      tag:    "x"
      out:    {}
variables: {}
`
	path := writeRules(t, body)
	code, stdout, stderr := run(t, celsius.DefaultEnv(), "validate", path)
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "OK")
}

func TestRun_UnknownSubcommand(t *testing.T) {
	code, _, stderr := run(t, nil, "wat")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "unknown subcommand")
}

func TestRun_NoArgs(t *testing.T) {
	code, _, stderr := run(t, nil)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "usage")
}
