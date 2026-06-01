// Package celfn contains the implementations of Celsius' default CEL
// helper functions. It is an internal package; consumers reach them via
// celsius.DefaultEnv().
package celfn

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"math/rand"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// MD5 hashes a string to its 32-char lowercase hex MD5 digest.
func MD5() cel.FunctionOpt {
	return cel.Overload("md5_string",
		[]*cel.Type{cel.StringType},
		cel.StringType,
		cel.UnaryBinding(func(val ref.Val) ref.Val {
			str, ok := val.(types.String)
			if !ok {
				return types.NewErr("unexpected type '%v' passed to md5", val.Type())
			}
			sum := md5.Sum([]byte(str))
			return types.String(hex.EncodeToString(sum[:]))
		}),
	)
}

// HashString CRC32-hashes a string and returns the result modulo the
// second argument (when used as the two-arg overload registered as `hash`).
func HashString() cel.FunctionOpt {
	return cel.Overload("hash_string",
		[]*cel.Type{cel.StringType, cel.IntType},
		cel.IntType,
		cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
			s, ok := lhs.(types.String)
			if !ok {
				return types.NewErr("unexpected type '%v' passed to hash", lhs.Type())
			}
			n, ok := rhs.(types.Int)
			if !ok {
				return types.NewErr("unexpected type '%v' passed to hash", rhs.Type())
			}
			if int64(n) == 0 {
				return types.NewErr("hash modulus must be non-zero")
			}
			return types.Int(int64(crc32.ChecksumIEEE([]byte(s))) % int64(n))
		}),
	)
}

func hashNumeric(val, mod ref.Val) ref.Val {
	n, ok := mod.(types.Int)
	if !ok {
		return types.NewErr("unexpected modulus type '%v' passed to hash", mod.Type())
	}
	if int64(n) == 0 {
		return types.NewErr("hash modulus must be non-zero")
	}
	switch val.Type() {
	case cel.IntType, cel.UintType, cel.DoubleType:
		return types.Int(int64(crc32.ChecksumIEEE(fmt.Append(nil, val.Value()))) % int64(n))
	default:
		return types.NewErr("unexpected type '%v' passed to hash", val.Type())
	}
}

// HashInt is the int overload of hash.
func HashInt() cel.FunctionOpt {
	return cel.Overload("hash_int",
		[]*cel.Type{cel.IntType, cel.IntType},
		cel.IntType,
		cel.BinaryBinding(hashNumeric),
	)
}

// HashUint is the uint overload of hash.
func HashUint() cel.FunctionOpt {
	return cel.Overload("hash_uint",
		[]*cel.Type{cel.UintType, cel.IntType},
		cel.IntType,
		cel.BinaryBinding(hashNumeric),
	)
}

// ToStr stringifies an int / uint / double.
func ToStr() cel.FunctionOpt {
	return cel.Overload("to_str_num",
		[]*cel.Type{cel.DynType},
		cel.StringType,
		cel.UnaryBinding(func(val ref.Val) ref.Val {
			switch val.Type() {
			case cel.IntType, cel.UintType, cel.DoubleType:
				return types.String(fmt.Sprint(val.Value()))
			default:
				return types.NewErr("unexpected type '%v' passed to to_str", val.Type())
			}
		}),
	)
}

// Contains is a binary string-contains predicate.
func Contains() cel.FunctionOpt {
	return cel.Overload("contains_string_string",
		[]*cel.Type{cel.StringType, cel.StringType},
		cel.BoolType,
		cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
			a, aok := lhs.(types.String)
			b, bok := rhs.(types.String)
			if !aok || !bok {
				return types.NewErr("unexpected types '%v','%v' passed to contains", lhs.Type(), rhs.Type())
			}
			return types.Bool(strings.Contains(string(a), string(b)))
		}),
	)
}

// Rand returns a pseudo-random double in [0,1).
func Rand() cel.FunctionOpt {
	return cel.Overload("rand_double",
		nil,
		cel.DoubleType,
		cel.FunctionBinding(func(args ...ref.Val) ref.Val {
			return types.Double(rand.Float64())
		}),
	)
}

// Replace performs strings.ReplaceAll.
func Replace() cel.FunctionOpt {
	return cel.Overload("replace_string_string_string",
		[]*cel.Type{cel.StringType, cel.StringType, cel.StringType},
		cel.StringType,
		cel.FunctionBinding(func(args ...ref.Val) ref.Val {
			if len(args) != 3 {
				return types.NewErr("replace: expected 3 args, got %d", len(args))
			}
			s, sok := args[0].(types.String)
			o, ook := args[1].(types.String)
			n, nok := args[2].(types.String)
			if !sok || !ook || !nok {
				return types.NewErr("replace: all args must be strings")
			}
			return types.String(strings.ReplaceAll(string(s), string(o), string(n)))
		}),
	)
}
