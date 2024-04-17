package gcrypto

// SignatureProofMergeResult includes three important details that determine
// whether meaningful new information was learned from two signature proofs,
// and whether the message for the "other" proof should be propagated further
// or if the current/merged proof should be sent in its place.
//
// If AllValidSignatures was false, then the other message should not be propagated.
//
// IncreasedSignatures indicates whether the other proof had any signatures
// missing from the current proof.
// This does not indicate whether the current proof had any signatures
// missing from other.
type SignatureProofMergeResult struct {
	// Whether every signature in the "other" proof was valid.
	AllValidSignatures bool

	// Whether merging resulted in signatures we did not yet have.
	IncreasedSignatures bool

	// Was the "other" proof a strict superset of the current proof?
	WasStrictSuperset bool
}

// Combine returns a new SignatureProofMergeResult, the result of combining r and other.
// This is helpful for methods that combine multiple proofs, such as a prevote merge
// that must handle both active and nil prevotes.
func (r SignatureProofMergeResult) Combine(other SignatureProofMergeResult) SignatureProofMergeResult {
	return SignatureProofMergeResult{
		AllValidSignatures: r.AllValidSignatures && other.AllValidSignatures,

		IncreasedSignatures: r.IncreasedSignatures || other.IncreasedSignatures,

		WasStrictSuperset: r.WasStrictSuperset && other.WasStrictSuperset,
	}
}
