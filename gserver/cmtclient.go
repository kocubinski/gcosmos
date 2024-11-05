package gserver

import (
	"context"
	"errors"
	"fmt"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	cmtcryptoed25519 "github.com/cometbft/cometbft/crypto/ed25519"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
	client "github.com/cosmos/cosmos-sdk/client"
	ggrpc "github.com/gordian-engine/gcosmos/gserver/internal/ggrpc"
	"github.com/spf13/cobra"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var _ client.CometRPC = (*Client)(nil)

type Client struct {
	cmd     *cobra.Command
	gclient ggrpc.GordianGRPCClient
}

func NewClient(
	cmd *cobra.Command,
	grpcAddress string,
	grpcInsecure bool,
) (*Client, error) {
	var dialOpts []grpc.DialOption
	if grpcInsecure {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	cc, err := grpc.NewClient(grpcAddress, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial gRPC: %w", err)
	}

	return &Client{
		cmd:     cmd,
		gclient: ggrpc.NewGordianGRPCClient(cc),
	}, nil
}

func (c *Client) ABCIInfo(ctx context.Context) (*coretypes.ResultABCIInfo, error) {
	panic(fmt.Errorf("not implemented"))
}

func (c *Client) ABCIQuery(ctx context.Context, path string, data cmtbytes.HexBytes) (*coretypes.ResultABCIQuery, error) {
	panic(fmt.Errorf("not implemented"))
}

func (c *Client) ABCIQueryWithOptions(ctx context.Context, path string, data cmtbytes.HexBytes, opts cmtclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	panic(fmt.Errorf("not implemented"))
}

func (c *Client) BroadcastTxAsync(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	// TODO implement
	return c.BroadcastTxSync(ctx, tx)
}

func (c *Client) BroadcastTxCommit(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTxCommit, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) BroadcastTxSync(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	clientCtx, err := client.GetClientTxContext(c.cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to get client tx context: %w", err)
	}

	sdkTx, err := clientCtx.TxConfig.TxDecoder()(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to decode tx: %w", err)
	}

	txJson, err := clientCtx.TxConfig.TxJSONEncoder()(sdkTx)
	if err != nil {
		return nil, fmt.Errorf("failed to encode tx: %w", err)
	}

	res, err := c.gclient.SimulateTransaction(ctx, &ggrpc.SubmitSimulationTransactionRequest{
		Tx: txJson,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to simulate transaction: %w", err)
	}
	if res.Error != "" {
		return nil, fmt.Errorf("failure expecting simulated transaction: %s", res.Error)
	}

	res, err = c.gclient.SubmitTransaction(ctx, &ggrpc.SubmitTransactionRequest{
		Tx: txJson,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to submit transaction: %w", err)
	}
	if res.Error != "" {
		return nil, fmt.Errorf("failure expecting transaction: %s", res.Error)
	}

	return &coretypes.ResultBroadcastTx{
		Code:      res.Code,
		Data:      res.Data,
		Log:       res.Log,
		Hash:      tx.Hash(),
		Codespace: res.Codespace,
	}, nil
}

func (c *Client) Validators(ctx context.Context, height *int64, page, perPage *int) (*coretypes.ResultValidators, error) {
	valsRes, err := c.gclient.GetValidators(ctx, &ggrpc.GetValidatorsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to get validators: %w", err)
	}

	vals := valsRes.Validators

	res := &coretypes.ResultValidators{
		BlockHeight: 0, // TODO
		Validators:  make([]*cmttypes.Validator, len(vals)),
		Count:       len(vals), // no pagination yet
		Total:       len(vals),
	}

	for i, val := range vals {
		res.Validators[i] = &cmttypes.Validator{
			// Address: // TODO derive from pubkey
			PubKey:      cmtcryptoed25519.PubKey(val.GetEncodedPubKey()), // TODO may need to decode based on method name
			VotingPower: int64(val.Power),
		}
	}

	return res, nil
}

func (c *Client) Status(ctx context.Context) (*coretypes.ResultStatus, error) {
	gRes, err := c.gclient.GetBlocksWatermark(ctx, &ggrpc.CurrentBlockRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to get blocks watermark: %w", err)
	}

	// TODO fill out remaining fields
	return &coretypes.ResultStatus{
		SyncInfo: coretypes.SyncInfo{
			LatestBlockHeight: int64(gRes.CommittingHeight) - 1,
		},
	}, nil
}

func (c *Client) Block(ctx context.Context, height *int64) (*coretypes.ResultBlock, error) {
	panic(fmt.Errorf("not implemented"))
}

func (c *Client) BlockByHash(ctx context.Context, hash []byte) (*coretypes.ResultBlock, error) {
	panic(fmt.Errorf("not implemented"))
}

func (c *Client) BlockResults(ctx context.Context, height *int64) (*coretypes.ResultBlockResults, error) {
	panic(fmt.Errorf("not implemented"))
}

func (c *Client) BlockchainInfo(ctx context.Context, minHeight, maxHeight int64) (*coretypes.ResultBlockchainInfo, error) {
	panic(fmt.Errorf("not implemented"))
}

func (c *Client) Commit(ctx context.Context, height *int64) (*coretypes.ResultCommit, error) {
	panic(fmt.Errorf("not implemented"))
}

func (c *Client) Tx(ctx context.Context, hash []byte, prove bool) (*coretypes.ResultTx, error) {
	if prove {
		panic(errors.New("TODO: prove is not yet supported"))
	}
	res, err := c.gclient.QueryTx(ctx, &ggrpc.SDKQueryTxRequest{
		TxHash: hash,
		Prove:  prove,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query transaction: %w", err)
	}

	resEvents := make([]abcitypes.Event, len(res.Result.Events))
	for i, e := range res.Result.Events {
		out := abcitypes.Event{
			Type:       e.Type,
			Attributes: make([]abcitypes.EventAttribute, len(e.Attributes)),
		}

		for j, a := range e.Attributes {
			out.Attributes[j] = abcitypes.EventAttribute{
				Key:   a.Key,
				Value: a.Value,

				// TODO: when should Index be true?
			}
		}

		resEvents[i] = out
	}

	return &coretypes.ResultTx{
		Hash:   cmtbytes.HexBytes(res.TxHash),
		Height: res.Height,

		// TODO: what is Index supposed to be?

		TxResult: abcitypes.ExecTxResult{
			Code: res.Result.Code,
			Data: res.Result.Data,
			Log:  res.Result.Log,
			Info: res.Result.Info,

			// TODO: decide if the proto type should use int64 instead.
			GasWanted: int64(res.Result.GasWanted),
			GasUsed:   int64(res.Result.GasUsed),

			Events:    resEvents,
			Codespace: res.Result.Codespace,
		},

		Tx: cmttypes.Tx(res.TxBytes),

		Proof: cmttypes.TxProof{
			// TODO: what are RootHash, Data, and Proof supposed to be?
		},
	}, nil
}

func (c *Client) TxSearch(
	ctx context.Context,
	query string,
	prove bool,
	page, perPage *int,
	orderBy string,
) (*coretypes.ResultTxSearch, error) {
	panic(fmt.Errorf("not implemented"))
}

func (c *Client) BlockSearch(
	ctx context.Context,
	query string,
	page, perPage *int,
	orderBy string,
) (*coretypes.ResultBlockSearch, error) {
	panic(fmt.Errorf("not implemented"))
}
