package builder

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewGPGKeyClient(t *testing.T) {
	client := NewGPGKeyClient("http://localhost:8080")
	if client == nil {
		t.Fatal("NewGPGKeyClient returned nil")
	}
	if client.serverURL != "http://localhost:8080" {
		t.Errorf("serverURL = %s, want http://localhost:8080", client.serverURL)
	}
	if client.httpClient == nil {
		t.Error("httpClient not initialized")
	}
}

func TestFetchGPGKey(t *testing.T) {
	tests := []struct {
		name           string
		serverStatus   int
		serverResponse string
		expectError    bool
	}{
		{
			name:           "success",
			serverStatus:   http.StatusOK,
			serverResponse: "-----BEGIN PGP PUBLIC KEY BLOCK-----\ntest key\n-----END PGP PUBLIC KEY BLOCK-----\n",
			expectError:    false,
		},
		{
			name:         "server error",
			serverStatus: http.StatusInternalServerError,
			expectError:  true,
		},
		{
			name:         "not found",
			serverStatus: http.StatusNotFound,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/gpg/public-key" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tt.serverStatus)
				if tt.serverResponse != "" {
					_, _ = w.Write([]byte(tt.serverResponse))
				}
			}))
			defer server.Close()

			client := NewGPGKeyClient(server.URL)

			tmpDir := t.TempDir()
			destPath := filepath.Join(tmpDir, "gpg-key.asc")

			err := client.FetchGPGKey(destPath)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				// Verify file was created
				if _, err := os.Stat(destPath); err != nil {
					t.Errorf("key file not created: %v", err)
				}

				// Verify content
				content, err := os.ReadFile(destPath)
				if err != nil {
					t.Fatalf("failed to read key file: %v", err)
				}
				if string(content) != tt.serverResponse {
					t.Errorf("content = %q, want %q", string(content), tt.serverResponse)
				}
			}
		})
	}
}

func TestFetchGPGKeyServerDown(t *testing.T) {
	client := NewGPGKeyClient("http://localhost:9999")

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "gpg-key.asc")

	err := client.FetchGPGKey(destPath)
	if err == nil {
		t.Error("expected error when server is down")
	}
}

func TestFetchGPGKeyCreateDirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test key"))
	}))
	defer server.Close()

	client := NewGPGKeyClient(server.URL)

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "subdir", "another", "gpg-key.asc")

	err := client.FetchGPGKey(destPath)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if _, err := os.Stat(destPath); err != nil {
		t.Errorf("key file not created: %v", err)
	}
}

func TestGPGKeyClientWithGnupgHome(t *testing.T) {
	client := NewGPGKeyClient("http://localhost:8080")
	client = client.WithGnupgHome("/tmp/gpg-test")

	if client.gnupgHome != "/tmp/gpg-test" {
		t.Errorf("gnupgHome = %s, want /tmp/gpg-test", client.gnupgHome)
	}
}

func TestImportGPGKeyNoFile(t *testing.T) {
	client := NewGPGKeyClient("http://localhost:8080")

	err := client.ImportGPGKey("/nonexistent/path/to/key.asc")
	if err == nil {
		t.Error("expected error when key file doesn't exist")
	}
}

func TestGetKeyIDNoFile(t *testing.T) {
	client := NewGPGKeyClient("http://localhost:8080")

	_, err := client.GetKeyID("/nonexistent/path/to/key.asc")
	if err == nil {
		t.Error("expected error when key file doesn't exist")
	}
}

func TestFetchAndImportGPGKeyServerDown(t *testing.T) {
	client := NewGPGKeyClient("http://localhost:9999")

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "gpg-key.asc")

	err := client.FetchAndImportGPGKey(destPath)
	if err == nil {
		t.Error("expected error when server is down")
	}
}
