package tmconsensus

import (
	"fmt"

	"github.com/rollchains/gordian/gcrypto"
)

// SparseSignatureCollection is a tm-specific,
// network- and store-optimized collection of [gcrypto.SparseSignature].
//
// A [gcrypto.SparseSignatureProof] is one public key hash
// with a collection of signatures,
// without a clear relationship to any particular block hash.
// Using a map[string]gcrypto.SparseSignatureProof
// requires the caller to verify every public key hash in every proof.
// So instead, with the SparseSignatureCollection,
// we raise the public key hash up a level so it only exists once.
//
// This type lives in tmconsensus instead of gcrypto
// due to its larger awareness of block hashes,
// which are outside the scope of the gcrypto package.
type SparseSignatureCollection struct {
	// There is exactly one public key hash
	// the keys and signatures in the entire collection.
	// Consumers must assume this slice is shared across many goroutines,
	// and therefore must never modify the slice.
	PubKeyHash []byte

	// Mapping of block hash to sparse signatures.
	// The empty string represents a vote for nil.
	BlockSignatures map[string][]gcrypto.SparseSignature
}

// ToFullPrevoteProofMap converts c
// to a map of block hashes to full proofs.
// Note that the returned map will always include an empty key,
// because internal components of the engine expect a full nil proof
// to always be present.
func (c SparseSignatureCollection) ToFullPrevoteProofMap(
	height uint64,
	round uint32,
	valSet ValidatorSet,
	sigScheme SignatureScheme,
	cmspScheme gcrypto.CommonMessageSignatureProofScheme,
) (map[string]gcrypto.CommonMessageSignatureProof, error) {
	out, err := c.toFullProofMap(
		height, round,
		valSet,
		cmspScheme,
		func(vt VoteTarget) ([]byte, error) {
			return PrevoteSignBytes(vt, sigScheme)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build prevote proof map: %w", err)
	}
	return out, nil
}

// ToFullPrecommitProofMap converts c
// to a map of block hashes to full proofs.
// Note that the returned map will always include an empty key,
// because internal components of the engine expect a full nil proof
// to always be present.
func (c SparseSignatureCollection) ToFullPrecommitProofMap(
	height uint64,
	round uint32,
	valSet ValidatorSet,
	sigScheme SignatureScheme,
	cmspScheme gcrypto.CommonMessageSignatureProofScheme,
) (map[string]gcrypto.CommonMessageSignatureProof, error) {
	out, err := c.toFullProofMap(
		height, round,
		valSet,
		cmspScheme,
		func(vt VoteTarget) ([]byte, error) {
			return PrecommitSignBytes(vt, sigScheme)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build precommit proof map: %w", err)
	}
	return out, nil
}

func (c SparseSignatureCollection) toFullProofMap(
	height uint64,
	round uint32,
	valSet ValidatorSet,
	cmspScheme gcrypto.CommonMessageSignatureProofScheme,
	signBytesFunc func(VoteTarget) ([]byte, error),
) (map[string]gcrypto.CommonMessageSignatureProof, error) {
	valPubKeys := ValidatorsToPubKeys(valSet.Validators)
	pubKeyHash := string(valSet.PubKeyHash)

	// Components of the consensus engine expect the nil proof to be populated,
	// even if there are not yet any votes for nil.
	//
	// I think my preference for populating this would be to concatenate
	// the iterator over c.BlockSignatures with a map containing only an empty string key.
	//
	// Redoing the loop iteration once seems like the best compromise,
	// while there is no standard library iter.Seq2 concatenator.
	//
	// The other alternatives would have been:
	//   - hardcoding a separate iteration
	//   - modifying the original, or a clone of, ssc.BlockSignatures
	//   - extracting the inside of the loop to another function
	_, hasNilProof := c.BlockSignatures[""]
	blockSigMap := c.BlockSignatures
	if len(blockSigMap) == 0 {
		// If the length was zero, we wouldn't do any iterations,
		// and we would return an empty map.
		// So handle that special case by just explicitly
		// iterating over a map with that one key,
		// again so we do not have to mutate the input.
		blockSigMap = map[string][]gcrypto.SparseSignature{
			"": nil,
		}
		hasNilProof = true
	}

	sizeHint := len(c.BlockSignatures)
	if !hasNilProof {
		sizeHint++
	}
	out := make(map[string]gcrypto.CommonMessageSignatureProof, sizeHint)

	vt := VoteTarget{Height: height, Round: round}
	for hash, sparseSigs := range blockSigMap {
	REDO_FOR_NIL_PROOF:
		vt.BlockHash = hash
		content, err := signBytesFunc(vt)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to build sign bytes for block hash %x: %w", hash, err,
			)
		}
		out[hash], err = cmspScheme.New(content, valPubKeys, pubKeyHash)
		if err != nil {
			return nil, fmt.Errorf("failed to get initial proof for %x: %w", hash, err)
		}

		if len(sparseSigs) > 0 {
			sparseProof := gcrypto.SparseSignatureProof{
				PubKeyHash: pubKeyHash,
				Signatures: sparseSigs,
			}
			mergeRes := out[hash].MergeSparse(sparseProof)
			if !mergeRes.AllValidSignatures || !mergeRes.IncreasedSignatures {
				panic(fmt.Errorf(
					"BUG: invalid result after merging signatures: all_valid=%t, increased=%t (nSigs=%d signBytes=%q)",
					mergeRes.AllValidSignatures, mergeRes.IncreasedSignatures,
					len(sparseSigs),
					content,
				))
			}
		} else {
			if hash != "" {
				panic(fmt.Errorf("BUG: saw len(sparseSigs) == 0 for hash=%x", hash))
			}
		}

		if !hasNilProof {
			// Redo one iteration of the loop to add in the required nil hash.
			hash = ""
			sparseSigs = nil
			hasNilProof = true

			goto REDO_FOR_NIL_PROOF
		}
	}

	return out, nil
}
