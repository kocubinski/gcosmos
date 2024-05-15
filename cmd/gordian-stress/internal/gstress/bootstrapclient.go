package gstress

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tv42/httpunix"
)

// BootstrapClient returns a client to connect to a bootstrap host,
// over the host's unix socket.
//
// Method on BootstrapClient are not safe for concurrent use.
type BootstrapClient struct {
	log *slog.Logger

	client *http.Client
}

const bootstrapURL = "http+unix://bootstraphost"

func NewBootstrapClient(log *slog.Logger, serverSocketPath string) (*BootstrapClient, error) {
	// We are using a HTTP client over a Unix socket,
	// as this is a bit simpler from the end user's perspective.
	tr := &httpunix.Transport{
		DialTimeout:           100 * time.Millisecond,
		RequestTimeout:        time.Second,
		ResponseHeaderTimeout: time.Second,
	}
	// When encountering a URL with the domain "bootstraphost",
	// access it through the serverSocketPath.
	tr.RegisterLocation("bootstraphost", serverSocketPath)

	return &BootstrapClient{
		log: log,

		client: &http.Client{Transport: tr},
	}, nil
}

func (c *BootstrapClient) SeedAddrs() ([]string, error) {
	resp, err := c.client.Get(bootstrapURL + "/seed-addrs")
	if err != nil {
		return nil, fmt.Errorf("failed to get seed addresses: %w", err)
	}

	defer resp.Body.Close()

	var addrs []string

	if err := json.NewDecoder(resp.Body).Decode(&addrs); err != nil {
		return nil, fmt.Errorf("failed to parse seed addresses: %w", err)
	}

	return addrs, nil
}

func (c *BootstrapClient) ChainID() (string, error) {
	resp, err := c.client.Get(bootstrapURL + "/chain-id")
	if err != nil {
		return "", fmt.Errorf("failed to get chain ID: %w", err)
	}

	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read chain ID response: %w", err)
	}

	id, ok := strings.CutSuffix(string(b), "\n")
	if !ok {
		return "", fmt.Errorf("got malformatted chain ID: %q", b)
	}

	return id, nil
}

func (c *BootstrapClient) SetChainID(newID string) error {
	resp, err := c.client.Post(
		bootstrapURL+"/chain-id",
		"text/plain",
		strings.NewReader(newID+"\n"),
	)
	if err != nil {
		return fmt.Errorf("failed to get chain ID: %w", err)
	}

	resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("got unexpected response status: %d", resp.StatusCode)
	}

	return nil
}
