# Architecture Overview

This document is an overview of the Gordian core architecture,
starting at a very high level and drilling down.

## Terminology

Terms to understand in this document, organized alphabetically:

- **Consensus Strategy**: Driver subcomponent responsible for deciding whether to propose a block,
  and how to prevote or precommit for a particular block header.
- **Driver**: Application-specific components to communicate with the core engine.
  There are several standard underlying types within a typical driver.
- **Engine**: The primary type to consume when using the Gordian.
  This contains several underlying types.
- **Gossip strategy**: Subcomponent of the Mirror.
  Determines specifics of broadcasting messages to peers on the p2p network.
- **Mirror**: Engine component that handles incoming and outgoing p2p traffic.
  Responsible for playing messages to the state machine in the correct order.
  See the Mirror section under Engine Internals.
- **p2p network**: A mesh network of remote peers, which is by default untrusted.
- **State machine**: Engine component responsible for accepting updates from the mirror,
  communicating directly with the Driver, and reporting certain state updates back to the mirror.
  See the State Machine section under Engine Internals.

## High level overview

At a _very_ high level, the Gordian core engine sits between the p2p network
and the Driver.

```
┌──────────────────────────────────────┐
│             p2p network              │
└──────────────────────────────────────┘
                    ▲
                    │
                    ▼
┌──────────────────────────────────────┐
│                                      │
│                Engine                │
│                                      │
└──────────────────────────────────────┘
                    ▲
                    │
                    ▼
┌──────────────────────────────────────┐
│                Driver                │
└──────────────────────────────────────┘
```

The engine is comprised of two major components --
the Mirror and the State Machine:

```
 ┌─────────────────────────────────────┐
 │             p2p network             │
 └─────────────────────────────────────┘
                    ▲
                    │
                    │
┌───────────────────┼──────────────────┐
│Engine             │                  │
│                   ▼                  │
│  ┌────────────────────────────────┐  │
│  │             Mirror             │  │
│  └────────────────────────────────┘  │
│                   ▲                  │
│                   │                  │
│                   ▼                  │
│  ┌────────────────────────────────┐  │
│  │         State Machine          │  │
│  └────────────────────────────────┘  │
│                   ▲                  │
│                   │                  │
└───────────────────┼──────────────────┘
                    │
                    │
                    ▼
┌──────────────────────────────────────┐
│                Driver                │
└──────────────────────────────────────┘
```

## Engine internals

### Mirror

The Mirror handles incoming messages from the p2p network and sequences them correctly for the State Machine.
It is aware of both the height and round being currently voted,
and the height and round which has sufficient precommits that is being "committed".
"Committed" in this case means, we know the header and content of the block that will be committed to chain,
and we have >2/3 voting power in favor of that header,
but we don't know specifically which precommits will be committed to chain until the voting block enters committing.

The Mirror is aware of the State Machine's current height and round,
and the Mirror can tolerate the State Machine lagging the network by an arbitrary amount.
The Mirror can also be run without a State Machine, such that it only tracks block headers.

As the Mirror receives new proposed blocks or votes,
it sends its current state to the Gossip Strategy.
The Gossip Strategy is responsible for determining exactly how to send those updates to the rest of the p2p network.
Suppose the Mirror receives a prevote from Validator 1, and updates the Gossip Strategy;
then the Mirror receives a prevote from Validator 2, and updates the Gossip Strategy again.
A "chatty" gossip strategy could broadcast `Vote(1)` first, and then broadcast `Vote(1,2)`.
A more sophisticated gossip strategy could broadcast `Vote(1)` and then only broadcast `Vote(2)`,
under the assumption that the `Vote(1)` broadcast was successful.

If the State Machine is on the same Voting or Committing height
that the Mirror thinks the rest of the network is on,
then proposed headers and vote information from the State Machine are treated the same as
any other update arriving from the network.

```
┌───────────────────────────────────┐
│            p2p network            │◀────────────┐
└───────────────────────────────────┘             │
                  │                               │
                  │             ┌───────────────────────────────────┐
                  │             │          Gossip Strategy          │
                  │             └───────────────────────────────────┘
                  │                               ▲
                  ▼                               │
┌───────────────────────────────────┐             │
│              Mirror               │─────────────┘
└───────────────────────────────────┘
                  ▲
                  │
                  │
                  ▼
 ┌────────────────────────────────┐
 │         State Machine          │
 └────────────────────────────────┘
 ```

 ### State Machine

 The State Machine receives updates from the mirror and interacts with the Driver.

(Note that some terms here assume familiarity with the Tendermint® consensus algorithm;
refer to [the whitepaper](https://arxiv.org/abs/1807.04938) and other references if you are unfamiliar.)

When the State Machine enters a round, it first informs the Mirror,
and the Mirror reports back any known state for the round.
The State Machine uses that information from the Mirror to decide what "step" to take within the round.

If that round is currently in voting or committing,
then the State Machine informs the Consensus Strategy of the round entrance.
The Consensus Strategy is a subcomponent of the Driver.
Depending on whether majority prevotes were present in the initial round information,
the State Machine may or may not allow the Consensus Strategy to propose a header,
and it may "allow" the Consensus Strategy to pick which block to prevote,
or it may "force" the Consensus Strategy to pick a block to prevote or precommit
with the infomration available at that point in time.

Any information the State Machine gains from the Consensus Strategy is passed back to the Mirror.

The state machine is also responsible for managing certain timeouts during the Tendermint algorithm,
such as providing ample time for a proposed header to arrive,
and waiting for a certain duration after sufficient precommits are received,
in order to allow late precommits to arrive.

When the State Machine reaches the end of a round, it requests that the Driver "finalizes" the block.
Finalization determines a new application state hash and any validator set updates.
Because the finalization for blocks is stored separately from other header data,
and because the mirror operates only on headers,
a nondeterministic or otherwise buggy finalization that diverges from the rest of the network
can be quickly detected as the mirror continues to track headers from other validators.

```
┌─────────────────────────────────┐
│             Mirror              │
└─────────────────────────────────┘
                 ▲
                 │
                 │
                 ▼
┌─────────────────────────────────┐      Proposals and Votes
│          State Machine          │◀─────────┐
└─────────────────────────────────┘          │
                 ▲                           │
                 │                           │
                 │       ┌───────────────────┼──────────────────┐
                 │       │Driver             │                  │
   Finalizations │       │                   ▼                  │
                 │       │  ┌────────────────────────────────┐  │
                 └──────▶│  │       Consensus Strategy       │  │
                         │  └────────────────────────────────┘  │
                         │                                      │
                         └──────────────────────────────────────┘
```
