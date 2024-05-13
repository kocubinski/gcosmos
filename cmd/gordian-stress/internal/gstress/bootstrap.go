package gstress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rollchains/gordian/internal/gchan"
)

const (
	cmdSeedAddrs  = "seed-addrs"
	cmdDisconnect = "disconnect"
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
// For that reason, privileged commands such as stopping the network
//
// See also [BootstrapClient].
type BootstrapHost struct {
	log *slog.Logger

	hostAddrs []string

	wg sync.WaitGroup
}

func NewBootstrapHost(
	ctx context.Context,
	log *slog.Logger,
	socketPath string,
	hostAddrs []string,
) (*BootstrapHost, error) {
	h := &BootstrapHost{
		log: log,

		hostAddrs: hostAddrs,
	}

	l, err := new(net.ListenConfig).Listen(ctx, "unix", socketPath)
	if err != nil {
		return nil, err
	}

	h.log.Info("Listening", "socket_path", socketPath)

	const workers = 3
	connCh := make(chan utConn, workers)

	h.wg.Add(2)
	go h.acceptConns(ctx, l, connCh)
	go h.closeListenerOnContextCancel(ctx, l)

	h.wg.Add(workers)
	for range workers {
		go h.connHandler(ctx, connCh)
	}

	return h, nil
}

func (h *BootstrapHost) Wait() {
	h.wg.Wait()
}

// acceptConns accepts incoming connections from the provided net.Listener
// and then delegates those connections to one of the worker goroutines
// running h.connHandler.
func (h *BootstrapHost) acceptConns(
	ctx context.Context, l net.Listener, connCh chan<- utConn,
) {
	defer h.wg.Done()

	// This close shouldn't be strictly necessary due to the Close in
	// closeListenerOnContextCancel, but put it here anyway to be defensive.
	defer l.Close()

	for {
		nConn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				// Correct shutdown; should only have happened on context cancel,
				// probably via the closeListenerOnContextCancel method.
				h.log.Info("Quitting due to context cancellation", "cause", context.Cause(ctx))
				return
			}

			// Failure to accept connection.
			// Just log it and continue.
			h.log.Info("Error during accept", "err", err)
			continue
		}

		ut := utConn{
			N:  nConn,
			TP: textproto.NewConn(nConn),
		}
		if !gchan.SendCLogBlocked(
			ctx, h.log,
			connCh, ut,
			"sending connection to worker goroutine",
			100*time.Millisecond,
		) {
			return
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

// connHandler accepts opened connections on the connCh channel,
// which originate from the goroutine running acceptConns.
func (h *BootstrapHost) connHandler(ctx context.Context, connCh <-chan utConn) {
	defer h.wg.Done()

	for {
		select {
		case <-ctx.Done():
			// No log on worker quit; that would be too noisy.
			return

		case ut := <-connCh:
			// Do the actual work in a dedicated method for easier defer scoping.
			h.handleConn(ctx, ut)
		}
	}
}

// handleConn accepts an arbitrary number of commands on a single connection,
// and then closes the connection.
func (h *BootstrapHost) handleConn(ctx context.Context, ut utConn) {
	defer ut.N.Close()

	// Handle an arbitrary number of commands in the connection.
	for {
		// The individual request must be handled within one second,
		// or we will close the connection.
		ut.N.SetDeadline(time.Now().Add(time.Second))

		if !h.handleRequest(ctx, ut.TP) {
			// The request was not handled successfully,
			// so exit now, causing the underlying connection to close.
			return
		}
	}
}

func (h *BootstrapHost) handleRequest(ctx context.Context, tp *textproto.Conn) (ok bool) {
	id := tp.Next()

	tp.StartRequest(id)
	defer tp.EndRequest(id)

	line, err := tp.ReadLine()
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			// Slightly misbehaving client; it should have done a clean shutdown
			// instead of letting the connection time out.
			h.log.Warn("Client failed to send command within deadline")
			return false
		}

		h.log.Info("Failed to read command from client", "err", err)
		return false
	}

	switch string(line) {
	case cmdSeedAddrs:
		tp.StartResponse(id)
		defer tp.EndResponse(id)
		w := tp.DotWriter()
		defer w.Close()

		_, err := io.WriteString(w, strings.Join(h.hostAddrs, "\t"))
		if err != nil {
			h.log.Info("Failed to write host addresses to client", "err", err)
			return false
		}

		return true

	case cmdDisconnect:
		// Clean connection shutdown requested.
		return false

	default:
		h.log.Warn("Got unknown command", "cmd", line)
		return false
	}
}

// BootstrapClient returns a client to connect to a bootstrap host,
// over the host's unix socket.
//
// Method on BootstrapClient are not safe for concurrent use.
type BootstrapClient struct {
	log *slog.Logger
	ut  utConn
}

func NewBootstrapClient(log *slog.Logger, socketPath string) (*BootstrapClient, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}

	return &BootstrapClient{
		log: log,
		ut: utConn{
			N:  conn,
			TP: textproto.NewConn(conn),
		},
	}, nil
}

// SeedAddrs returns the reported seed addresses from the bootstrap host.
func (c *BootstrapClient) SeedAddrs() ([]string, error) {
	c.ut.N.SetDeadline(time.Now().Add(time.Second))

	tp := c.ut.TP
	id, err := tp.Cmd(cmdSeedAddrs)
	if err != nil {
		return nil, fmt.Errorf("failed to start seed addresses command: %w", err)
	}

	tp.StartResponse(id)
	defer tp.EndResponse(id)

	line, err := tp.ReadLine()
	if err != nil {
		return nil, fmt.Errorf("failed to read seed addresses response: %w", err)
	}

	return strings.Split(line, "\t"), nil
}

// Stop cleanly disconnects the client from the host.
// Any further use of the client after a call to Stop results in undefined behavior.
func (c *BootstrapClient) Stop() error {
	_, err := c.ut.TP.Cmd(cmdDisconnect)
	if err != nil {
		// If we failed to send the disconnect command,
		// still attempt to close the underlying connection,
		// just to ensure resources are cleaned up.
		_ = c.ut.N.Close()
		c.ut = utConn{}

		return fmt.Errorf("failed to send disconnect command: %w", err)
	}

	// Successfully sent disconnect command, so close the network connection.
	err = c.ut.N.Close()
	c.ut = utConn{}
	return err
}

// utConn is a tuple of a raw [net.Conn] (backed by a [*net.UnixConn])
// and a corresponding [*textproto.Conn] wrapping the net.Conn.
//
// The textproto.Conn is a minimal interface that has little overlap with net.Conn,
// so both are put in place to ensure we can access useful methods
// such as read and write deadlines.
type utConn struct {
	N  net.Conn
	TP *textproto.Conn
}
