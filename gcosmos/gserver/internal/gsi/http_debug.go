package gsi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"cosmossdk.io/core/transaction"
	"cosmossdk.io/server/v2/appmanager"
	"github.com/gorilla/mux"
)

type debugHandler struct {
	log *slog.Logger

	txCodec transaction.Codec[transaction.Tx]

	am appmanager.AppManager[transaction.Tx]
}

func setDebugRoutes(log *slog.Logger, cfg HTTPServerConfig, r *mux.Router) {
	h := debugHandler{
		log:     log,
		txCodec: cfg.TxCodec,
		am:      cfg.AppManager,
	}

	r.HandleFunc("/debug/submit_tx", h.HandleSubmitTx).Methods("POST")
	r.HandleFunc("/debug/simulate_tx", h.HandleSimulateTx).Methods("POST")
}

func (h debugHandler) HandleSubmitTx(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	b, err := io.ReadAll(req.Body)
	if err != nil {
		h.log.Warn("Failed to read request body", "route", "submit_tx", "err", err)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	tx, err := h.txCodec.DecodeJSON(b)
	if err != nil {
		h.log.Warn("Failed to decode transaction", "route", "submit_tx", "err", err)
		http.Error(w, "failed to decode transaction", http.StatusBadRequest)
		return
	}

	// TODO: should this have a configurable timeout?
	// Probably fine to skip since this is a "debug" endpoint for now,
	// but if this gets promoted to a non-debug route,
	// it should have a timeout.
	ctx := req.Context()

	res, err := h.am.ValidateTx(ctx, tx)
	if err != nil {
		// ValidateTx should only return an error at this level,
		// if it failed to get state from the store.
		h.log.Warn("Error attempting to validate transaction", "route", "submit_tx", "err", err)
		http.Error(w, "internal error while attempting to validate transaction", http.StatusInternalServerError)
		return
	}

	if res.Error != nil {
		// This is fine from the server's perspective, no need to log.
		http.Error(w, "transaction validation failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := json.NewEncoder(w).Encode(res); err != nil {
		h.log.Warn("Failed to encode submit_tx result", "err", err)
	}
}

func (h debugHandler) HandleSimulateTx(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	b, err := io.ReadAll(req.Body)
	if err != nil {
		h.log.Warn("Failed to read request body", "route", "simulate_tx", "err", err)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	tx, err := h.txCodec.DecodeJSON(b)
	if err != nil {
		h.log.Warn("Failed to decode transaction", "route", "simulate_tx", "err", err)
		http.Error(w, "failed to decode transaction", http.StatusBadRequest)
		return
	}

	// TODO: should this have a configurable timeout?
	// Probably fine to skip since this is a "debug" endpoint for now,
	// but if this gets promoted to a non-debug route,
	// it should have a timeout.
	ctx := req.Context()

	res, _, err := h.am.Simulate(ctx, tx)
	if err != nil {
		// Simulate should only return an error at this level,
		// if it failed to get state from the store.
		h.log.Warn("Error attempting to simulate transaction", "route", "simulate_tx", "err", err)
		http.Error(w, "internal error while attempting to simulate transaction", http.StatusInternalServerError)
		return
	}

	if res.Error != nil {
		// This is fine from the server's perspective, no need to log.
		http.Error(w, "transaction simulation failed: "+res.Error.Error(), http.StatusBadRequest)
		return
	}

	if err := json.NewEncoder(w).Encode(res); err != nil {
		h.log.Warn("Failed to encode simulate_tx result", "err", err)
	}
}
