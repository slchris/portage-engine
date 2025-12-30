// Package emerge provides emerge command wrapper for Portage Engine integration.
package emerge

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Config represents the emerge wrapper configuration.
type Config struct {
	Enabled        bool   `json:"enabled"`
	ServerURL      string `json:"server_url"`
	FallbackLocal  bool   `json:"fallback_local"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	CacheDir       string `json:"cache_dir"`
}

// Wrapper wraps the emerge command to use Portage Engine.
type Wrapper struct {
	config         *Config
	originalEmerge string
	httpClient     *http.Client
}

// NewWrapper creates a new emerge wrapper.
func NewWrapper(config *Config) (*Wrapper, error) {
	originalEmerge, err := findOriginalEmerge()
	if err != nil {
		return nil, fmt.Errorf("failed to find original emerge: %w", err)
	}

	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	if config.TimeoutSeconds == 0 {
		timeout = 30 * time.Second
	}

	return &Wrapper{
		config:         config,
		originalEmerge: originalEmerge,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// Execute runs emerge with Portage Engine integration.
func (w *Wrapper) Execute(args []string) error {
	if !w.config.Enabled {
		return w.runOriginalEmerge(args)
	}

	if w.shouldBypass(args) {
		return w.runOriginalEmerge(args)
	}

	packages := w.extractPackages(args)
	if len(packages) == 0 {
		return w.runOriginalEmerge(args)
	}

	available, err := w.checkPackageAvailability(packages)
	if err != nil || !available {
		if w.config.FallbackLocal {
			return w.runOriginalEmerge(args)
		}
		return fmt.Errorf("packages not available on server: %w", err)
	}

	if err := w.downloadPackages(packages); err != nil {
		if w.config.FallbackLocal {
			return w.runOriginalEmerge(args)
		}
		return fmt.Errorf("failed to download packages: %w", err)
	}

	newArgs := w.modifyArgsForBinary(args)
	return w.runOriginalEmerge(newArgs)
}

// shouldBypass determines if we should bypass the wrapper.
func (w *Wrapper) shouldBypass(args []string) bool {
	for _, arg := range args {
		if arg == "--sync" || arg == "-s" ||
			arg == "--info" || arg == "--version" ||
			arg == "--search" || arg == "-S" ||
			arg == "--searchdesc" || strings.HasPrefix(arg, "@") {
			return true
		}
	}
	return false
}

// extractPackages extracts package names from emerge arguments.
func (w *Wrapper) extractPackages(args []string) []string {
	packages := []string{}
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "@") {
			packages = append(packages, arg)
		}
	}
	return packages
}

// checkPackageAvailability checks if packages are available on server.
func (w *Wrapper) checkPackageAvailability(packages []string) (bool, error) {
	reqBody := map[string]interface{}{
		"packages": packages,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return false, err
	}

	url := fmt.Sprintf("%s/api/v1/packages/query", w.config.ServerURL)
	resp, err := w.httpClient.Post(url, "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, err
	}

	available, ok := result["available"].(bool)
	if !ok {
		return false, fmt.Errorf("invalid response format")
	}

	return available, nil
}

// downloadPackages downloads binary packages from server.
func (w *Wrapper) downloadPackages(packages []string) error {
	if w.config.CacheDir == "" {
		w.config.CacheDir = "/var/cache/binpkgs"
	}

	if err := os.MkdirAll(w.config.CacheDir, 0750); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}

	for _, pkg := range packages {
		url := fmt.Sprintf("%s/api/v1/packages/download?package=%s", w.config.ServerURL, pkg)
		resp, err := w.httpClient.Get(url)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", pkg, err)
		}

		pkgFile := fmt.Sprintf("%s/%s.tbz2", w.config.CacheDir, strings.ReplaceAll(pkg, "/", "_"))
		out, err := os.Create(pkgFile)
		if err != nil {
			_ = resp.Body.Close()
			return fmt.Errorf("failed to create file: %w", err)
		}

		_, err = io.Copy(out, resp.Body)
		_ = out.Close()
		_ = resp.Body.Close()

		if err != nil {
			return fmt.Errorf("failed to write package: %w", err)
		}
	}

	return nil
}

// modifyArgsForBinary modifies emerge args to use binary packages.
func (w *Wrapper) modifyArgsForBinary(args []string) []string {
	newArgs := []string{}
	hasUsepkg := false

	for _, arg := range args {
		if arg == "--usepkg" || arg == "-k" || arg == "--usepkgonly" || arg == "-K" {
			hasUsepkg = true
		}
		newArgs = append(newArgs, arg)
	}

	if !hasUsepkg {
		newArgs = append([]string{"--usepkg"}, newArgs...)
	}

	hasBinpkgDir := false
	for _, arg := range newArgs {
		if strings.HasPrefix(arg, "PKGDIR=") {
			hasBinpkgDir = true
			break
		}
	}

	if !hasBinpkgDir && w.config.CacheDir != "" {
		newArgs = append([]string{fmt.Sprintf("PKGDIR=%s", w.config.CacheDir)}, newArgs...)
	}

	return newArgs
}

// runOriginalEmerge executes the original emerge command.
func (w *Wrapper) runOriginalEmerge(args []string) error {
	cmd := exec.Command(w.originalEmerge, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// findOriginalEmerge finds the path to the original emerge binary.
func findOriginalEmerge() (string, error) {
	emergeOriginal := os.Getenv("PORTAGE_ENGINE_ORIGINAL_EMERGE")
	if emergeOriginal != "" {
		return emergeOriginal, nil
	}

	paths := []string{
		"/usr/bin/emerge.original",
		"/usr/bin/emerge-original",
		"/usr/local/bin/emerge.original",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("original emerge not found")
}

// LoadConfig loads wrapper configuration from file or environment.
func LoadConfig() (*Config, error) {
	config := &Config{
		Enabled:        false,
		ServerURL:      "http://localhost:8080",
		FallbackLocal:  true,
		TimeoutSeconds: 30,
		CacheDir:       "/var/cache/binpkgs",
	}

	envEnabled := os.Getenv("PORTAGE_ENGINE_ENABLED")
	if envEnabled == "1" || envEnabled == "true" {
		config.Enabled = true
	}

	if serverURL := os.Getenv("PORTAGE_ENGINE_SERVER"); serverURL != "" {
		config.ServerURL = serverURL
	}

	if fallback := os.Getenv("PORTAGE_ENGINE_FALLBACK"); fallback == "0" || fallback == "false" {
		config.FallbackLocal = false
	}

	if cacheDir := os.Getenv("PORTAGE_ENGINE_CACHE"); cacheDir != "" {
		config.CacheDir = cacheDir
	}

	confPath := "/etc/portage/portage-engine.conf"
	if _, err := os.Stat(confPath); err == nil {
		fileConfig, err := loadConfigFile(confPath)
		if err == nil {
			if fileConfig.Enabled {
				config.Enabled = fileConfig.Enabled
			}
			if fileConfig.ServerURL != "" {
				config.ServerURL = fileConfig.ServerURL
			}
			if fileConfig.TimeoutSeconds > 0 {
				config.TimeoutSeconds = fileConfig.TimeoutSeconds
			}
			if fileConfig.CacheDir != "" {
				config.CacheDir = fileConfig.CacheDir
			}
			config.FallbackLocal = fileConfig.FallbackLocal
		}
	}

	return config, nil
}

// loadConfigFile loads configuration from a file.
func loadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	config := &Config{}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "enabled":
			config.Enabled = value == "true" || value == "1"
		case "server_url":
			config.ServerURL = value
		case "fallback_local":
			config.FallbackLocal = value == "true" || value == "1"
		case "timeout_seconds":
			_, _ = fmt.Sscanf(value, "%d", &config.TimeoutSeconds)
		case "cache_dir":
			config.CacheDir = value
		}
	}

	return config, nil
}
