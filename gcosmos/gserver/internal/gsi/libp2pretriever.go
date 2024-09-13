package gsi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"cosmossdk.io/core/transaction"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
	"github.com/rollchains/gordian/gcosmos/gserver/internal/gsbd"
)

// libp2pRetriever wraps the Libp2pClient
// to adapt it into the [gdatapool.Retriever] interface.
type libp2pRetriever struct {
	log *slog.Logger

	c *gsbd.Libp2pClient
}

// Retrieve handles unmarshaling the metadata
// for a direct call into the [Libp2pClient.Retrieve].
func (r *libp2pRetriever) Retrieve(
	ctx context.Context, dataID string, metadata []byte,
) ([]transaction.Tx, error) {
	var pda ProposalDriverAnnotation
	if err := json.Unmarshal(metadata, &pda); err != nil {
		return nil, fmt.Errorf("failed to decode metadata: %w", err)
	}

	for _, loc := range pda.Locations {
		if loc.Scheme != gsbd.Libp2pScheme {
			// Silently skip anything that isn't libp2p.
			continue
		}

		// If it is a libp2p scheme,
		// then the Addr field should be a JSON-encoded libp2p addr info.
		var ai libp2ppeer.AddrInfo
		if err := json.Unmarshal([]byte(loc.Addr), &ai); err != nil {
			r.log.Debug(
				"Skipping retrieval location due to failure to unmarshal AddrInfo",
				"err", err,
			)
			continue
		}

		txs, err := r.c.Retrieve(ctx, ai, dataID)
		if err != nil {
			// On error to retrieve the data, there is nothing we can really do.
			// Maybe we could accumulate this error into a joined error?
			r.log.Warn("Error while attempting to retrieve data", "err", err)
			continue
		}

		// We have successfully got the transactions,
		// and they have been validated according to the data ID.
		// Now we can write it back to the request,
		// signaling that writes are done and reads may proceed.
		return txs, nil
	}

	return nil, errors.New("failed to retrieve data from any location")
}
