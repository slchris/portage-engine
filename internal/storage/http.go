// Package storage provides interfaces and implementations for storing build artifacts.
package storage

import "fmt"

// HTTPStorage implements Storage interface for HTTP servers.
type HTTPStorage struct {
	baseURL string
}

// NewHTTPStorage creates a new HTTP storage backend.
func NewHTTPStorage(baseURL string) (*HTTPStorage, error) {
	return &HTTPStorage{baseURL: baseURL}, nil
}

// Upload uploads a file via HTTP.
func (h *HTTPStorage) Upload(_, _ string) error {
	return fmt.Errorf("HTTP storage not yet implemented")
}

// Download downloads a file via HTTP.
func (h *HTTPStorage) Download(_, _ string) error {
	return fmt.Errorf("HTTP storage not yet implemented")
}

// Delete removes a file via HTTP.
func (h *HTTPStorage) Delete(_ string) error {
	return fmt.Errorf("HTTP storage not yet implemented")
}

// List lists files via HTTP.
func (h *HTTPStorage) List(_ string) ([]string, error) {
	return nil, fmt.Errorf("HTTP storage not yet implemented")
}

// GetURL returns the URL for a file.
func (h *HTTPStorage) GetURL(remotePath string) (string, error) {
	return h.baseURL + "/" + remotePath, nil
}

// Exists checks if a file exists.
func (h *HTTPStorage) Exists(_ string) (bool, error) {
	return false, fmt.Errorf("HTTP storage not yet implemented")
}
