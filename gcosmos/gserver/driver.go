package gserver

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	coreappmgr "cosmossdk.io/core/app"
	"cosmossdk.io/core/transaction"
	"cosmossdk.io/server/v2/appmanager"
	consensustypes "cosmossdk.io/x/consensus/types"
	cometapitypes "github.com/cometbft/cometbft/api/cometbft/types/v1"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/internal/gchan"
	"github.com/rollchains/gordian/internal/glog"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmdriver"
)

type driver[T transaction.Tx] struct {
	log *slog.Logger

	done chan struct{}
}

func newDriver[T transaction.Tx](
	lifeCtx, valCtx context.Context,
	log *slog.Logger,
	consensusAuthority string,
	appManager *appmanager.AppManager[T],
	initChainCh <-chan tmdriver.InitChainRequest,
) (*driver[T], error) {
	// Fine if these panic on conversion failure.
	cc := valCtx.Value(client.ClientContextKey).(*client.Context)
	serverCtx := valCtx.Value(server.ServerContextKey).(*server.Context)

	genesisPath := filepath.Join(cc.HomeDir, serverCtx.Config.Genesis)

	ag, err := genutiltypes.AppGenesisFromFile(genesisPath)
	if err != nil {
		return nil, fmt.Errorf("failed to run AppGenesisFromFile(%s): %w", genesisPath, err)
	}

	d := &driver[T]{
		log:  log,
		done: make(chan struct{}),
	}

	go d.run(lifeCtx, valCtx, ag, consensusAuthority, appManager, cc.TxConfig, initChainCh)

	return d, nil
}

func (d *driver[T]) run(
	lifeCtx, valCtx context.Context,
	ag *genutiltypes.AppGenesis,
	consensusAuthority string,
	appManager *appmanager.AppManager[T],
	txConfig client.TxConfig,
	initChainCh <-chan tmdriver.InitChainRequest,
) {
	defer close(d.done)

	d.log.Info("Driver starting...")
	defer d.log.Info("Driver goroutine finished")

	// We are currently assuming we always need to handle init chain,
	// but we should handle non-initial height.
	req, ok := gchan.RecvC(lifeCtx, d.log, initChainCh, "receiving init chain request")
	if !ok {
		d.log.Warn("Context cancelled before receiving init chain message")
		return
	}
	d.log.Info("Got init chain request", "val", req)

	blockReq := &coreappmgr.BlockRequest[T]{
		Height: req.Genesis.InitialHeight, // Comet does genesis height - 1, do we need that too?

		ChainId:   req.Genesis.ChainID,
		IsGenesis: true,

		ConsensusMessages: []transaction.Msg{
			&consensustypes.MsgUpdateParams{
				Authority: consensusAuthority,
				Block: &cometapitypes.BlockParams{
					// Just setting these to something non-zero for now.
					MaxBytes: -1,
					MaxGas:   -1,
				},
				Evidence: &cometapitypes.EvidenceParams{
					// Completely arbitrary non-zero values.
					MaxAgeNumBlocks: 10,
					MaxAgeDuration:  10 * time.Minute,
					MaxBytes:        1024,
				},
				Validator: &cometapitypes.ValidatorParams{
					PubKeyTypes: []string{"ed25519"},
				},
			},
		},

		// Omitted vs comet server code: Time, Hash, AppHash
	}

	appState := []byte(ag.AppState)

	// Now, init genesis in the SDK-layer application.
	var codec transaction.Codec[T] = txDecoder[T]{txConfig: txConfig}
	blockResp, genesisState, err := appManager.InitGenesis(
		lifeCtx, blockReq, appState, codec,
	)
	if err != nil {
		d.log.Warn("Failed to run appManager.InitGenesis", "appState", fmt.Sprintf("%q", appState), "err", err)
		return
	}

	d.log.Info("App response for init chain", "blockResp", blockResp)
	for i, res := range blockResp.TxResults {
		if res.Error != nil {
			d.log.Info("Error in blockResp", "i", i, "err", res.Error)
		}
	}
	d.log.Info("Genesis state from init chain", "genesisState", genesisState)

	gVals := make([]tmconsensus.Validator, len(blockResp.ValidatorUpdates))
	for i, vu := range blockResp.ValidatorUpdates {
		if vu.PubKeyType != "ed25519" {
			panic(fmt.Errorf(
				"TODO: handle validator with non-ed25519 key type: %q",
				vu.PubKeyType,
			))
		}
		pk, err := gcrypto.NewEd25519PubKey(vu.PubKey)
		if err != nil {
			panic(fmt.Errorf(
				"BUG: NewEd25519PubKey should never error; got %w", err,
			))
		}
		gVals[i] = tmconsensus.Validator{
			PubKey: pk,
			Power:  uint64(vu.Power),
		}
	}

	resp := tmdriver.InitChainResponse{
		// Assuming this is correct for AppStateHash but not really sure yet.
		AppStateHash: blockResp.Apphash,

		Validators: gVals,
	}
	if !gchan.SendC(
		lifeCtx, d.log,
		req.Resp, resp,
		"sending init chain response to Gordian",
	) {
		// If this failed it will have logged, so we can just return here.
		return
	}

	d.log.Info(
		"Successfully sent init chain response to Gordian engine",
		"n_vals", len(resp.Validators),
		"app_state_hash", glog.Hex(resp.AppStateHash),
	)
}

func (d *driver[T]) Wait() {
	<-d.done
}
