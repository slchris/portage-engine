// Package main provides a command-line tool for submitting build requests.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/slchris/portage-engine/internal/builder"
)

var (
	serverURL      = flag.String("server", "http://localhost:8080", "Server URL")
	packageName    = flag.String("package", "", "Package name (e.g., dev-lang/python)")
	packageVersion = flag.String("version", "", "Package version")
	useFlags       = flag.String("use", "", "USE flags (comma-separated)")
	keywords       = flag.String("keywords", "", "Keywords (comma-separated)")
	configFile     = flag.String("config", "", "Portage configuration file (JSON)")
	portageDir     = flag.String("portage-dir", "", "Read configuration from Portage directory (e.g., /etc/portage)")
	arch           = flag.String("arch", "amd64", "Target architecture")
	profile        = flag.String("profile", "default/linux/amd64/23.0", "Portage profile")
	outputBundle   = flag.String("output", "", "Output configuration bundle path")
	userID         = flag.String("user", "default", "User ID")
	description    = flag.String("desc", "", "Build description")
)

func main() {
	flag.Parse()

	if *packageName == "" && *configFile == "" && *portageDir == "" {
		fmt.Println("Usage:")
		flag.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  # Simple build with USE flags:")
		fmt.Println("  portage-client -package=dev-lang/python -version=3.11 -use=ssl,threads")
		fmt.Println("")
		fmt.Println("  # Build with custom configuration:")
		fmt.Println("  portage-client -config=myconfig.json -package=dev-lang/python")
		fmt.Println("")
		fmt.Println("  # Build using system Portage configuration:")
		fmt.Println("  portage-client -portage-dir=/etc/portage -package=dev-lang/python")
		fmt.Println("")
		fmt.Println("  # Generate configuration bundle from system:")
		fmt.Println("  portage-client -portage-dir=/etc/portage -package=dev-lang/python -output=bundle.tar.gz")
		os.Exit(1)
	}

	// Load or create configuration
	var config *builder.PortageConfig
	if *portageDir != "" {
		// Read from system Portage directory
		transfer := builder.NewConfigTransfer("")
		var err error
		config, err = transfer.ReadSystemPortageConfig(*portageDir)
		if err != nil {
			log.Fatalf("Failed to read Portage configuration from %s: %v", *portageDir, err)
		}
		log.Printf("Successfully loaded configuration from %s", *portageDir)
		log.Printf("Found %d package.use entries, %d repos", len(config.PackageUse), len(config.Repos))
	} else if *configFile != "" {
		var err error
		config, err = loadConfigFromFile(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
	} else {
		config = &builder.PortageConfig{
			PackageUse:      make(map[string][]string),
			PackageKeywords: make(map[string][]string),
			MakeConf:        make(map[string]string),
			Environment:     make(map[string]string),
		}
	}

	// Parse USE flags
	var useFlagsList []string
	if *useFlags != "" {
		useFlagsList = strings.Split(*useFlags, ",")
		for i, flag := range useFlagsList {
			useFlagsList[i] = strings.TrimSpace(flag)
		}
	}

	// Parse keywords
	var keywordsList []string
	if *keywords != "" {
		keywordsList = strings.Split(*keywords, ",")
		for i, keyword := range keywordsList {
			keywordsList[i] = strings.TrimSpace(keyword)
		}
	}

	// Create package specification
	packageSpecs := []builder.PackageSpec{}
	if *packageName != "" {
		spec := builder.PackageSpec{
			Atom:     *packageName,
			Version:  *packageVersion,
			UseFlags: useFlagsList,
			Keywords: keywordsList,
		}
		packageSpecs = append(packageSpecs, spec)
	}

	packages := &builder.BuildPackageSpec{
		Packages: packageSpecs,
	}

	// Create metadata
	metadata := builder.BundleMetadata{
		UserID:      *userID,
		TargetArch:  *arch,
		Profile:     *profile,
		CreatedAt:   time.Now().Format(time.RFC3339),
		Description: *description,
	}

	// Create configuration bundle
	transfer := builder.NewConfigTransfer("")
	bundle, err := transfer.CreateConfigBundle(config, packages, metadata)
	if err != nil {
		log.Fatalf("Failed to create config bundle: %v", err)
	}

	// If output path specified, export bundle and exit
	if *outputBundle != "" {
		if err := transfer.ExportBundle(bundle, *outputBundle); err != nil {
			log.Fatalf("Failed to export bundle: %v", err)
		}
		fmt.Printf("Configuration bundle saved to: %s\n", *outputBundle)
		return
	}

	// Submit build request to server
	if err := submitBuildRequest(bundle); err != nil {
		log.Fatalf("Failed to submit build request: %v", err)
	}
}

// loadConfigFromFile loads Portage configuration from a JSON file.
func loadConfigFromFile(path string) (*builder.PortageConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config builder.PortageConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// submitBuildRequest submits a build request to the server.
func submitBuildRequest(bundle *builder.ConfigBundle) error {
	client := builder.NewBuilderClient(*serverURL)

	// For each package in the bundle, submit a build request
	for _, pkg := range bundle.Packages.Packages {
		// Convert to legacy format for now
		req := &builder.LocalBuildRequest{
			PackageName:  pkg.Atom,
			Version:      pkg.Version,
			ConfigBundle: bundle,
		}

		jobID, err := client.SubmitBuild(req)
		if err != nil {
			return fmt.Errorf("failed to submit build for %s: %w", pkg.Atom, err)
		}

		fmt.Printf("Build submitted for %s: Job ID = %s\n", pkg.Atom, jobID)

		// Poll for job status
		fmt.Println("Waiting for build to complete...")
		if err := waitForBuild(client, jobID); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}

		fmt.Printf("Build completed successfully: %s\n", pkg.Atom)
	}

	return nil
}

// waitForBuild polls the server for build status until completion.
func waitForBuild(client *builder.Client, jobID string) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeout := time.After(2 * time.Hour)

	for {
		select {
		case <-timeout:
			return fmt.Errorf("build timeout")
		case <-ticker.C:
			job, err := client.GetJobStatus(jobID)
			if err != nil {
				return err
			}

			fmt.Printf("Status: %s\n", job.Status)

			switch job.Status {
			case "success":
				if job.ArtifactURL != "" {
					fmt.Printf("Artifact: %s\n", job.ArtifactURL)
				}
				return nil
			case "failed":
				if job.Error != "" {
					return fmt.Errorf("build error: %s", job.Error)
				}
				return fmt.Errorf("build failed")
			}
		}
	}
}

// Example configuration generator
func generateExampleConfig() *builder.PortageConfig {
	config := &builder.PortageConfig{
		PackageUse: map[string][]string{
			"dev-lang/python:3.11": {"ssl", "threads", "sqlite"},
			"sys-devel/gcc":        {"openmp", "fortran"},
		},
		PackageKeywords: map[string][]string{
			"dev-lang/python:3.11": {"~amd64"},
		},
		PackageMask: []string{
			">=dev-lang/python-3.12",
		},
		PackageUnmask: []string{},
		MakeConf: map[string]string{
			"MAKEOPTS":            "-j8",
			"EMERGE_DEFAULT_OPTS": "--quiet-build=y --jobs=4",
		},
		Environment: map[string]string{
			"FEATURES": "buildpkg",
		},
		GlobalUse: []string{"systemd", "-consolekit"},
		Repos: []builder.RepoConfig{
			{
				Name:     "gentoo",
				Location: "/var/db/repos/gentoo",
				SyncType: "git",
				SyncURI:  "https://github.com/gentoo-mirror/gentoo.git",
				Priority: 100,
			},
		},
	}

	return config
}

// GenerateExampleConfigFile creates an example configuration file.
func GenerateExampleConfigFile(path string) error {
	config := generateExampleConfig()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
