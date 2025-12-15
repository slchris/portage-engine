package builder

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GPGKeyClient handles fetching and importing GPG keys from server.
type GPGKeyClient struct {
	serverURL  string
	httpClient *http.Client
	gnupgHome  string
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

// WithGnupgHome sets the GNUPGHOME directory.
func (g *GPGKeyClient) WithGnupgHome(home string) *GPGKeyClient {
	g.gnupgHome = home
	return g
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

// ImportGPGKey imports a GPG public key into the keyring.
func (g *GPGKeyClient) ImportGPGKey(keyPath string) error {
	args := []string{"--batch", "--yes", "--import", keyPath}
	if g.gnupgHome != "" {
		args = append([]string{"--homedir", g.gnupgHome}, args...)
	}

	cmd := exec.Command("gpg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to import GPG key: %w: %s", err, stderr.String())
	}

	return nil
}

// FetchAndImportGPGKey fetches the GPG key from server and imports it.
func (g *GPGKeyClient) FetchAndImportGPGKey(destPath string) error {
	if err := g.FetchGPGKey(destPath); err != nil {
		return fmt.Errorf("failed to fetch GPG key: %w", err)
	}

	if err := g.ImportGPGKey(destPath); err != nil {
		return fmt.Errorf("failed to import GPG key: %w", err)
	}

	return nil
}

// GetKeyID extracts the key ID from an imported key file.
func (g *GPGKeyClient) GetKeyID(keyPath string) (string, error) {
	args := []string{"--batch", "--with-colons", "--import-options", "show-only", "--import", keyPath}
	if g.gnupgHome != "" {
		args = append([]string{"--homedir", g.gnupgHome}, args...)
	}

	cmd := exec.Command("gpg", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get key info: %w: %s", err, stderr.String())
	}

	// Parse output for key ID (pub:...:keyid:...)
	lines := strings.Split(stdout.String(), "\n")
	for _, line := range lines {
		fields := strings.Split(line, ":")
		if len(fields) > 4 && fields[0] == "pub" {
			return fields[4], nil
		}
	}

	return "", fmt.Errorf("key ID not found in output")
}

// TrustKey sets ultimate trust for a key ID.
func (g *GPGKeyClient) TrustKey(keyID string) error {
	args := []string{"--batch", "--yes"}
	if g.gnupgHome != "" {
		args = append([]string{"--homedir", g.gnupgHome}, args...)
	}
	args = append(args, "--edit-key", keyID, "trust", "quit")

	// Create input for trust level (5 = ultimate)
	cmd := exec.Command("gpg", args...)
	cmd.Stdin = strings.NewReader("5\ny\n")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to trust key: %w: %s", err, stderr.String())
	}

	return nil
}
