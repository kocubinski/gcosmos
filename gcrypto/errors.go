package gcrypto

import "errors"

var ErrInvalidSignature = errors.New("signature could not be verified")

var ErrUnknownKey = errors.New("unknown key")
