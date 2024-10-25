package glog

import "log/slog"

// HR returns a copy of log that includes fields for the given height and round.
//
// This is a convenient shorthand in many log calls where
// the height and round are pertinent details.
func HR(log *slog.Logger, height uint64, round uint32) *slog.Logger {
	return log.With("height", height, "round", round)
}

// HR returns a copy of log that includes fields for the given height, round, and error.
//
// This is a convenient shorthand in many log calls where
// the height, round, and error are pertinent details.
func HRE(log *slog.Logger, height uint64, round uint32, e error) *slog.Logger {
	return log.With("height", height, "round", round, "err", e)
}
