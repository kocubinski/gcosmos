# gcosmos

This is a temporary workspace for integrating Gordian with [the Cosmos SDK](https://github.com/cosmos/cosmos-sdk).
As Gordian core reaches a stable release, the gcosmos tree will move to its own repository.

## Setup

We are currently using a local, unmodified clone of the SDK in tandem with Go workspaces.
It is a bit unconventional to commit a Go workspace file, but while both Gordian and the Cosmos SDK
are being actively changed, a fixed Go workspace fits well for now.

From the `gcosmos` directory, run `./_cosmosvendor/sync_sdk.bash` to clone or fetch and checkout
a "known working" version of the Cosmos SDK compatible with the current gcosmos tree.
You may need to run `go work sync` from the `gcosmos` directory again.

## Running

This currently adds one new subcommand, "gstart", to the simapp command.
Begin running the updated command by using `go run . gstart` from the `gcosmos` directory.
