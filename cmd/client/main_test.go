package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/slchris/portage-engine/internal/builder"
)

func TestLoadConfigFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	configData := `{
  "package_use": {
    "dev-lang/python": ["ssl", "threads"]
  },
  "package_keywords": {
    "dev-lang/rust": ["~amd64"]
  },
  "make_conf": {
    "CFLAGS": "-O2 -pipe",
    "MAKEOPTS": "-j4"
  },
  "environment": {
    "LC_ALL": "C"
  }
}`

	if err := os.WriteFile(configPath, []byte(configData), 0600); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	config, err := loadConfigFromFile(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if len(config.PackageUse) != 1 {
		t.Errorf("Expected 1 package.use entry, got %d", len(config.PackageUse))
	}

	if len(config.PackageKeywords) != 1 {
		t.Errorf("Expected 1 package.keywords entry, got %d", len(config.PackageKeywords))
	}

	if len(config.MakeConf) != 2 {
		t.Errorf("Expected 2 make.conf entries, got %d", len(config.MakeConf))
	}
}

func TestLoadConfigFromFileInvalid(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.json")

	configData := `invalid json`

	if err := os.WriteFile(configPath, []byte(configData), 0600); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	_, err := loadConfigFromFile(configPath)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestLoadConfigFromFileNotExist(t *testing.T) {
	_, err := loadConfigFromFile("/nonexistent/path/config.json")
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}
}

func TestCreateConfigBundle(t *testing.T) {
	config := &builder.PortageConfig{
		PackageUse: map[string][]string{
			"dev-lang/python": {"ssl", "threads"},
		},
		PackageKeywords: map[string][]string{
			"dev-lang/rust": {"~amd64"},
		},
		MakeConf: map[string]string{
			"CFLAGS":   "-O2 -pipe",
			"MAKEOPTS": "-j4",
		},
		Environment: map[string]string{
			"LC_ALL": "C",
		},
	}

	packages := &builder.BuildPackageSpec{
		Packages: []builder.PackageSpec{
			{
				Atom:     "dev-lang/python",
				Version:  "3.11",
				UseFlags: []string{"ssl", "threads"},
			},
		},
	}

	metadata := builder.BundleMetadata{
		UserID:      "test-user",
		TargetArch:  "amd64",
		Profile:     "default/linux/amd64/23.0",
		Description: "Test build",
	}

	transfer := builder.NewConfigTransfer("")
	bundle, err := transfer.CreateConfigBundle(config, packages, metadata)
	if err != nil {
		t.Fatalf("Failed to create config bundle: %v", err)
	}

	if bundle == nil {
		t.Fatal("Expected non-nil bundle")
	}

	if bundle.Config == nil {
		t.Error("Expected non-nil bundle.Config")
	}

	if bundle.Packages == nil {
		t.Error("Expected non-nil bundle.Packages")
	}

	if bundle.Metadata.UserID != "test-user" {
		t.Errorf("Expected UserID 'test-user', got '%s'", bundle.Metadata.UserID)
	}
}

func TestParseFlagsAndKeywords(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Single flag",
			input:    "ssl",
			expected: []string{"ssl"},
		},
		{
			name:     "Multiple flags",
			input:    "ssl,threads,ipv6",
			expected: []string{"ssl", "threads", "ipv6"},
		},
		{
			name:     "Flags with spaces",
			input:    "ssl, threads, ipv6",
			expected: []string{"ssl", "threads", "ipv6"},
		},
		{
			name:     "Empty string",
			input:    "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result []string
			if tt.input != "" {
				parts := splitAndTrim(tt.input, ",")
				result = parts
			}

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d items, got %d", len(tt.expected), len(result))
				return
			}

			for i, exp := range tt.expected {
				if result[i] != exp {
					t.Errorf("Expected item %d to be '%s', got '%s'", i, exp, result[i])
				}
			}
		})
	}
}

func splitAndTrim(s, sep string) []string {
	if s == "" {
		return nil
	}
	parts := make([]string, 0)
	for _, p := range splitString(s, sep) {
		trimmed := trimSpace(p)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func splitString(s, sep string) []string {
	if s == "" {
		return []string{}
	}
	result := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
