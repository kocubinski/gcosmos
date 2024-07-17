# gcosmos

This is a temporary workspace for integrating Gordian with [the Cosmos SDK](https://github.com/cosmos/cosmos-sdk).
As Gordian core reaches a stable release, the gcosmos tree will move to its own repository.

## Setup

We are currently using a local, unmodified clone of the SDK in tandem with Go workspaces.
It is a bit unconventional to commit a Go workspace file, but while both Gordian and the Cosmos SDK
are being actively changed, a fixed Go workspace fits well for now.

From the `gcosmos` directory, run `./_cosmosvendor/sync_sdk.bash` to clone or fetch and checkout
a "known working" version of the Cosmos SDK compatible with the current gcosmos tree,
and then apply any currently necessary patches to the SDK.
You may need to run `go work sync` from the `gcosmos` directory again.

### New patches

If further patching to the SDK is required for any reason,
add new patches to the `patches` directory.
The easiest way to do this is to create a commit locally in the `_cosmosvendor/cosmos-sdk` repository,
and then run `git format-patch -o ../patches/ HEAD^` to add the patch for only your most recent commit
into the `patches` directory. The `sync_sdk.bash` script applies any patches declared in that directory
following the sync to the commit specified in `_cosmosvendor/COSMOS_SDK.commit`.

Of course, upstreaming changes to the actual Cosmos SDK repository would be preferred,
but sometimes a local patch makes more sense.

## Running

This currently adds one new subcommand, "gstart", to the simapp command.
Begin running the updated command by using `go run . gstart` from the `gcosmos` directory.
