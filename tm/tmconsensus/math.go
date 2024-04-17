package tmconsensus

import "errors"

// ByzantineMajority returns the minimum value to exceed 2/3 of n.
// Use should always involve >= comparison, not >.
// For example, 2/3 of 12 is 8, so ByzantineMajority(12) = 9.
// Similarly, 2/3 of 10 is 6+2/3, so ByzantineMajority(10) = 7.
//
// ByzantineMajority(0) panics.
func ByzantineMajority(n uint64) uint64 {
	if n == 0 {
		panic(errors.New("ByzantineMajority: n must be positive"))
	}

	quo, rem := n/3, n%3
	if rem < 2 {
		return 2*quo + 1
	}
	return 2*quo + 2
}

// ByzantineMinority returns the minimum value to reach 1/3 of n.
// Use should always involve >= comparison, not >.
//
// For votes that are simply in favor or against a binary decision,
// reaching 1/3 means it is possible to reach a majority decision;
// unless both the for and against votes have reached the minority,
// in which case it is impossible for one vote to reach the majority.
//
// ByzantineMinority(0) panics.
func ByzantineMinority(n uint64) uint64 {
	if n == 0 {
		panic(errors.New("ByzantineMinority: n must be positive"))
	}

	quo, rem := n/3, n%3
	if rem == 0 {
		return quo
	}

	return quo + 1
}
