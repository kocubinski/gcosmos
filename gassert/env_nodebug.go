//go:build !debug

package gassert

// Env is the environment, a pseudo-global state indicating
// which assertions or debugers are enabled.
//
// Core engine types that support assertions
// should always accept a gassert.Env.
// In non-debug builds, Env is an empty struct so as to not consume any memory.
// In debug builds, Env is a type alias to *Environment
// (a type which is only compiled into debug builds).
//
// The non-debug Env deliberately does not have any defined methods.
// User code depending on the assertion environment should also be guarded
// behind the build tag "debug".
type Env struct{}
