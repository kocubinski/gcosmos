# Gordian

Gordian is a modular, [Byzantine-fault-tolerant](https://en.wikipedia.org/wiki/Byzantine_fault) consensus engine.

A consensus engine is a [replicated state machine](https://en.wikipedia.org/wiki/State_machine_replication) --
a participant in a distributed system where the participants agree on initial state, actions to apply, and resulting state.

Almost anything that a chain developer would need to tune is able to be modified in Gordian:

- storage layer
- p2p networking and gossiping
- hashing algorithms
- cryptographic key types and how signatures are calculated

## Try it out

Clone the repository and `cd` into it.
Make sure you have [the most recent version of Go installed](https://go.dev/dl/).
Run `./demo-echo.bash start 4` to start a local network of 4 validators,
running a toy application where the network participants agree on the current height and round.
