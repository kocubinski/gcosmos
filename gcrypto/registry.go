package gcrypto

import (
	"bytes"
	"fmt"
	"reflect"
)

// Prefixes are encoded as a fixed width.
const prefixSize = 8

// Registry is a runtime-defined registry to manage encoding and decoding
// a predetermined set of public key types.
type Registry struct {
	byType map[reflect.Type]string

	// For unmarshalling
	byPrefix map[string]NewPubKeyFunc
}

type NewPubKeyFunc func([]byte) (PubKey, error)

func (r *Registry) Register(name string, inst PubKey, newFn NewPubKeyFunc) {
	// TODO: validation on name.
	// TODO: fail if previously registered.

	if r.byPrefix == nil {
		r.byPrefix = map[string]NewPubKeyFunc{}
	}
	r.byPrefix[name] = newFn

	if r.byType == nil {
		r.byType = map[reflect.Type]string{}
	}
	r.byType[reflect.TypeOf(inst)] = name
}

func (r *Registry) Marshal(pubKey PubKey) []byte {
	var nameHeader [prefixSize]byte

	typ := reflect.TypeOf(pubKey)
	prefix, ok := r.byType[typ]
	if !ok {
		panic(fmt.Errorf("cannot marshal public key of type %s", typ))
	}

	copy(nameHeader[:], prefix)

	return append(nameHeader[:], pubKey.PubKeyBytes()...)
}

func (r *Registry) Unmarshal(b []byte) (PubKey, error) {
	// TODO: more validation against b
	prefix := bytes.TrimRight(b[:prefixSize], "\x00")

	fn := r.byPrefix[string(prefix)]
	if fn == nil {
		return nil, fmt.Errorf("no registered public key type for prefix %q", prefix)
	}

	return fn(b[prefixSize:])
}
