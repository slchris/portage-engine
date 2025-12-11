package binpkg

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewStore tests creating a new binary package store.
func TestNewStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "binpkg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	store := NewStore(tmpDir)
	if store == nil {
		t.Fatal("NewStore returned nil")
	}

	if store.basePath != tmpDir {
		t.Errorf("Expected basePath=%s, got %s", tmpDir, store.basePath)
	}
}

// TestQuery tests querying for packages.
func TestQuery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "binpkg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	store := NewStore(tmpDir)

	// Create a test package structure
	pkgDir := filepath.Join(tmpDir, "amd64", "dev-lang")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("Failed to create package dir: %v", err)
	}

	// Create a test package file
	pkgFile := filepath.Join(pkgDir, "python-3.11.0.tbz2")
	if err := os.WriteFile(pkgFile, []byte("test package"), 0644); err != nil {
		t.Fatalf("Failed to create package file: %v", err)
	}

	// Add package to store explicitly since loadPackages doesn't scan
	pkg := &Package{
		Name:    "python",
		Version: "3.11.0",
		Arch:    "amd64",
		Path:    pkgFile,
	}
	if err := store.Add(pkg); err != nil {
		t.Fatalf("Failed to add package: %v", err)
	}

	req := &QueryRequest{
		Name:     "python",
		Version:  "3.11.0",
		Arch:     "amd64",
		UseFlags: []string{},
	}

	foundPkg, found := store.Query(req)

	if !found {
		t.Error("Package should be found")
	}

	if foundPkg == nil {
		t.Fatal("Package is nil")
	}

	if foundPkg.Name != "python" {
		t.Errorf("Expected name=python, got %s", foundPkg.Name)
	}

	if foundPkg.Version != "3.11.0" {
		t.Errorf("Expected version=3.11.0, got %s", foundPkg.Version)
	}
}

// TestQueryNotFound tests querying for non-existent package.
func TestQueryNotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "binpkg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	store := NewStore(tmpDir)

	req := &QueryRequest{
		Name:     "dev-lang/nonexistent",
		Version:  "1.0.0",
		Arch:     "amd64",
		UseFlags: []string{},
	}

	_, found := store.Query(req)

	if found {
		t.Error("Non-existent package should not be found")
	}
}

// TestQueryRequest tests QueryRequest structure.
func TestQueryRequest(t *testing.T) {
	req := &QueryRequest{
		Name:     "dev-lang/python",
		Version:  "3.11",
		Arch:     "amd64",
		UseFlags: []string{"ssl", "threads"},
	}

	if req.Name != "dev-lang/python" {
		t.Errorf("Expected Name=dev-lang/python, got %s", req.Name)
	}

	if len(req.UseFlags) != 2 {
		t.Errorf("Expected 2 USE flags, got %d", len(req.UseFlags))
	}
}

// TestPackage tests Package structure.
func TestPackage(t *testing.T) {
	pkg := &Package{
		Name:     "python",
		Version:  "3.11.0",
		Arch:     "amd64",
		UseFlags: []string{"ssl", "threads"},
		Path:     "/path/to/package.tbz2",
	}

	if pkg.Name != "python" {
		t.Errorf("Expected Name=python, got %s", pkg.Name)
	}

	if pkg.Version != "3.11.0" {
		t.Errorf("Expected Version=3.11.0, got %s", pkg.Version)
	}
}
