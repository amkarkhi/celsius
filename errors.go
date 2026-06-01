package celsius

import "errors"

// ErrNoMatch is returned by [Engine.Match] when no rule in the group
// evaluated to true.
var ErrNoMatch = errors.New("celsius: no rule matched")

// ErrNoRuleGroup is returned by [Engine.Match] when the named rule group
// does not exist in the loaded configuration.
var ErrNoRuleGroup = errors.New("celsius: rule group not found")

// ErrClosed is returned when an operation is performed on a closed engine.
var ErrClosed = errors.New("celsius: engine is closed")
