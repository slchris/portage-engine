// Package main provides a command-line tool for submitting build requests.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
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
		printUsage()
		os.Exit(1)
	}

	config := loadPortageConfig()
	useFlagsList := parseFlags(*useFlags)
	keywordsList := parseFlags(*keywords)
	packageSpecs := createPackageSpecs(useFlagsList, keywordsList)

	bundle := createConfigBundle(config, packageSpecs)

	if *outputBundle != "" {
		exportBundle(bundle)
		return
	}

	submitBuildRequest(bundle)
}

// printUsage prints usage information.
func printUsage() {
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
}

// loadPortageConfig loads Portage configuration from various sources.
func loadPortageConfig() *builder.PortageConfig {
	switch {
	case *portageDir != "":
		return loadFromPortageDir()
	case *configFile != "":
		config, err := loadConfigFromFile(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		return config
	default:
		return createEmptyConfig()
	}
}

// loadFromPortageDir loads config from system Portage directory.
func loadFromPortageDir() *builder.PortageConfig {
	transfer := builder.NewConfigTransfer("")
	config, err := transfer.ReadSystemPortageConfig(*portageDir)
	if err != nil {
		log.Fatalf("Failed to read Portage configuration from %s: %v", *portageDir, err)
	}
	log.Printf("Successfully loaded configuration from %s", *portageDir)
	log.Printf("Found %d package.use entries, %d repos", len(config.PackageUse), len(config.Repos))
	return config
}

// createEmptyConfig creates an empty Portage configuration.
func createEmptyConfig() *builder.PortageConfig {
	return &builder.PortageConfig{
		PackageUse:      make(map[string][]string),
		PackageKeywords: make(map[string][]string),
		MakeConf:        make(map[string]string),
		Environment:     make(map[string]string),
	}
}

// parseFlags parses comma-separated flags.
func parseFlags(flags string) []string {
	if flags == "" {
		return nil
	}

	parts := strings.Split(flags, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return parts
}

// createPackageSpecs creates package specifications.
func createPackageSpecs(useFlagsList, keywordsList []string) []builder.PackageSpec {
	if *packageName == "" {
		return []builder.PackageSpec{}
	}

	spec := builder.PackageSpec{
		Atom:     *packageName,
		Version:  *packageVersion,
		UseFlags: useFlagsList,
		Keywords: keywordsList,
	}
	return []builder.PackageSpec{spec}
}

// createConfigBundle creates a configuration bundle.
func createConfigBundle(config *builder.PortageConfig, packageSpecs []builder.PackageSpec) *builder.ConfigBundle {
	packages := &builder.BuildPackageSpec{
		Packages: packageSpecs,
	}

	metadata := builder.BundleMetadata{
		UserID:      *userID,
		TargetArch:  *arch,
		Profile:     *profile,
		CreatedAt:   time.Now().Format(time.RFC3339),
		Description: *description,
	}

	transfer := builder.NewConfigTransfer("")
	bundle, err := transfer.CreateConfigBundle(config, packages, metadata)
	if err != nil {
		log.Fatalf("Failed to create config bundle: %v", err)
	}

	return bundle
}

// exportBundle exports the bundle to a file.
func exportBundle(bundle *builder.ConfigBundle) {
	transfer := builder.NewConfigTransfer("")
	if err := transfer.ExportBundle(bundle, *outputBundle); err != nil {
		log.Fatalf("Failed to export bundle: %v", err)
	}
	fmt.Printf("Configuration bundle saved to: %s\n", *outputBundle)
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
func submitBuildRequest(bundle *builder.ConfigBundle) {
	client := builder.NewBuilderClient(*serverURL)

	for _, pkg := range bundle.Packages.Packages {
		req := &builder.LocalBuildRequest{
			PackageName:  pkg.Atom,
			Version:      pkg.Version,
			ConfigBundle: bundle,
		}

		jobID, err := client.SubmitBuild(req)
		if err != nil {
			log.Fatalf("Failed to submit build: %v", err)
		}

		fmt.Printf("Build submitted for %s (job ID: %s)\n", pkg.Atom, jobID)
	}
}
