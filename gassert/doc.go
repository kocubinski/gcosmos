// Package gassert (Gordian assert) provides functionality around assertions at runtime.
//
// It is assumed to be prohibitively expensive to validate every invariant
// at every function entrypoint in production.
// But, if unexpected behavior is observed, enabling invariant checks
// may immediately reveal the problem.
//
// To enable invariant checks is a two-step process.
// First, the assertion functionality is not compiled into Gordian code by default.
// To enable assertions, you must build with the "debug" build tag,
// i.e. use "go build -tags debug" or "go run -tags debug".
// Second, you must enable some set of assertions,
// by producing an [Env] via the [EnvironmentFromString] or [ParseEnvironment] functions
// (which are only available in debug builds).
// A fully developed chain using Gordian should support the debug build tag
// and allow setting rules from the command line.
// But if not, you probably have to build from source anyway,
// so you can just manually set the rules when the [Env] is created.
//
// Rule behavior is as follows:
//   - Individual components call [*Environment.Enabled] to determine whether to make the assertion.
//     They provide a dot-separated string indicating the path of the assertion they may make.
//   - The assertion environment checks the path against the registered rules.
//   - No rules are enabled by default.
//   - A top level rule of "*" (wildcard) enables all assertions.
//   - The "*" wildcard may only occur as the last segment of a dot-separated rule,
//     so "foo.bar.*" is valid but "foo.*.bar" is not.
//   - A rule with a leading "!" excludes certain exact matches from a wildcard rule,
//     so "foo.bar.*,!foo.bar.baz" would match "foo.bar.quux" but not "foo.bar.baz".
//   - Exact match rules are also accepted, so "foo.bar.baz" would match that exact rule
//     but not "foo.bar.baz_quux"
//   - The rules expect plain ASCII words between dots, with no special symbols
//     other than dash or underscore.
//     This is not currently enforced but may be in the future.
//   - The [EnvironmentFromString] function expects a comma-separated list of rules.
//   - The [ParseEnvironmentFromString] function operates on an [io.Reader],
//     and it ignores blank lines and any comment lines whose first character is "#".
package gassert
