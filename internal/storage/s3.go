package storage

import "fmt"

// S3Storage implements Storage interface for AWS S3. The backend is not yet
// implemented; the fields are placeholders for a future implementation.
type S3Storage struct{}

// NewS3Storage creates a new S3 storage backend.
//
// The S3 backend is not yet implemented, so the constructor fails immediately.
// This surfaces a misconfiguration (STORAGE_TYPE=s3) at startup rather than
// letting every artifact upload fail silently at runtime.
func NewS3Storage(_, _, _ string) (*S3Storage, error) {
	return nil, fmt.Errorf("S3 storage backend is not yet implemented (STORAGE_TYPE=s3); use STORAGE_TYPE=local")
}

// Upload uploads a file to S3.
func (s *S3Storage) Upload(_, _ string) error {
	return fmt.Errorf("S3 storage not yet implemented")
}

// Download downloads a file from S3.
func (s *S3Storage) Download(_, _ string) error {
	return fmt.Errorf("S3 storage not yet implemented")
}

// Delete removes a file from S3.
func (s *S3Storage) Delete(_ string) error {
	return fmt.Errorf("S3 storage not yet implemented")
}

// List lists files in S3.
func (s *S3Storage) List(_ string) ([]string, error) {
	return nil, fmt.Errorf("S3 storage not yet implemented")
}

// GetURL returns the URL for a file.
func (s *S3Storage) GetURL(_ string) (string, error) {
	return "", fmt.Errorf("S3 storage not yet implemented")
}

// Exists checks if a file exists.
func (s *S3Storage) Exists(_ string) (bool, error) {
	return false, fmt.Errorf("S3 storage not yet implemented")
}
