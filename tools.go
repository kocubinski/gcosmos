//go:build tools

// For the tools.go pattern, see:
// https://go.dev/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module

package gordian

import (
	// For stringer, used in a few go:generate calls.
	_ "golang.org/x/tools/cmd/stringer"
)
