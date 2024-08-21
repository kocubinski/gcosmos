//go:build !debug

package gserver

import (
	"github.com/rollchains/gordian/tm/tmengine"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// No-op functions to match the debug build.

func addAssertRuleFlag(fs *pflag.FlagSet) {}

func getAssertEngineOpt(v *viper.Viper) (_ tmengine.Opt, _ error) {
	return
}
