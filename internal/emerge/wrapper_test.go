package emerge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewWrapper(t *testing.T) {
	config := &Config{
		Enabled:        true,
		ServerURL:      "http://localhost:8080",
		FallbackLocal:  true,
		TimeoutSeconds: 30,
		CacheDir:       "/tmp/test-cache",
	}

	_ = os.Setenv("PORTAGE_ENGINE_ORIGINAL_EMERGE", "/usr/bin/emerge")
	defer func() { _ = os.Unsetenv("PORTAGE_ENGINE_ORIGINAL_EMERGE") }()

	wrapper, err := NewWrapper(config)
	if err != nil {
		t.Fatalf("Failed to create wrapper: %v", err)
	}

	if wrapper.config.ServerURL != config.ServerURL {
		t.Errorf("Expected ServerURL %s, got %s", config.ServerURL, wrapper.config.ServerURL)
	}

	if wrapper.originalEmerge != "/usr/bin/emerge" {
		t.Errorf("Expected originalEmerge /usr/bin/emerge, got %s", wrapper.originalEmerge)
	}
}

func TestShouldBypass(t *testing.T) {
	config := &Config{Enabled: true}
	_ = os.Setenv("PORTAGE_ENGINE_ORIGINAL_EMERGE", "/usr/bin/emerge")
	defer func() { _ = os.Unsetenv("PORTAGE_ENGINE_ORIGINAL_EMERGE") }()

	wrapper, _ := NewWrapper(config)

	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{"Sync", []string{"--sync"}, true},
		{"Short sync", []string{"-s"}, true},
		{"Info", []string{"--info"}, true},
		{"Version", []string{"--version"}, true},
		{"Search", []string{"--search", "vim"}, true},
		{"Set", []string{"@world"}, true},
		{"Normal install", []string{"dev-lang/python"}, false},
		{"Install with flags", []string{"-av", "dev-lang/python"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := wrapper.shouldBypass(tt.args)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestExtractPackages(t *testing.T) {
	config := &Config{Enabled: true}
	_ = os.Setenv("PORTAGE_ENGINE_ORIGINAL_EMERGE", "/usr/bin/emerge")
	defer func() { _ = os.Unsetenv("PORTAGE_ENGINE_ORIGINAL_EMERGE") }()

	wrapper, _ := NewWrapper(config)

	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "Single package",
			args:     []string{"dev-lang/python"},
			expected: []string{"dev-lang/python"},
		},
		{
			name:     "Multiple packages",
			args:     []string{"dev-lang/python", "app-editors/vim"},
			expected: []string{"dev-lang/python", "app-editors/vim"},
		},
		{
			name:     "With flags",
			args:     []string{"-av", "dev-lang/python"},
			expected: []string{"dev-lang/python"},
		},
		{
			name:     "Mixed",
			args:     []string{"-av", "dev-lang/python", "--verbose", "app-editors/vim"},
			expected: []string{"dev-lang/python", "app-editors/vim"},
		},
		{
			name:     "No packages",
			args:     []string{"-av", "--verbose"},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := wrapper.extractPackages(tt.args)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d packages, got %d", len(tt.expected), len(result))
				return
			}
			for i, pkg := range result {
				if pkg != tt.expected[i] {
					t.Errorf("Expected package %s, got %s", tt.expected[i], pkg)
				}
			}
		})
	}
}

func TestCheckPackageAvailability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/packages/query" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		response := map[string]interface{}{
			"available": true,
			"packages":  req["packages"],
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	config := &Config{
		Enabled:   true,
		ServerURL: server.URL,
	}
	_ = os.Setenv("PORTAGE_ENGINE_ORIGINAL_EMERGE", "/usr/bin/emerge")
	defer func() { _ = os.Unsetenv("PORTAGE_ENGINE_ORIGINAL_EMERGE") }()

	wrapper, _ := NewWrapper(config)

	available, err := wrapper.checkPackageAvailability([]string{"dev-lang/python"})
	if err != nil {
		t.Fatalf("Failed to check availability: %v", err)
	}

	if !available {
		t.Error("Expected packages to be available")
	}
}

func TestModifyArgsForBinary(t *testing.T) {
	config := &Config{
		Enabled:  true,
		CacheDir: "/var/cache/binpkgs",
	}
	_ = os.Setenv("PORTAGE_ENGINE_ORIGINAL_EMERGE", "/usr/bin/emerge")
	defer func() { _ = os.Unsetenv("PORTAGE_ENGINE_ORIGINAL_EMERGE") }()

	wrapper, _ := NewWrapper(config)

	tests := []struct {
		name     string
		args     []string
		contains []string
	}{
		{
			name:     "Add usepkg",
			args:     []string{"dev-lang/python"},
			contains: []string{"--usepkg", "dev-lang/python"},
		},
		{
			name:     "Already has usepkg",
			args:     []string{"--usepkg", "dev-lang/python"},
			contains: []string{"--usepkg", "dev-lang/python"},
		},
		{
			name:     "Has usepkgonly",
			args:     []string{"-K", "dev-lang/python"},
			contains: []string{"-K", "dev-lang/python"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := wrapper.modifyArgsForBinary(tt.args)
			resultStr := strings.Join(result, " ")
			for _, required := range tt.contains {
				if !strings.Contains(resultStr, required) {
					t.Errorf("Expected result to contain '%s', got %v", required, result)
				}
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	_ = os.Setenv("PORTAGE_ENGINE_ENABLED", "1")
	_ = os.Setenv("PORTAGE_ENGINE_SERVER", "http://test:9999")
	_ = os.Setenv("PORTAGE_ENGINE_FALLBACK", "0")
	_ = os.Setenv("PORTAGE_ENGINE_CACHE", "/tmp/test")
	defer func() {
		_ = os.Unsetenv("PORTAGE_ENGINE_ENABLED")
		_ = os.Unsetenv("PORTAGE_ENGINE_SERVER")
		_ = os.Unsetenv("PORTAGE_ENGINE_FALLBACK")
		_ = os.Unsetenv("PORTAGE_ENGINE_CACHE")
	}()

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !config.Enabled {
		t.Error("Expected Enabled to be true")
	}

	if config.ServerURL != "http://test:9999" {
		t.Errorf("Expected ServerURL 'http://test:9999', got '%s'", config.ServerURL)
	}

	if config.FallbackLocal {
		t.Error("Expected FallbackLocal to be false")
	}

	if config.CacheDir != "/tmp/test" {
		t.Errorf("Expected CacheDir '/tmp/test', got '%s'", config.CacheDir)
	}
}

func TestLoadConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "portage-engine.conf")

	configData := `enabled=true
server_url=http://localhost:9090
fallback_local=false
timeout_seconds=60
cache_dir=/tmp/packages
`

	if err := os.WriteFile(configPath, []byte(configData), 0600); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	config, err := loadConfigFile(configPath)
	if err != nil {
		t.Fatalf("Failed to load config file: %v", err)
	}

	if !config.Enabled {
		t.Error("Expected Enabled to be true")
	}

	if config.ServerURL != "http://localhost:9090" {
		t.Errorf("Expected ServerURL 'http://localhost:9090', got '%s'", config.ServerURL)
	}

	if config.FallbackLocal {
		t.Error("Expected FallbackLocal to be false")
	}

	if config.TimeoutSeconds != 60 {
		t.Errorf("Expected TimeoutSeconds 60, got %d", config.TimeoutSeconds)
	}

	if config.CacheDir != "/tmp/packages" {
		t.Errorf("Expected CacheDir '/tmp/packages', got '%s'", config.CacheDir)
	}
}

func TestLoadConfigFileComments(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "portage-engine.conf")

	configData := `# This is a comment
enabled=true
# Another comment
server_url=http://localhost:9090

# Empty lines above
fallback_local=true
`

	if err := os.WriteFile(configPath, []byte(configData), 0600); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	config, err := loadConfigFile(configPath)
	if err != nil {
		t.Fatalf("Failed to load config file: %v", err)
	}

	if !config.Enabled {
		t.Error("Expected Enabled to be true")
	}

	if config.ServerURL != "http://localhost:9090" {
		t.Errorf("Expected ServerURL 'http://localhost:9090', got '%s'", config.ServerURL)
	}
}

func TestFindOriginalEmerge(t *testing.T) {
	_ = os.Setenv("PORTAGE_ENGINE_ORIGINAL_EMERGE", "/test/emerge")
	defer func() { _ = os.Unsetenv("PORTAGE_ENGINE_ORIGINAL_EMERGE") }()

	path, err := findOriginalEmerge()
	if err != nil {
		t.Fatalf("Failed to find original emerge: %v", err)
	}

	if path != "/test/emerge" {
		t.Errorf("Expected '/test/emerge', got '%s'", path)
	}
}
