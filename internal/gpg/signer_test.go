package gpg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewSigner tests creating a new GPG signer.
func TestNewSigner(t *testing.T) {
	t.Parallel()

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

// TestNewSignerWithOptions tests creating a signer with options.
func TestNewSignerWithOptions(t *testing.T) {
	t.Parallel()

	signer := NewSigner("test-key", "", true,
		WithGnupgHome("/custom/gpg"),
		WithAutoCreate("Test Name", "test@example.com"),
	)

	if signer == nil {
		t.Fatal("NewSigner returned nil")
	}

	if signer.gnupgHome != "/custom/gpg" {
		t.Errorf("Expected gnupgHome=/custom/gpg, got %s", signer.gnupgHome)
	}

	if !signer.autoCreate {
		t.Error("Expected autoCreate=true")
	}

	if signer.keyName != "Test Name" {
		t.Errorf("Expected keyName=Test Name, got %s", signer.keyName)
	}

	if signer.keyEmail != "test@example.com" {
		t.Errorf("Expected keyEmail=test@example.com, got %s", signer.keyEmail)
	}
}

// TestKeyID tests getting and setting key ID.
func TestKeyID(t *testing.T) {
	t.Parallel()

	signer := NewSigner("initial-key", "", true)

	if signer.KeyID() != "initial-key" {
		t.Errorf("Expected KeyID=initial-key, got %s", signer.KeyID())
	}

	signer.SetKeyID("new-key")

	if signer.KeyID() != "new-key" {
		t.Errorf("Expected KeyID=new-key, got %s", signer.KeyID())
	}
}

// TestIsEnabled tests checking if signing is enabled.
func TestIsEnabled(t *testing.T) {
	t.Parallel()

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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			signer := NewSigner(tt.keyID, tt.keyPath, tt.enabled)
			if signer.IsEnabled() != tt.expected {
				t.Errorf("Expected IsEnabled=%v, got %v", tt.expected, signer.IsEnabled())
			}
		})
	}
}

// TestInitializeDisabled tests that Initialize does nothing when disabled.
func TestInitializeDisabled(t *testing.T) {
	t.Parallel()

	signer := NewSigner("", "", false)
	if err := signer.Initialize(); err != nil {
		t.Errorf("Initialize should succeed when disabled: %v", err)
	}
}

// TestInitializeNoKey tests Initialize fails without key when not auto-create.
func TestInitializeNoKey(t *testing.T) {
	t.Parallel()

	signer := NewSigner("", "", true)
	err := signer.Initialize()
	if err == nil {
		t.Error("Initialize should fail without key when auto-create disabled")
	}
	if !strings.Contains(err.Error(), "no key ID configured") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestBuildBaseArgs tests building base GPG arguments.
func TestBuildBaseArgs(t *testing.T) {
	t.Parallel()

	// Without keyPath
	signer := NewSigner("test-key", "", true)
	args := signer.buildBaseArgs()
	if len(args) != 0 {
		t.Errorf("Expected empty args without keyPath, got %v", args)
	}

	// With non-existent keyPath (should not add args)
	signer2 := NewSigner("test-key", "/nonexistent/path", true)
	args2 := signer2.buildBaseArgs()
	if len(args2) != 0 {
		t.Errorf("Expected empty args with non-existent keyPath, got %v", args2)
	}
}

// TestSignPackage tests package signing functionality.
func TestSignPackage(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a test package file
	pkgPath := filepath.Join(tmpDir, "test-package.tbz2")
	if err := os.WriteFile(pkgPath, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	signer := NewSigner("", "", false)

	// Should not create signature when disabled
	err := signer.SignPackage(pkgPath)
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
	t.Parallel()

	tmpDir := t.TempDir()

	pkgPath := filepath.Join(tmpDir, "test-package.tbz2")
	if err := os.WriteFile(pkgPath, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	signer := NewSigner("test-key", "/path/to/key", false)

	// Should not error when disabled
	err := signer.SignPackage(pkgPath)
	if err != nil {
		t.Errorf("SignPackage should not error when disabled: %v", err)
	}
}

// TestSignPackageNoKeyID tests signing fails without key ID.
func TestSignPackageNoKeyID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	pkgPath := filepath.Join(tmpDir, "test-package.tbz2")
	if err := os.WriteFile(pkgPath, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	signer := NewSigner("", "", true) // enabled but no key ID

	err := signer.SignPackage(pkgPath)
	if err == nil {
		t.Error("SignPackage should fail without key ID")
	}
	if !strings.Contains(err.Error(), "key ID not configured") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestGetPublicKeyNoKeyID tests GetPublicKey fails without key ID.
func TestGetPublicKeyNoKeyID(t *testing.T) {
	t.Parallel()

	signer := NewSigner("", "", true)

	_, err := signer.GetPublicKey()
	if err == nil {
		t.Error("GetPublicKey should fail without key ID")
	}
	if !strings.Contains(err.Error(), "no key ID configured") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestExportPublicKeyNoKeyID tests ExportPublicKey fails without key ID.
func TestExportPublicKeyNoKeyID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	signer := NewSigner("", "", true)

	err := signer.ExportPublicKey(filepath.Join(tmpDir, "key.asc"))
	if err == nil {
		t.Error("ExportPublicKey should fail without key ID")
	}
}

// TestSignDirectoryDisabled tests SignDirectory when disabled.
func TestSignDirectoryDisabled(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	signer := NewSigner("test-key", "", false)

	err := signer.SignDirectory(tmpDir)
	if err != nil {
		t.Errorf("SignDirectory should succeed when disabled: %v", err)
	}
}
