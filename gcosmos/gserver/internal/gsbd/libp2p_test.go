package gsbd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"cosmossdk.io/core/transaction"
	authtx "cosmossdk.io/x/auth/tx"
	banktypes "cosmossdk.io/x/bank/types"
	"cosmossdk.io/x/tx/signing"
	"github.com/cosmos/cosmos-sdk/codec"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
	"github.com/rollchains/gordian/gcosmos/gccodec"
	"github.com/rollchains/gordian/gcosmos/gserver/internal/gsbd"
	"github.com/rollchains/gordian/internal/gtest"
	"github.com/rollchains/gordian/tm/tmcodec/tmjson"
	"github.com/rollchains/gordian/tm/tmp2p/tmlibp2p/tmlibp2ptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLibp2pHost_roundTrip(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// tmlibp2ptest probably isn't exactly the right network setup to use,
	// as it does extra work for consensus message setup;
	// but it does all the other heavy lifting we need,
	// and it's already written, so we are using it here.
	log := gtest.NewLogger(t)
	net, err := tmlibp2ptest.NewNetwork(ctx, log.With("sys", "net"), tmjson.MarshalCodec{
		// I'm a little surprised this doesn't fail when we omit a registry.
		// I guess it's okay since we don't attempt to marshal any consensus messages here.
	})
	require.NoError(t, err)
	defer net.Wait()
	defer cancel()

	host, err := net.Connect(ctx)
	require.NoError(t, err)

	client, err := net.Connect(ctx)
	require.NoError(t, err)

	require.NoError(t, net.Stabilize(ctx))

	provider := gsbd.NewLibp2pProviderHost(host.Host().Libp2pHost())

	// Generated from one of the gcosmos integration tests.
	// We don't really care about the actual content, so long as it is a transaction.Tx.
	const txSendJSON = `{"body":{"messages":[{"@type":"/cosmos.bank.v1beta1.MsgSend","from_address":"cosmos1r5v5srda7xfth3hn2s26txvrcrntldjumt8mhl","to_address":"cosmos1znj9yaxy2yjaldx20833zwayjduutddxf0czje","amount":[{"denom":"stake","amount":"100"}]}],"memo":"","timeout_height":"0","unordered":false,"timeout_timestamp":"0001-01-01T00:00:00Z","extension_options":[],"non_critical_extension_options":[]},"auth_info":{"signer_infos":[{"public_key":{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"ArpmqEz3g5rxcqE+f8n15wCMuLyhWF+PO6+zA57aPB/d"},"mode_info":{"single":{"mode":"SIGN_MODE_DIRECT"}},"sequence":"30"}],"fee":{"amount":[],"gas_limit":"200000","payer":"cosmos1r5v5srda7xfth3hn2s26txvrcrntldjumt8mhl","granter":""},"tip":null},"signatures":["llNpiex9bSOHt4ai+C5KCE7Qo/7zn3ijZGo4dhBRpLNnLtMlZvuTTYCxeNecWaPO4r5Il9L6+nNkFbANKsolFw=="]}`

	ir := codectypes.NewInterfaceRegistry()

	// Normally this registration would happen through depinject
	// and the runtime manager, but we don't want all that weight here.
	std.RegisterInterfaces(ir)
	banktypes.RegisterInterfaces(ir)

	protoCodec := codec.NewProtoCodec(ir)
	txCfg, err := authtx.NewTxConfigWithOptions(protoCodec, authtx.ConfigOptions{
		SigningOptions: &signing.Options{
			AddressCodec:          addresscodec.NewBech32Codec("cosmos"),
			ValidatorAddressCodec: addresscodec.NewBech32Codec("cosmosvaloper"),
		},
	})
	require.NoError(t, err)

	sendTx, err := txCfg.TxJSONDecoder()([]byte(txSendJSON))
	require.NoError(t, err)

	// Provide this set of transactions as the block data for 1/0.
	txs := []transaction.Tx{sendTx}
	res, err := provider.Provide(ctx, 1, 0, txs)
	require.NoError(t, err)

	// Parse out the host address from the provide result.
	var ai libp2ppeer.AddrInfo
	require.NoError(t, json.Unmarshal([]byte(res.Addrs[0].Addr), &ai))
	c := gsbd.NewLibp2pClient(
		log.With("sys", "client"),
		client.Host().Libp2pHost(),
		gccodec.NewTxDecoder(txCfg),
	)
	gotTxs, err := c.Retrieve(ctx, ai, res.DataID, res.DataSize)
	require.NoError(t, err)
	require.Len(t, gotTxs, 1)
	RequireEqualTx(t, sendTx, gotTxs[0])
}

func RequireEqualTx(t *testing.T, want, got transaction.Tx) {
	t.Helper()

	// Using assert instead of require in this helper,
	// so we can see multiple messages about the diff.
	assert.Equal(t, want.Hash(), got.Hash())

	// Assuming the Bytes() output is deterministic.
	// assert.Equal(t, want.Bytes(), got.Bytes())
	wantBytes, gotBytes := want.Bytes(), got.Bytes()
	assert.Truef(
		t,
		bytes.Equal(wantBytes, gotBytes),
		"encoded bytes differed:\nwant=%x\ngot= %x",
		wantBytes, gotBytes,
	)

	wantMsgs, err := want.GetMessages()
	require.NoError(t, err)
	gotMsgs, err := got.GetMessages()
	require.NoError(t, err)

	require.Equal(t, wantMsgs, gotMsgs)

	if t.Failed() {
		// Stop the test if any assertion failed.
		t.FailNow()
	}
}
