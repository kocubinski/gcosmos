package tmconsensus

import (
	"bytes"
	"slices"
	"sort"

	"github.com/rollchains/gordian/gcrypto"
)

type Validator struct {
	PubKey gcrypto.PubKey
	Power  uint64
}

// SortValidators sorts vs in-place, by power descending,
// and then by public key ascending.
func SortValidators(vs []Validator) {
	sort.Slice(vs, func(i, j int) bool {
		if vs[i].Power == vs[j].Power {
			return bytes.Compare(vs[i].PubKey.PubKeyBytes(), vs[j].PubKey.PubKeyBytes()) < 0
		}
		return vs[i].Power > vs[j].Power
	})
}

// ValidatorsToPubKeys returns a slice of just the public keys of vs.
func ValidatorsToPubKeys(vs []Validator) []gcrypto.PubKey {
	out := make([]gcrypto.PubKey, len(vs))
	for i, v := range vs {
		out[i] = v.PubKey
	}
	return out
}

// ValidatorsToVotePowers returns a slice of just the vote powers of vs.
func ValidatorsToVotePowers(vs []Validator) []uint64 {
	out := make([]uint64, len(vs))
	for i, v := range vs {
		out[i] = v.Power
	}
	return out
}

// ValidatorSlicesEqual reports whether the slices vs1 and vs2 are equivalent.
func ValidatorSlicesEqual(vs1, vs2 []Validator) bool {
	return slices.EqualFunc(vs1, vs2, func(v1, v2 Validator) bool {
		return v1.Power == v2.Power && v1.PubKey.Equal(v2.PubKey)
	})
}

// CanTrustValidators reports whether the validator set vs contains at least 1/3 voting power
// represented by the passed-in set of trusted public keys.
func CanTrustValidators(vs []Validator, pubKeys []gcrypto.PubKey) bool {
	trustedKeys := make(map[string]struct{}, len(pubKeys))
	for _, k := range pubKeys {
		trustedKeys[string(k.PubKeyBytes())] = struct{}{}
	}

	var totalPower, trustedPower uint64
	for _, v := range vs {
		totalPower += v.Power
		if _, trusted := trustedKeys[string(v.PubKey.PubKeyBytes())]; trusted {
			trustedPower += v.Power
		}
	}

	return trustedPower > ByzantineMinority(totalPower)+1
}
