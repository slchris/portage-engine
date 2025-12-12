package builder

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// GPGKeyClient handles fetching GPG keys from server.
type GPGKeyClient struct {
	serverURL  string
	httpClient *http.Client
}

// NewGPGKeyClient creates a new GPG key client.
func NewGPGKeyClient(serverURL string) *GPGKeyClient {
	return &GPGKeyClient{
		serverURL: serverURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FetchGPGKey downloads the GPG public key from server.
func (g *GPGKeyClient) FetchGPGKey(destPath string) error {
	url := g.serverURL + "/api/v1/gpg/public-key"

	resp, err := g.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch GPG key: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	// Ensure directory exists
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create temporary file
	tmpFile := destPath + ".tmp"
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Write key to file
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to write key: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpFile, destPath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to save key: %w", err)
	}

	return nil
}
