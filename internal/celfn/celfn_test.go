package celfn_test

import (
	"strings"
	"testing"

	"github.com/google/cel-go/cel"

	"github.com/amkarkhi/celsius/internal/celfn"
)

func mustEnv(t *testing.T) *cel.Env {
	t.Helper()
	env, err := cel.NewEnv(
		cel.Variable("s", cel.StringType),
		cel.Variable("i", cel.IntType),
		cel.Variable("u", cel.UintType),
		cel.Variable("d", cel.DoubleType),
		cel.Function("md5", celfn.MD5()),
		cel.Function("hash", celfn.HashString(), celfn.HashInt(), celfn.HashUint()),
		cel.Function("to_str", celfn.ToStr()),
		cel.Function("contains", celfn.Contains()),
		cel.Function("rand", celfn.Rand()),
		cel.Function("replace", celfn.Replace()),
	)
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	return env
}

func eval(t *testing.T, env *cel.Env, expr string, in map[string]any) any {
	t.Helper()
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("compile %q: %v", expr, issues.Err())
	}
	prog, err := env.Program(ast)
	if err != nil {
		t.Fatalf("program: %v", err)
	}
	v, _, err := prog.Eval(in)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	return v.Value()
}

func TestMD5(t *testing.T) {
	env := mustEnv(t)
	got := eval(t, env, `md5(s)`, map[string]any{"s": "hello"})
	if got != "5d41402abc4b2a76b9719d911017c592" {
		t.Fatalf("got %v", got)
	}
}

func TestHashStringDeterministic(t *testing.T) {
	env := mustEnv(t)
	a := eval(t, env, `hash(s, 100)`, map[string]any{"s": "abc"})
	b := eval(t, env, `hash(s, 100)`, map[string]any{"s": "abc"})
	if a != b {
		t.Fatalf("non-deterministic: %v vs %v", a, b)
	}
	n, ok := a.(int64)
	if !ok {
		t.Fatalf("got type %T", a)
	}
	if n < 0 || n >= 100 {
		t.Fatalf("hash out of range: %d", n)
	}
}

func TestHashInt(t *testing.T) {
	env := mustEnv(t)
	v := eval(t, env, `hash(i, 10)`, map[string]any{"i": 42})
	n, ok := v.(int64)
	if !ok {
		t.Fatalf("got type %T", v)
	}
	if n < 0 || n >= 10 {
		t.Fatalf("out of range: %d", n)
	}
}

func TestHashUint(t *testing.T) {
	env := mustEnv(t)
	v := eval(t, env, `hash(u, 10)`, map[string]any{"u": uint(7)})
	n, ok := v.(int64)
	if !ok {
		t.Fatalf("got type %T", v)
	}
	if n < 0 || n >= 10 {
		t.Fatalf("out of range: %d", n)
	}
}

func TestHashZeroModulusErrors(t *testing.T) {
	env := mustEnv(t)
	ast, issues := env.Compile(`hash(s, 0)`)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("compile: %v", issues.Err())
	}
	prog, _ := env.Program(ast)
	_, _, err := prog.Eval(map[string]any{"s": "x"})
	if err == nil || !strings.Contains(err.Error(), "non-zero") {
		t.Fatalf("expected non-zero error, got %v", err)
	}
}

func TestContains(t *testing.T) {
	env := mustEnv(t)
	if v := eval(t, env, `contains(s, "ell")`, map[string]any{"s": "hello"}); v != true {
		t.Fatalf("expected true")
	}
	if v := eval(t, env, `contains(s, "xyz")`, map[string]any{"s": "hello"}); v != false {
		t.Fatalf("expected false")
	}
}

func TestRandInRange(t *testing.T) {
	env := mustEnv(t)
	for range 20 {
		v := eval(t, env, `rand()`, map[string]any{})
		f, ok := v.(float64)
		if !ok {
			t.Fatalf("got %T", v)
		}
		if f < 0 || f >= 1 {
			t.Fatalf("rand out of range: %v", f)
		}
	}
}

func TestReplace(t *testing.T) {
	env := mustEnv(t)
	got := eval(t, env, `replace(s, "-", "")`, map[string]any{"s": "a-b-c"})
	if got != "abc" {
		t.Fatalf("got %v", got)
	}
}

func TestToStr(t *testing.T) {
	env := mustEnv(t)
	if v := eval(t, env, `to_str(i)`, map[string]any{"i": 42}); v != "42" {
		t.Fatalf("got %v", v)
	}
	if v := eval(t, env, `to_str(d)`, map[string]any{"d": 1.5}); v != "1.5" {
		t.Fatalf("got %v", v)
	}
	if v := eval(t, env, `to_str(u)`, map[string]any{"u": uint(7)}); v != "7" {
		t.Fatalf("got %v", v)
	}
}
