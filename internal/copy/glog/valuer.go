package glog

import (
	"fmt"
	"log/slog"
)

// Hex wraps a byte slice to ensure it serializes as a hex-encoded string.
// Without this, it gets rendered as a Unicode string with embedded escape codes.
type Hex []byte

func (v Hex) LogValue() slog.Value {
	return slog.StringValue(fmt.Sprintf("%x", v))
}
