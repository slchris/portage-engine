package builder

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCreateConfigBundle tests the creation of configuration bundles.
func TestCreateConfigBundle(t *testing.T) {
	config := &PortageConfig{
		PackageUse: map[string][]string{
			"dev-lang/python:3.11": {"ssl", "threads"},
		},
		MakeConf: map[string]string{
			"MAKEOPTS": "-j8",
		},
	}

	packages := &BuildPackageSpec{
		Packages: []PackageSpec{
			{
				Atom:     "dev-lang/python:3.11",
				Version:  "3.11.8",
				UseFlags: []string{"ssl", "threads"},
			},
		},
	}

	metadata := BundleMetadata{
		UserID:     "test-user",
		TargetArch: "amd64",
		Profile:    "default/linux/amd64/23.0",
		CreatedAt:  time.Now().Format(time.RFC3339),
	}

	transfer := NewConfigTransfer("")
	bundle, err := transfer.CreateConfigBundle(config, packages, metadata)
	if err != nil {
		t.Fatalf("Failed to create config bundle: %v", err)
	}

	if bundle == nil {
		t.Fatal("Bundle is nil")
	}

	if len(bundle.Packages.Packages) != 1 {
		t.Errorf("Expected 1 package, got %d", len(bundle.Packages.Packages))
	}

	if bundle.Config.MakeConf["MAKEOPTS"] != "-j8" {
		t.Errorf("Expected MAKEOPTS=-j8, got %s", bundle.Config.MakeConf["MAKEOPTS"])
	}
}

// TestExportImportBundle tests exporting and importing configuration bundles.
func TestExportImportBundle(t *testing.T) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "portage-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	config := &PortageConfig{
		PackageUse: map[string][]string{
			"dev-lang/python:3.11": {"ssl", "threads"},
		},
		PackageKeywords: map[string][]string{
			"dev-lang/python:3.11": {"~amd64"},
		},
		MakeConf: map[string]string{
			"MAKEOPTS": "-j8",
		},
		GlobalUse: []string{"systemd", "-consolekit"},
	}

	packages := &BuildPackageSpec{
		Packages: []PackageSpec{
			{
				Atom:     "dev-lang/python:3.11",
				Version:  "3.11.8",
				UseFlags: []string{"ssl", "threads"},
			},
		},
	}

	metadata := BundleMetadata{
		UserID:     "test-user",
		TargetArch: "amd64",
		Profile:    "default/linux/amd64/23.0",
		CreatedAt:  time.Now().Format(time.RFC3339),
	}

	transfer := NewConfigTransfer(tmpDir)

	// Create bundle
	bundle, err := transfer.CreateConfigBundle(config, packages, metadata)
	if err != nil {
		t.Fatalf("Failed to create config bundle: %v", err)
	}

	// Export bundle
	bundlePath := filepath.Join(tmpDir, "test-bundle.tar.gz")
	if err := transfer.ExportBundle(bundle, bundlePath); err != nil {
		t.Fatalf("Failed to export bundle: %v", err)
	}

	// Verify bundle file exists
	if _, err := os.Stat(bundlePath); os.IsNotExist(err) {
		t.Fatalf("Bundle file does not exist: %s", bundlePath)
	}

	// Import bundle
	importedBundle, err := transfer.ImportBundle(bundlePath)
	if err != nil {
		t.Fatalf("Failed to import bundle: %v", err)
	}

	// Verify imported data
	if importedBundle.Metadata.UserID != metadata.UserID {
		t.Errorf("Expected UserID=%s, got %s", metadata.UserID, importedBundle.Metadata.UserID)
	}

	if len(importedBundle.Packages.Packages) != 1 {
		t.Errorf("Expected 1 package, got %d", len(importedBundle.Packages.Packages))
	}

	if importedBundle.Config.MakeConf["MAKEOPTS"] != "-j8" {
		t.Errorf("Expected MAKEOPTS=-j8, got %s", importedBundle.Config.MakeConf["MAKEOPTS"])
	}
}

// TestApplyConfigToSystem tests applying configuration to a system.
func TestApplyConfigToSystem(t *testing.T) {
	// Create a temporary directory to simulate a target system
	tmpDir, err := os.MkdirTemp("", "portage-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	config := &PortageConfig{
		PackageUse: map[string][]string{
			"dev-lang/python:3.11": {"ssl", "threads"},
		},
		PackageKeywords: map[string][]string{
			"dev-lang/python:3.11": {"~amd64"},
		},
		PackageMask: []string{
			">=dev-lang/python-3.12",
		},
		MakeConf: map[string]string{
			"MAKEOPTS": "-j8",
		},
	}

	packages := &BuildPackageSpec{
		Packages: []PackageSpec{
			{
				Atom:    "dev-lang/python:3.11",
				Version: "3.11.8",
			},
		},
	}

	metadata := BundleMetadata{
		UserID:     "test-user",
		TargetArch: "amd64",
	}

	transfer := NewConfigTransfer("")
	bundle, err := transfer.CreateConfigBundle(config, packages, metadata)
	if err != nil {
		t.Fatalf("Failed to create config bundle: %v", err)
	}

	// Apply config to temp directory
	if err := transfer.ApplyConfigToSystem(bundle, tmpDir); err != nil {
		t.Fatalf("Failed to apply config to system: %v", err)
	}

	// Verify files were created
	portageDir := filepath.Join(tmpDir, "etc", "portage")

	// Check package.use
	packageUsePath := filepath.Join(portageDir, "package.use", "00-user")
	if _, err := os.Stat(packageUsePath); os.IsNotExist(err) {
		t.Errorf("package.use file not created: %s", packageUsePath)
	} else {
		content, err := os.ReadFile(packageUsePath)
		if err != nil {
			t.Errorf("Failed to read package.use: %v", err)
		} else {
			contentStr := string(content)
			if !contains(contentStr, "dev-lang/python:3.11") {
				t.Errorf("package.use does not contain expected content")
			}
		}
	}

	// Check package.accept_keywords
	keywordsPath := filepath.Join(portageDir, "package.accept_keywords", "00-user")
	if _, err := os.Stat(keywordsPath); os.IsNotExist(err) {
		t.Errorf("package.accept_keywords file not created: %s", keywordsPath)
	}

	// Check package.mask
	maskPath := filepath.Join(portageDir, "package.mask", "00-user")
	if _, err := os.Stat(maskPath); os.IsNotExist(err) {
		t.Errorf("package.mask file not created: %s", maskPath)
	} else {
		content, err := os.ReadFile(maskPath)
		if err != nil {
			t.Errorf("Failed to read package.mask: %v", err)
		} else {
			contentStr := string(content)
			if !contains(contentStr, ">=dev-lang/python-3.12") {
				t.Errorf("package.mask does not contain expected content")
			}
		}
	}

	// Check make.conf
	makeConfPath := filepath.Join(portageDir, "make.conf.d", "00-user")
	if _, err := os.Stat(makeConfPath); os.IsNotExist(err) {
		t.Errorf("make.conf file not created: %s", makeConfPath)
	} else {
		content, err := os.ReadFile(makeConfPath)
		if err != nil {
			t.Errorf("Failed to read make.conf: %v", err)
		} else {
			contentStr := string(content)
			if !contains(contentStr, "MAKEOPTS") {
				t.Errorf("make.conf does not contain expected content")
			}
		}
	}
}

// TestPackageSpec tests package specification.
func TestPackageSpec(t *testing.T) {
	spec := PackageSpec{
		Atom:     "dev-lang/python:3.11",
		Version:  "3.11.8",
		UseFlags: []string{"ssl", "threads", "-test"},
		Keywords: []string{"~amd64"},
		Environment: map[string]string{
			"PYTHON_TARGETS": "python3_11",
		},
	}

	if spec.Atom != "dev-lang/python:3.11" {
		t.Errorf("Expected Atom=dev-lang/python:3.11, got %s", spec.Atom)
	}

	if len(spec.UseFlags) != 3 {
		t.Errorf("Expected 3 USE flags, got %d", len(spec.UseFlags))
	}

	if spec.Environment["PYTHON_TARGETS"] != "python3_11" {
		t.Errorf("Expected PYTHON_TARGETS=python3_11, got %s", spec.Environment["PYTHON_TARGETS"])
	}
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

// findSubstring finds a substring in a string.
func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
