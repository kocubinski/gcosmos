package tmlibp2p

import (
	"context"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	p2phost "github.com/libp2p/go-libp2p/core/host"
)

// Host is a libp2p host and a pubsub connection.
type Host struct {
	h p2phost.Host

	ps *pubsub.PubSub
}

// HostOptions holds libp2p configuration for the host and pubsub value.
type HostOptions struct {
	// Options are passed when creating a new libp2p host
	// (which is lower level than the Host type in this tmlibp2p package).
	Options []libp2p.Option

	// Currently PubSubOptions are always applied to NewGossipSub.
	PubSubOptions []pubsub.Option
}

func NewHost(ctx context.Context, opts HostOptions) (*Host, error) {
	h, err := libp2p.New(opts.Options...)
	if err != nil {
		return nil, err
	}

	ps, err := pubsub.NewGossipSub(ctx, h, opts.PubSubOptions...)
	if err != nil {
		return nil, err
	}

	return &Host{
		h:  h,
		ps: ps,
	}, nil
}

// Libp2pHost returns the underlying libp2p host value.
func (h *Host) Libp2pHost() p2phost.Host {
	return h.h
}

// PubSub returns the underlying libp2p pubsub value.
func (h *Host) PubSub() *pubsub.PubSub {
	return h.ps
}

// Close closes the underlying libp2p host and returns its error.
func (h *Host) Close() error {
	return h.h.Close()
}
