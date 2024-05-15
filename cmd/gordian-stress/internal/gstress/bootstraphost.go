package gstress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// BootstrapHost is the host portion for bootstrapping a stress cluster.
//
// It is a minimal and rudimentary host intended for bootstrapping nodes into a network.
// Once bootstrapped, further work should happen over libp2p protocols.
//
// The BootstrapHost (currently) depends on a Unix socket listener for a few reasons.
// First, it is easy for an operator to pick a file path for a bootstrap host
// without any kind of conflict.
// Inspecting the filesystem at a predetermined location gives simple and quick feedback
// that a particular host is running.
// Likewise, on a clean shutdown, the socket file is removed,
// also giving quick feedback that the particular host is no longer running.
//
// Next, running on a Unix socket simplifies permissioned access;
// there is no risk of the Unix socket being mistakenly exposed on the public internet.
//
// See also [BootstrapClient], which is the intended way to access the bootstrap host.
// But it should also be possible to use curl's --unix-socket flag,
// if CLI access is required for some reason.
type BootstrapHost struct {
	log *slog.Logger

	s bState

	hostAddrs []string

	wg sync.WaitGroup
}

func NewBootstrapHost(
	ctx context.Context,
	log *slog.Logger,
	serverSocketPath string,
	hostAddrs []string,
) (*BootstrapHost, error) {
	h := &BootstrapHost{
		log: log,

		s: bState{
			chainID: fmt.Sprintf("gstress%d", time.Now().Unix()),
		},

		hostAddrs: hostAddrs,
	}

	l, err := new(net.ListenConfig).Listen(ctx, "unix", serverSocketPath)
	if err != nil {
		return nil, fmt.Errorf("error listening: %w", err)
	}

	h.log.Info("Listening", "addr", l.Addr().String())

	h.wg.Add(2)
	go h.serve(l)
	go h.closeListenerOnContextCancel(ctx, l)

	return h, nil
}

func (h *BootstrapHost) Wait() {
	h.wg.Wait()
}

func (h *BootstrapHost) serve(l net.Listener) {
	defer h.wg.Done()

	if err := http.Serve(l, h.newMux()); err != nil {
		if errors.Is(err, net.ErrClosed) {
			h.log.Info("HTTP server shutting down")
		} else {
			h.log.Info("HTTP server shutting down due to error", "err", err)
		}
	}
}

// closeListenerOnContextCancel waits for a root context cancellation
// and then closes the host's listener.
// This is a workaround for the fact that an open listener does not have a way
// to stop an in-progress Accept call upon context cancellation;
// it requires an explicit Close call, which is allowed to originate on another goroutine.
func (h *BootstrapHost) closeListenerOnContextCancel(ctx context.Context, l net.Listener) {
	defer h.wg.Done()

	<-ctx.Done()

	if err := l.Close(); err != nil {
		h.log.Warn("Error closing listener", "err", err)
	}
}

func (h *BootstrapHost) newMux() *http.ServeMux {
	m := http.NewServeMux()

	m.HandleFunc("/seed-addrs", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		enc := json.NewEncoder(w)
		if err := enc.Encode(h.hostAddrs); err != nil {
			h.log.Info("Failed to write seed addresses", "err", err)
		}
	})

	m.HandleFunc("/chain-id", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			// GET: report the current chain ID.
			if _, err := io.WriteString(w, h.s.ChainID()+"\n"); err != nil {
				h.log.Info("Failed to write chain ID", "err", err)
			}
			return
		}

		if req.Method == http.MethodPost {
			// POST: replace the chain ID with what the client supplied.
			b, err := io.ReadAll(req.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				h.log.Info("Failed to read new chain ID from client", "err", err)
				return
			}

			newID, ok := strings.CutSuffix(string(b), "\n")
			if !ok {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			w.WriteHeader(http.StatusNoContent)
			h.s.SetChainID(strings.TrimSpace(string(newID)))
			return
		}

		// Didn't return earlier, so this was a bad method.
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	return m
}
