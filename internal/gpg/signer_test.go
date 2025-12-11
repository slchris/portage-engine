package gpg

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewSigner tests creating a new GPG signer.
func TestNewSigner(t *testing.T) {
	signer := NewSigner("test-key-id", "/path/to/key", true)

	if signer == nil {
		t.Fatal("NewSigner returned nil")
	}

	if signer.keyID != "test-key-id" {
		t.Errorf("Expected keyID=test-key-id, got %s", signer.keyID)
	}

	if signer.keyPath != "/path/to/key" {
		t.Errorf("Expected keyPath=/path/to/key, got %s", signer.keyPath)
	}

	if !signer.enabled {
		t.Error("Expected enabled=true, got false")
	}
}

// TestIsEnabled tests checking if signing is enabled.
func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		keyID    string
		keyPath  string
		enabled  bool
		expected bool
	}{
		{
			name:     "Enabled",
			keyID:    "test-key",
			keyPath:  "/path/to/key",
			enabled:  true,
			expected: true,
		},
		{
			name:     "Disabled",
			keyID:    "test-key",
			keyPath:  "/path/to/key",
			enabled:  false,
			expected: false,
		},
		{
			name:     "Not enabled",
			keyID:    "",
			keyPath:  "",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signer := NewSigner(tt.keyID, tt.keyPath, tt.enabled)
			if signer.IsEnabled() != tt.expected {
				t.Errorf("Expected IsEnabled=%v, got %v", tt.expected, signer.IsEnabled())
			}
		})
	}
}

// TestSignPackage tests package signing functionality.
func TestSignPackage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gpg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	// Create a test package file
	pkgPath := filepath.Join(tmpDir, "test-package.tbz2")
	if err := os.WriteFile(pkgPath, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	signer := NewSigner("", "", false)

	// Should not create signature when disabled
	err = signer.SignPackage(pkgPath)
	if err != nil {
		t.Errorf("SignPackage should succeed when disabled: %v", err)
	}

	// Verify no signature file was created
	sigFile := pkgPath + ".sig"
	if _, err := os.Stat(sigFile); err == nil {
		t.Error("Signature file should not be created when signing is disabled")
	}
}

// TestSignPackageDisabled tests that signing is skipped when disabled.
func TestSignPackageDisabled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gpg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	pkgPath := filepath.Join(tmpDir, "test-package.tbz2")
	if err := os.WriteFile(pkgPath, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	signer := NewSigner("test-key", "/path/to/key", false)

	// Should not error when disabled
	err = signer.SignPackage(pkgPath)
	if err != nil {
		t.Errorf("SignPackage should not error when disabled: %v", err)
	}
}
