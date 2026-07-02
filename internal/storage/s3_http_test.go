package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewS3Storage verifies the S3 constructor fails fast (backend not yet
// implemented), so a STORAGE_TYPE=s3 misconfiguration is caught at startup.
func TestNewS3Storage(t *testing.T) {
	storage, err := NewS3Storage("test-bucket", "us-east-1", "prefix")
	if err == nil {
		t.Fatal("expected an error: S3 backend is not implemented")
	}
	if storage != nil {
		t.Error("expected nil storage on constructor failure")
	}
}

// TestNewHTTPStorage verifies the HTTP constructor fails fast (backend not yet
// implemented), so a STORAGE_TYPE=http misconfiguration is caught at startup.
func TestNewHTTPStorage(t *testing.T) {
	storage, err := NewHTTPStorage("http://localhost:8080/storage")
	if err == nil {
		t.Fatal("expected an error: HTTP backend is not implemented")
	}
	if storage != nil {
		t.Error("expected nil storage on constructor failure")
	}
}

// TestStorageInterfaceCompliance tests interface compliance.
func TestStorageInterfaceCompliance(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage-interface-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	var _ Storage = &LocalStorage{}
	var _ Storage = &S3Storage{}
	var _ Storage = &HTTPStorage{}

	// Test LocalStorage implements interface
	local, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}
	testStorageInterface(t, local, "test.txt", testFile)

	// S3 and HTTP backends are not implemented: their constructors error.
	if _, err := NewS3Storage("bucket", "region", ""); err == nil {
		t.Error("expected NewS3Storage to error (not implemented)")
	}
	if _, err := NewHTTPStorage("http://localhost:8080"); err == nil {
		t.Error("expected NewHTTPStorage to error (not implemented)")
	}
}

// testStorageInterface is a helper to test Storage interface methods.
func testStorageInterface(t *testing.T, s Storage, remotePath, localPath string) {
	// Test Upload
	if err := s.Upload(localPath, remotePath); err != nil {
		t.Errorf("Upload failed: %v", err)
	}

	// Test GetURL
	if _, err := s.GetURL(remotePath); err != nil {
		t.Errorf("GetURL failed: %v", err)
	}

	// Test Exists
	if exists, err := s.Exists(remotePath); err != nil || !exists {
		t.Errorf("Exists failed or returned false: %v", err)
	}

	// Test List
	if _, err := s.List(""); err != nil {
		t.Errorf("List failed: %v", err)
	}
}
