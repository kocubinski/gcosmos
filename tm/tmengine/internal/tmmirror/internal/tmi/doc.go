// Package tmi is the internal package for tmmirror.
//
// The tmmirror package wants to only expose the core Mirror type,
// but there are many other types that have wide scope,
// so organizing them into an internal package for finer control
// over what is exported and what isn't, is appropriate here.
package tmi
