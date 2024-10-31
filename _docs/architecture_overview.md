# gcosmos Architecture Overview

gcosmos is the "driver" connecting the [Gordian consensus engine](https://github.com/gordian-engine/gordian)
and [the Cosmos SDK](https://github.com/cosmos/cosmos-sdk).

It may help to refer to the
[Gordian architecture overview](https://github.com/gordian-engine/gordian/blob/main/_docs/architecture_overview.md).

## Preface

This document will start from `main.go` and work inwards,
examining individual components that constitute the whole driver.
Therefore this document will be useful to people who want to understand
how gcosmos is composed and how it works.
It will also be useful to people who want to implement a driver
targeting something other than the Cosmos SDK.

The document may also be helpful for people developing their own consensus component
against the Cosmos SDK's server/v2 architecture.
(We will mention server/v2 several times throughout this document;
it is an updated internal architecture for the SDK that is intended to
allow integrating alternate consensus engines and to simplify other tasks.)

gcosmos is still in very active development.
The nature of documentation is to become outdated,
so be ready to cross-reference this document with the actual source code.

Some of the subcomponents' implementations were chosen for simplicity during initial integration.
Those will be noted where relevant.

gcosmos is written in [Go](https://go.dev),
and we will assume the reader is familiar with the language and its terminology and basic patterns.
For brevity when referring to import paths within gcosmos,
we will use `gcosmos/foo` as shorthand instead of the fully qualified `github.com/gordian-engine/gcosmos/foo`.
Occasionally wht import paths we will use `gordian` as shorthand for `github.com/gordian-engine/gordian`.
And when we say "SDK", we are referring to the Cosmos SDK.

Lastly, the output from building the gcosmos repository
is a `gcosmos` binary, which is a `simapp` instance backed by Gordian providing the consensus layer.

## Command-line construction

In `main.go`, we call `gcosmos/internal/gci.NewGcosmosCommand`.

The SDK is hardcoded to use [Cobra](https://github.com/spf13/cobra),
a popular Go library for managing command trees
(think subcommands within a single executable, like `git init` or `git stash pop`).
It is also hardcoded to use [Viper](https://github.com/spf13/viper),
another popular Go library, intended to manage configuration and command line flags.

The SDK's server/v2 architecture wants to own the entire command line,
so we have to provide our `cobra.Command` values to be injected into the command tree.

There is a lot of subtlety within the implementation of `NewGcosmosCommand`,
relying on very particular setup and ordering.
The outcome is that we provide the implementation of `gcosmos start`,
and we also provide a tree of commands under `gcosmos gordian`.
As of writing, we provide `gcosmos gordian seed` which runs a [libp2p](https://libp2p.io/) seed node
so that validators can connect to the known seed address
and automatically discover peers.

The primary detail to note in all of this,
is that we provide a `gcosmos/gserver.Component` as the `Consensus` value
to the simapp `CommandDependencies`,
and that is how the SDK decides to associate our component with the `gcosmos start` command.

## `gserver.Component`

The `gcosmos/gserver.Component` type is how the SDK begins to interact with the consensus layer.
As the SDK's server/v2 architecture initializes, it calls the `Start` method.

`Start` handles setup for the underlying
[`github.com/gordian-engine/gordian/tm/tmengine.Engine`](https://pkg.go.dev/github.com/gordian-engine/gordian/tm/tmengine#Engine)
instance; we will not discuss that setup in any depth in this document.

In addition to the engine setup, the `Component` initializes several other types:

- `gcosmos/gserver/internal/gp2papi.DataHost` provides header and block data to other peers over libp2p
- `gordian/gdriver/gtxbuf.Buffer` manages a local ordered set of transactions that we use as a kind of local mempool
- `gcosmos/gserver/internal/gsi.TxManager` handles the integration of the SDK application with the transaction buffer
- `gcosmos/gserver/internal/gp2papi.CatchupClient` integrates with the Engine to fetch headers and block data from other peers'
  `DataHost` endpoints over libp2p, so that if we detect that our validator has fallen behind,
  we can retrieve the missing data from peers
- `gcosmos/gserver/internal/gsi.Driver` integrates directly with the Engine, providing the external interfaces the Engine expects
- `gcosmos/gserver/internal/gsi.PBDRetriever` retrieves proposed block data from proposal annotations
  (this is a different mechanism from the general block data fetching)
- `gcosmos/gserver/internal/gsi.ConsensusStrategy` determines how to propose blocks and how to vote on incoming proposals
