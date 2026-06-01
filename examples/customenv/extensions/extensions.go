// Package extensions shows how a downstream service ships its own CEL
// variables and functions on top of Celsius' DefaultEnv. The same Env() is
// consumed by the server (examples/customenv/server) and the CLI wrapper
// (examples/customenv/cli), so production behavior and pipeline validation
// stay in sync.
//
// This is the canonical pattern — see docs/custom-env.md for the full
// walkthrough.
package extensions

import (
	"github.com/amkarkhi/celsius"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// internalUIDs is a tiny allow-list used by the is_internal() helper. Real
// code would read this from a config source.
var internalUIDs = map[int64]struct{}{
	1: {}, 2: {}, 3: {}, 42: {},
}

// IsInternal is a CEL function: is_internal(uid int) -> bool. It returns
// true when uid is in the hardcoded allow-list.
func IsInternal() cel.FunctionOpt {
	return cel.Overload("is_internal_int",
		[]*cel.Type{cel.IntType}, cel.BoolType,
		cel.UnaryBinding(func(v ref.Val) ref.Val {
			n, ok := v.(types.Int)
			if !ok {
				return types.NewErr("is_internal: expected int, got %v", v.Type())
			}
			_, ok = internalUIDs[int64(n)]
			return types.Bool(ok)
		}),
	)
}

// Tier is a CEL function: tier(uid int) -> string. It returns "vip" for
// uids above 100, otherwise "regular".
func Tier() cel.FunctionOpt {
	return cel.Overload("tier_int",
		[]*cel.Type{cel.IntType}, cel.StringType,
		cel.UnaryBinding(func(v ref.Val) ref.Val {
			n, ok := v.(types.Int)
			if !ok {
				return types.NewErr("tier: expected int, got %v", v.Type())
			}
			if int64(n) > 100 {
				return types.String("vip")
			}
			return types.String("regular")
		}),
	)
}

// Env returns the EnvBuilder this service uses everywhere: built-in helpers
// plus our custom variables and functions. The server feeds it to
// celsius.New; the CLI wrapper feeds it to clikit.Run.
func Env() *celsius.EnvBuilder {
	return celsius.DefaultEnv().
		WithVariable("uid", cel.IntType).
		WithVariable("country", cel.StringType).
		WithFunction("is_internal", IsInternal()).
		WithFunction("tier", Tier())
}
