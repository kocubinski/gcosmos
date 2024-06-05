// Command gcosmos wraps the Cosmos SDK simappv2
// and adds a "gstart" subcommand to run parts of Gordian.
package main

import (
	"fmt"
	"os"

	"cosmossdk.io/simapp/v2"
	simdcmd "cosmossdk.io/simapp/v2/simdv2/cmd"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/rollchains/gordian/gcosmos/internal/gci"
)

func main() {
	rootCmd := simdcmd.NewRootCmd()
	rootCmd.AddCommand(gci.StartGordianCommand())
	if err := svrcmd.Execute(rootCmd, "", simapp.DefaultNodeHome); err != nil {
		fmt.Fprintln(rootCmd.OutOrStderr(), err)
		os.Exit(1)
	}
}
