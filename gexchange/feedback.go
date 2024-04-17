package gexchange

// Feedback is an indicator sent back to the p2p layer
// to give feedback about a particular message.
//
// Depending on the p2p implementation,
// feedback may increase a peer's score, giving preference to that peer,
// or it may reduce a peer's score, eventually rejecting
// all future messages from that peer.
type Feedback uint8

// Valid feedback values.
//
//go:generate go run golang.org/x/tools/cmd/stringer -type Feedback -trimprefix=Feedback
const (
	// FeedbackUnspecified is the zero value for Feedback.
	// Returning FeedbackUnspecified is a bug.
	FeedbackUnspecified Feedback = iota

	// FeedbackAccepted indicates that the input was valid
	// and that the message should continue to propagate.
	FeedbackAccepted

	// FeedbackRejected indicates that the input was invalid,
	// the message should not be propagated,
	// and the sender should be penalized.
	FeedbackRejected

	// FeedbackIgnored indicates that the input was invalid;
	// although we are not going to propagate this message,
	// in contrast to FeedbackRejected, we will not penalize the sender.
	FeedbackIgnored

	// FeedbackRejectAndDisconnect indicates that the input was invalid
	// and appeared to be malicious.
	// The message will not be propagated,
	// and no future messages will be sent to that peer.
	FeedbackRejectAndDisconnect
)
