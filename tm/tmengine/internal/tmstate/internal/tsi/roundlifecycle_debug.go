//go:build debug

package tsi

import (
	"errors"
	"fmt"
)

func (rlc *RoundLifecycle) invariantCycleFinalization() {
	if !rlc.AssertEnv.Enabled("tm.engine.state_machine.rlc") {
		return
	}

	var err error
	if len(rlc.FinalizedValSet.Validators) == 0 {
		err = errors.New("rlc.FinalizedValidators is empty")
	}

	if rlc.FinalizedAppStateHash == "" {
		err = errors.Join(err, errors.New("rlc.FinalizedAppStateHash is empty"))
	}

	if rlc.FinalizedBlockHash == "" {
		err = errors.Join(err, errors.New("rlc.FinalizedBlockHash is empty"))
	}

	if err != nil {
		rlc.AssertEnv.HandleAssertionFailure(
			fmt.Errorf("invariant for CycleFinalization failed at %d/%d: %w", rlc.H, rlc.R, err),
		)
	}
}
