package gserver

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	coreappmgr "cosmossdk.io/core/app"
	"cosmossdk.io/core/transaction"
	"cosmossdk.io/server/v2/appmanager"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/rollchains/gordian/internal/gchan"
	"github.com/rollchains/gordian/tm/tmdriver"
)

type driver[T transaction.Tx] struct {
	log *slog.Logger

	done chan struct{}
}

func newDriver[T transaction.Tx](
	lifeCtx, valCtx context.Context,
	log *slog.Logger,
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

	go d.run(lifeCtx, valCtx, ag, appManager, cc.TxConfig, initChainCh)

	return d, nil
}

func (d *driver[T]) run(
	lifeCtx, valCtx context.Context,
	ag *genutiltypes.AppGenesis,
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

		// Omitted vs comet server code: Time, Hash, AppHash, ConsensusMessages
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
}

func (d *driver[T]) Wait() {
	<-d.done
}
