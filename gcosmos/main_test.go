package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rollchains/gordian/internal/gtest"
	"github.com/stretchr/testify/require"
)

// Cheap toggle at build time to quickly switch to running comet,
// for cases where the difference in behavior needs to be inspected.
const runCometInsteadOfGordian = false

func TestRootCmd(t *testing.T) {
	t.Parallel()

	e := NewRootCmd(t, gtest.NewLogger(t))

	e.Run("init", "defaultmoniker").NoError(t)
}

func TestRootCmd_startWithGordian_singleValidator(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := ConfigureChain(t, ctx, ChainConfig{
		ID:            t.Name(),
		NVals:         1,
		StakeStrategy: ConstantStakeStrategy(1_000_000_000),
	})

	var startCmd = []string{"start"}
	var gHTTPAddrFile string
	if !runCometInsteadOfGordian {
		gHTTPAddrFile = filepath.Join(t.TempDir(), "http_addr.txt")
		// Then include the HTTP server flags.
		startCmd = append(
			startCmd,
			"--g-http-addr", "127.0.0.1:0",
			"--g-http-addr-file", gHTTPAddrFile,
		)
	}

	// Ensure the start command has fully completed by the end of the test.
	startDone := make(chan struct{})
	go func() {
		defer close(startDone)
		_ = c.RootCmds[0].RunC(ctx, startCmd...)
	}()
	defer func() {
		<-startDone
	}()
	defer cancel()

	// Get the HTTP address, which may require a few tries,
	// depending on how quickly the start command begins.
	if !runCometInsteadOfGordian {
		// Gratuitously long deadline.
		var httpAddr string
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			a, err := os.ReadFile(gHTTPAddrFile)
			if err != nil {
				// Swallow the error and delay.
				time.Sleep(25 * time.Millisecond)
				continue
			}
			if !bytes.HasSuffix(a, []byte("\n")) {
				// Very unlikely incomplete write/read.
				time.Sleep(25 * time.Millisecond)
				continue
			}

			httpAddr = strings.TrimSuffix(string(a), "\n")
			break
		}

		if httpAddr == "" {
			t.Fatalf("did not read http address from %s in time", gHTTPAddrFile)
		}

		u := "http://" + httpAddr + "/blocks/watermark"
		// TODO: we might need to delay until we get a non-error HTTP response.

		deadline = time.Now().Add(10 * time.Second)
		var maxHeight uint
		for time.Now().Before(deadline) {
			resp, err := http.Get(u)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)

			var m map[string]uint
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
			resp.Body.Close()

			maxHeight = m["VotingHeight"]
			if maxHeight < 3 {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// We got at least to height 3, so quit the loop.
			break
		}

		require.GreaterOrEqual(t, maxHeight, uint(3))
	}
}

func TestRootCmd_startWithGordian_multipleValidators(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	const totalVals = 11
	const interestingVals = 4

	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := ConfigureChain(t, ctx, ChainConfig{
		ID:    t.Name(),
		NVals: totalVals,

		// Due to an outstanding and undocumented SDK bug,
		// we require at least 11 validators.
		// But, we don't want to run 11 validators.
		// So put the majority of the stake in the first four validators,
		// and the rest get the bare minimum.
		StakeStrategy: func(idx int) string {
			const minAmount = "1000000"
			if idx < interestingVals {
				// Validators with substantial stake.
				// Give every validator a slightly different amount,
				// so that a tracked single vote's power can be distinguished.
				return minAmount + fmt.Sprintf("%02d000000stake", idx)
			}

			// Beyond that, give them bare minimum stake.
			return minAmount + "stake"
		},
	})

	addrDir := t.TempDir()
	p2pSeedPath := filepath.Join(addrDir, "p2p.seed.txt")

	seedDone := make(chan struct{})
	go func() {
		defer close(seedDone)
		// Just discard the run result.
		// If the seed fails to start, we will have obvious failures later.
		_ = c.RootCmds[0].RunC(ctx, "gordian", "seed", p2pSeedPath)
	}()
	defer func() {
		<-seedDone
	}()
	defer cancel()

	var seedAddrs string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(p2pSeedPath)
		if err != nil {
			if os.IsNotExist(err) {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			t.Fatalf("failed to read p2p seed path file %q: %v", p2pSeedPath, err)
		}

		if !bytes.HasSuffix(b, []byte("\n")) {
			// Very unlikely partial write to file;
			// delay and try again.
			time.Sleep(20 * time.Millisecond)
			continue
		}

		// Otherwise it does end with a \n.
		// We will assume that we have sufficient addresses to connect to,
		// if there is at least one entry.
		seedAddrs = strings.TrimSuffix(string(b), "\n")
	}

	httpAddrFiles := make([]string, interestingVals)

	// Ensure the start command has fully completed by the end of the test.
	var wg sync.WaitGroup
	wg.Add(interestingVals)
	for i := range interestingVals {
		httpAddrFiles[i] = filepath.Join(addrDir, fmt.Sprintf("http_addr_%d.txt", i))

		go func(i int) {
			defer wg.Done()

			startCmd := []string{"start"}
			if !runCometInsteadOfGordian {
				// Then include the HTTP server flags.
				startCmd = append(
					startCmd,
					"--g-seed-addrs", seedAddrs,
					"--g-http-addr", "127.0.0.1:0",
					"--g-http-addr-file", httpAddrFiles[i],
				)
			}

			_ = c.RootCmds[i].RunC(ctx, startCmd...)
		}(i)
	}
	defer wg.Wait()
	defer cancel()

	// Each of the interesting validators must report a height beyond the first few blocks.
	for i := range interestingVals {
		if runCometInsteadOfGordian {
			// Nothing to check in this mode.
			break
		}

		// Gratuitously long deadline to confirm the HTTP address.
		var httpAddr string
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			a, err := os.ReadFile(httpAddrFiles[i])
			if err != nil {
				// Swallow the error and delay.
				time.Sleep(25 * time.Millisecond)
				continue
			}
			if !bytes.HasSuffix(a, []byte("\n")) {
				// Very unlikely incomplete write/read.
				time.Sleep(25 * time.Millisecond)
				continue
			}

			httpAddr = strings.TrimSuffix(string(a), "\n")
			break
		}

		if httpAddr == "" {
			t.Fatalf("did not read http address from %s in time", httpAddrFiles[i])
		}

		u := "http://" + httpAddr + "/blocks/watermark"
		// TODO: we might need to delay until we get a non-error HTTP response.

		// Another deadline to confirm the voting height.
		// This is a lot longer than the deadline to check HTTP addresses
		// because the very first proposed block at 1/0 is expected to time out.
		deadline = time.Now().Add(30 * time.Second)
		var maxHeight uint
		for time.Now().Before(deadline) {
			resp, err := http.Get(u)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)

			var m map[string]uint
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
			resp.Body.Close()

			maxHeight = m["VotingHeight"]
			if maxHeight < 4 {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// We got at least to height 4, so quit the loop.
			break
		}

		require.GreaterOrEqualf(t, maxHeight, uint(3), "checking max block height on validator at index %d", i)
	}
}
