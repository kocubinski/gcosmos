// Package gdatapool contains interfaces for both providing and retrieving block data.
//
// From Gordian's perspective, the block data is only a byte slice.
// It is the driver's responsibility to inform the data pool how to translate
// the raw bytes into a meaningful value.
package gdatapool
