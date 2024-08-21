//go:build debug

package gserver

import (
	"github.com/rollchains/gordian/gassert"
	"github.com/rollchains/gordian/tm/tmengine"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const assertRuleFlag = "g-assert-rules"

func addAssertRuleFlag(fs *pflag.FlagSet) {
	// Default to all rules.
	fs.String(assertRuleFlag, "*", "Comma-separated assertion rules. Only available in debug builds. See package docs for github.com/rollchains/gordian/gassert.")
}

func getAssertEngineOpt(v *viper.Viper) (tmengine.Opt, error) {
	rules := v.GetString(assertRuleFlag)
	env, err := gassert.EnvironmentFromString(rules)
	if err != nil {
		return nil, err
	}
	return tmengine.WithAssertEnv(env), nil
}
