package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewS3Storage tests creating a new S3 storage backend.
func TestNewS3Storage(t *testing.T) {
	storage, err := NewS3Storage("test-bucket", "us-east-1", "prefix")
	if err != nil {
		t.Fatalf("NewS3Storage failed: %v", err)
	}

	if storage == nil {
		t.Fatal("Expected non-nil storage")
	}

	if storage.bucket != "test-bucket" {
		t.Errorf("Expected bucket test-bucket, got %s", storage.bucket)
	}

	if storage.region != "us-east-1" {
		t.Errorf("Expected region us-east-1, got %s", storage.region)
	}

	if storage.prefix != "prefix" {
		t.Errorf("Expected prefix prefix, got %s", storage.prefix)
	}
}

// TestS3StorageUpload tests that Upload returns not implemented error.
func TestS3StorageUpload(t *testing.T) {
	storage, _ := NewS3Storage("test-bucket", "us-east-1", "")

	err := storage.Upload("local.txt", "remote.txt")
	if err == nil {
		t.Error("Expected not implemented error")
	}
}

// TestS3StorageDownload tests that Download returns not implemented error.
func TestS3StorageDownload(t *testing.T) {
	storage, _ := NewS3Storage("test-bucket", "us-east-1", "")

	err := storage.Download("remote.txt", "local.txt")
	if err == nil {
		t.Error("Expected not implemented error")
	}
}

// TestS3StorageDelete tests that Delete returns not implemented error.
func TestS3StorageDelete(t *testing.T) {
	storage, _ := NewS3Storage("test-bucket", "us-east-1", "")

	err := storage.Delete("remote.txt")
	if err == nil {
		t.Error("Expected not implemented error")
	}
}

// TestS3StorageList tests that List returns not implemented error.
func TestS3StorageList(t *testing.T) {
	storage, _ := NewS3Storage("test-bucket", "us-east-1", "")

	_, err := storage.List("prefix")
	if err == nil {
		t.Error("Expected not implemented error")
	}
}

// TestS3StorageGetURL tests that GetURL returns not implemented error.
func TestS3StorageGetURL(t *testing.T) {
	storage, _ := NewS3Storage("test-bucket", "us-east-1", "")

	_, err := storage.GetURL("file.txt")
	if err == nil {
		t.Error("Expected not implemented error")
	}
}

// TestS3StorageExists tests that Exists returns not implemented error.
func TestS3StorageExists(t *testing.T) {
	storage, _ := NewS3Storage("test-bucket", "us-east-1", "")

	_, err := storage.Exists("file.txt")
	if err == nil {
		t.Error("Expected not implemented error")
	}
}

// TestNewHTTPStorage tests creating a new HTTP storage backend.
func TestNewHTTPStorage(t *testing.T) {
	storage, err := NewHTTPStorage("http://localhost:8080/storage")
	if err != nil {
		t.Fatalf("NewHTTPStorage failed: %v", err)
	}

	if storage == nil {
		t.Fatal("Expected non-nil storage")
	}

	if storage.baseURL != "http://localhost:8080/storage" {
		t.Errorf("Expected baseURL http://localhost:8080/storage, got %s", storage.baseURL)
	}
}

// TestHTTPStorageUpload tests that Upload returns not implemented error.
func TestHTTPStorageUpload(t *testing.T) {
	storage, _ := NewHTTPStorage("http://localhost:8080")

	err := storage.Upload("local.txt", "remote.txt")
	if err == nil {
		t.Error("Expected not implemented error")
	}
}

// TestHTTPStorageDownload tests that Download returns not implemented error.
func TestHTTPStorageDownload(t *testing.T) {
	storage, _ := NewHTTPStorage("http://localhost:8080")

	err := storage.Download("remote.txt", "local.txt")
	if err == nil {
		t.Error("Expected not implemented error")
	}
}

// TestHTTPStorageDelete tests that Delete returns not implemented error.
func TestHTTPStorageDelete(t *testing.T) {
	storage, _ := NewHTTPStorage("http://localhost:8080")

	err := storage.Delete("remote.txt")
	if err == nil {
		t.Error("Expected not implemented error")
	}
}

// TestHTTPStorageList tests that List returns not implemented error.
func TestHTTPStorageList(t *testing.T) {
	storage, _ := NewHTTPStorage("http://localhost:8080")

	_, err := storage.List("prefix")
	if err == nil {
		t.Error("Expected not implemented error")
	}
}

// TestHTTPStorageGetURL tests GetURL functionality.
func TestHTTPStorageGetURL(t *testing.T) {
	storage, _ := NewHTTPStorage("http://localhost:8080/storage")

	url, err := storage.GetURL("file.txt")
	if err != nil {
		t.Fatalf("GetURL failed: %v", err)
	}

	expected := "http://localhost:8080/storage/file.txt"
	if url != expected {
		t.Errorf("Expected URL %s, got %s", expected, url)
	}
}

// TestHTTPStorageExists tests that Exists returns not implemented error.
func TestHTTPStorageExists(t *testing.T) {
	storage, _ := NewHTTPStorage("http://localhost:8080")

	_, err := storage.Exists("file.txt")
	if err == nil {
		t.Error("Expected not implemented error")
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

	// S3 and HTTP are not fully implemented
	s3, _ := NewS3Storage("bucket", "region", "")
	if s3 == nil {
		t.Error("S3Storage should not be nil")
	}

	http, _ := NewHTTPStorage("http://localhost:8080")
	if http == nil {
		t.Error("HTTPStorage should not be nil")
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
