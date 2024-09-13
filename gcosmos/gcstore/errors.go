package gcstore

import (
	"errors"
	"fmt"
)

type AlreadyHaveBlockDataForHeightError struct {
	Height uint64
}

func (e AlreadyHaveBlockDataForHeightError) Error() string {
	return fmt.Sprintf("already have block data for height %d", e.Height)
}

type AlreadyHaveBlockDataForIDError struct {
	ID string
}

func (e AlreadyHaveBlockDataForIDError) Error() string {
	return fmt.Sprintf("already have block data for id %q", e.ID)
}

var ErrBlockDataNotFound = errors.New("block data not found")

func IsAlreadyHaveBlockDataError(e error) bool {
	return errors.As(e, new(AlreadyHaveBlockDataForHeightError)) ||
		errors.As(e, new(AlreadyHaveBlockDataForIDError))
}
