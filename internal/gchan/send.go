// Package gchan contains helpers for common operations with channels.
// The helpers use consistent log formatting to save some boilerplate where used.
package gchan

import (
	"context"
	"log/slog"
)

// SendC selects between ctx.Done and sending val to out.
// If ctx is canceled before the send to out completes,
// SendC logs the message "Context canceled while " + canceledDuring,
// and it reports false.
// Otherwise, val is successfully sent to out, and the function reports true.
func SendC[T any](ctx context.Context, log *slog.Logger, out chan<- T, val T, canceledDuring string) (sent bool) {
	select {
	case <-ctx.Done():
		log.Info("Context canceled while "+canceledDuring, "cause", context.Cause(ctx))
		return false
	case out <- val:
		return true
	}
}

// RecvC selects between ctx.Done and receiving from in.
// If ctx is canceled before the receive from in completes,
// RecvC logs the message "Context canceled while " + canceledDuring,
// and it returns the zero value of T and reports false.
// Otherwise, the received value is returned and the function reports true.
func RecvC[T any](ctx context.Context, log *slog.Logger, in <-chan T, canceledDuring string) (val T, received bool) {
	select {
	case <-ctx.Done():
		log.Info("Context canceled while "+canceledDuring, "cause", context.Cause(ctx))
		return val, false
	case val := <-in:
		return val, true
	}
}

// ReqResp performs a blocking send of reqValue to reqChan,
// then waits to receive a value from respChan.
// If ctx is canceled during either operation,
// it returns the zero value of U and returns false.
//
// This is a useful shorthand for synchronous request-responses.
func ReqResp[T, U any](
	ctx context.Context, log *slog.Logger,
	reqChan chan<- T, reqValue T,
	respChan <-chan U,
	reqRespType string,
) (respVal U, ok bool) {
	if !SendC(ctx, log, reqChan, reqValue, "making "+reqRespType+" request") {
		return respVal, false
	}

	return RecvC(ctx, log, respChan, "receiving "+reqRespType+" response")
}
