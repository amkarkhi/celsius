package varexpand_test

import (
	"strings"
	"testing"

	"github.com/amkarkhi/celsius/internal/varexpand"
)

func TestSimpleSubstitution(t *testing.T) {
	got, err := varexpand.Expand("uid > var.threshold", map[string]string{"threshold": "100"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "uid > 100" {
		t.Fatalf("got %q", got)
	}
}

func TestNoMatch(t *testing.T) {
	got, err := varexpand.Expand("uid > 100", map[string]string{"threshold": "100"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "uid > 100" {
		t.Fatalf("got %q", got)
	}
}

func TestUnknownReferenceLeftUntouched(t *testing.T) {
	got, err := varexpand.Expand("x == var.missing", map[string]string{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "x == var.missing" {
		t.Fatalf("got %q", got)
	}
}

func TestNestedExpansion(t *testing.T) {
	vars := map[string]string{
		"a": "var.b + 1",
		"b": "2",
	}
	got, err := varexpand.Expand("x == var.a", vars)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "x == 2 + 1" {
		t.Fatalf("got %q", got)
	}
}

func TestDirectCycle(t *testing.T) {
	_, err := varexpand.Expand("x == var.a", map[string]string{"a": "var.a"})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestIndirectCycle(t *testing.T) {
	vars := map[string]string{
		"a": "var.b",
		"b": "var.c",
		"c": "var.a",
	}
	_, err := varexpand.Expand("x == var.a", vars)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestSiblingRepetitionIsNotCycle(t *testing.T) {
	// `a` referencing `b` twice is not a cycle.
	vars := map[string]string{
		"a": "var.b + var.b",
		"b": "1",
	}
	got, err := varexpand.Expand("x == var.a", vars)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "x == 1 + 1" {
		t.Fatalf("got %q", got)
	}
}

func TestMultipleVarsInOneScript(t *testing.T) {
	vars := map[string]string{"a": "1", "b": "2"}
	got, err := varexpand.Expand("var.a + var.b", vars)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "1 + 2" {
		t.Fatalf("got %q", got)
	}
}
