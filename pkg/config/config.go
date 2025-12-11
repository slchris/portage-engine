// Package config provides configuration management for Portage Engine.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ServerConfig represents the server configuration.
type ServerConfig struct {
	Port        int               `yaml:"port"`
	BinpkgPath  string            `yaml:"binpkg_path"`
	MaxWorkers  int               `yaml:"max_workers"`
	CloudConfig map[string]string `yaml:"cloud_config"`
	// Build mode: "docker" for local containers, "cloud" for IaC provisioning
	BuildMode string `yaml:"build_mode"`
	// Docker configuration for local builds
	DockerImage string `yaml:"docker_image"`
	// GPG signing configuration
	GPGEnabled bool   `yaml:"gpg_enabled"`
	GPGKeyID   string `yaml:"gpg_key_id"`
	GPGKeyPath string `yaml:"gpg_key_path"`
}

// DashboardConfig represents the dashboard configuration.
type DashboardConfig struct {
	Port           int    `yaml:"port"`
	ServerURL      string `yaml:"server_url"`
	AuthEnabled    bool   `yaml:"auth_enabled"`
	JWTSecret      string `yaml:"jwt_secret"`
	AllowAnonymous bool   `yaml:"allow_anonymous"`
}

// LoadServerConfig loads server configuration from a file.
func LoadServerConfig(path string) (*ServerConfig, error) {
	// Set defaults
	config := &ServerConfig{
		Port:       8080,
		BinpkgPath: "/var/cache/binpkgs",
		MaxWorkers: 5,
		CloudConfig: map[string]string{
			"default_provider": "gcp",
		},
		BuildMode:   "docker", // Default to local Docker builds
		DockerImage: "gentoo/stage3:latest",
		GPGEnabled:  false,
		GPGKeyID:    "",
		GPGKeyPath:  "",
	}

	// If config file doesn't exist, return defaults
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Config file not found, using defaults: %s\n", path)
		return config, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config, nil
}

// LoadDashboardConfig loads dashboard configuration from a file.
func LoadDashboardConfig(path string) (*DashboardConfig, error) {
	// Set defaults
	config := &DashboardConfig{
		Port:           8081,
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    true,
		JWTSecret:      "change-me-in-production",
		AllowAnonymous: true,
	}

	// If config file doesn't exist, return defaults
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Config file not found, using defaults: %s\n", path)
		return config, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config, nil
}
