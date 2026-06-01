// Package varexpand resolves `var.NAME` references inside rule scripts.
//
// Expansion is iterative — a variable's value may itself contain `var.X`
// references. Cycles are detected and reported as errors rather than
// causing infinite loops.
package varexpand

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var varRef = regexp.MustCompile(`var\.(\w+)`)

// Expand replaces every `var.NAME` reference in script with the matching
// entry from vars. Unknown references are left untouched. If a cycle is
// detected (e.g. a → b → a), Expand returns an error naming the cycle.
func Expand(script string, vars map[string]string) (string, error) {
	return expand(script, vars, nil)
}

func expand(script string, vars map[string]string, stack []string) (string, error) {
	var firstErr error
	out := varRef.ReplaceAllStringFunc(script, func(match string) string {
		if firstErr != nil {
			return match
		}
		name := strings.TrimPrefix(match, "var.")
		val, ok := vars[name]
		if !ok {
			return match
		}
		if slices.Contains(stack, name) {
			firstErr = fmt.Errorf("variable expansion cycle: %s", strings.Join(append(stack, name), " -> "))
			return match
		}
		expanded, err := expand(val, vars, append(stack, name))
		if err != nil {
			firstErr = err
			return match
		}
		return expanded
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}
