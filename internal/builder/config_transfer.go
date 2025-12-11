// Package builder provides configuration transfer capabilities.
package builder

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// PortageConfig represents user's Portage configuration.
type PortageConfig struct {
	// Package.use entries
	PackageUse map[string][]string `json:"package_use"`
	// Package.accept_keywords entries
	PackageKeywords map[string][]string `json:"package_keywords"`
	// Package.mask entries
	PackageMask []string `json:"package_mask"`
	// Package.unmask entries
	PackageUnmask []string `json:"package_unmask"`
	// Make.conf settings
	MakeConf map[string]string `json:"make_conf"`
	// Environment variables
	Environment map[string]string `json:"environment"`
	// Global USE flags
	GlobalUse []string `json:"global_use"`
	// Repository configurations
	Repos []RepoConfig `json:"repos"`
}

// RepoConfig represents a repository configuration.
type RepoConfig struct {
	Name     string `json:"name"`
	Location string `json:"location"`
	SyncType string `json:"sync_type"`
	SyncURI  string `json:"sync_uri"`
	Priority int    `json:"priority"`
}

// BuildPackageSpec specifies packages to build with their configurations.
type BuildPackageSpec struct {
	Packages []PackageSpec `json:"packages"`
}

// PackageSpec represents a single package build specification.
type PackageSpec struct {
	Atom        string            `json:"atom"` // e.g., "dev-lang/python:3.11"
	Version     string            `json:"version,omitempty"`
	UseFlags    []string          `json:"use_flags,omitempty"`
	Keywords    []string          `json:"keywords,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

// ConfigBundle bundles Portage configuration and package specifications.
type ConfigBundle struct {
	Config   *PortageConfig    `json:"config"`
	Packages *BuildPackageSpec `json:"packages"`
	Metadata BundleMetadata    `json:"metadata"`
}

// BundleMetadata contains metadata about the configuration bundle.
type BundleMetadata struct {
	UserID      string `json:"user_id"`
	TargetArch  string `json:"target_arch"`
	Profile     string `json:"profile"`
	CreatedAt   string `json:"created_at"`
	Description string `json:"description,omitempty"`
}

// ConfigTransfer handles configuration transfer operations.
type ConfigTransfer struct {
	workDir string
}

// NewConfigTransfer creates a new configuration transfer handler.
func NewConfigTransfer(workDir string) *ConfigTransfer {
	return &ConfigTransfer{
		workDir: workDir,
	}
}

// CreateConfigBundle creates a configuration bundle from user's Portage setup.
func (ct *ConfigTransfer) CreateConfigBundle(
	config *PortageConfig,
	packages *BuildPackageSpec,
	metadata BundleMetadata,
) (*ConfigBundle, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if packages == nil {
		return nil, fmt.Errorf("packages cannot be nil")
	}

	bundle := &ConfigBundle{
		Config:   config,
		Packages: packages,
		Metadata: metadata,
	}

	return bundle, nil
}

// ExportBundle exports the configuration bundle to a tarball.
func (ct *ConfigTransfer) ExportBundle(bundle *ConfigBundle, outputPath string) error {
	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() {
		_ = outFile.Close()
	}()

	// Create gzip writer
	gzWriter := gzip.NewWriter(outFile)
	defer func() {
		_ = gzWriter.Close()
	}()

	// Create tar writer
	tarWriter := tar.NewWriter(gzWriter)
	defer func() {
		_ = tarWriter.Close()
	}()

	// Add bundle metadata
	metadataJSON, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal bundle: %w", err)
	}

	if err := ct.addFileToTar(tarWriter, "bundle.json", metadataJSON); err != nil {
		return fmt.Errorf("failed to add bundle.json: %w", err)
	}

	// Generate and add Portage configuration files
	if err := ct.addPortageConfigToTar(tarWriter, bundle.Config); err != nil {
		return fmt.Errorf("failed to add portage config: %w", err)
	}

	// Add package build specs
	packagesJSON, err := json.MarshalIndent(bundle.Packages, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal packages: %w", err)
	}

	if err := ct.addFileToTar(tarWriter, "packages.json", packagesJSON); err != nil {
		return fmt.Errorf("failed to add packages.json: %w", err)
	}

	return nil
}

// addFileToTar adds a file to the tar archive.
func (ct *ConfigTransfer) addFileToTar(tw *tar.Writer, name string, data []byte) error {
	header := &tar.Header{
		Name: name,
		Mode: 0644,
		Size: int64(len(data)),
	}

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

// addPortageConfigToTar adds Portage configuration files to the tar archive.
func (ct *ConfigTransfer) addPortageConfigToTar(tw *tar.Writer, config *PortageConfig) error {
	// Generate package.use
	if len(config.PackageUse) > 0 {
		var lines []string
		for pkg, flags := range config.PackageUse {
			lines = append(lines, fmt.Sprintf("%s %s", pkg, strings.Join(flags, " ")))
		}
		content := strings.Join(lines, "\n") + "\n"
		if err := ct.addFileToTar(tw, "etc/portage/package.use/00-user", []byte(content)); err != nil {
			return err
		}
	}

	// Generate package.accept_keywords
	if len(config.PackageKeywords) > 0 {
		var lines []string
		for pkg, keywords := range config.PackageKeywords {
			lines = append(lines, fmt.Sprintf("%s %s", pkg, strings.Join(keywords, " ")))
		}
		content := strings.Join(lines, "\n") + "\n"
		if err := ct.addFileToTar(tw, "etc/portage/package.accept_keywords/00-user", []byte(content)); err != nil {
			return err
		}
	}

	// Generate package.mask
	if len(config.PackageMask) > 0 {
		content := strings.Join(config.PackageMask, "\n") + "\n"
		if err := ct.addFileToTar(tw, "etc/portage/package.mask/00-user", []byte(content)); err != nil {
			return err
		}
	}

	// Generate package.unmask
	if len(config.PackageUnmask) > 0 {
		content := strings.Join(config.PackageUnmask, "\n") + "\n"
		if err := ct.addFileToTar(tw, "etc/portage/package.unmask/00-user", []byte(content)); err != nil {
			return err
		}
	}

	// Generate make.conf entries
	if len(config.MakeConf) > 0 {
		var lines []string
		for key, value := range config.MakeConf {
			lines = append(lines, fmt.Sprintf("%s=\"%s\"", key, value))
		}
		content := strings.Join(lines, "\n") + "\n"
		if err := ct.addFileToTar(tw, "etc/portage/make.conf.d/00-user", []byte(content)); err != nil {
			return err
		}
	}

	// Generate repos.conf entries
	if len(config.Repos) > 0 {
		for _, repo := range config.Repos {
			var lines []string
			lines = append(lines, fmt.Sprintf("[%s]", repo.Name))
			if repo.Location != "" {
				lines = append(lines, fmt.Sprintf("location = %s", repo.Location))
			}
			if repo.SyncType != "" {
				lines = append(lines, fmt.Sprintf("sync-type = %s", repo.SyncType))
			}
			if repo.SyncURI != "" {
				lines = append(lines, fmt.Sprintf("sync-uri = %s", repo.SyncURI))
			}
			if repo.Priority != 0 {
				lines = append(lines, fmt.Sprintf("priority = %d", repo.Priority))
			}
			content := strings.Join(lines, "\n") + "\n"
			filename := fmt.Sprintf("etc/portage/repos.conf/%s.conf", repo.Name)
			if err := ct.addFileToTar(tw, filename, []byte(content)); err != nil {
				return err
			}
		}
	}

	return nil
}

// ImportBundle imports a configuration bundle from a tarball.
func (ct *ConfigTransfer) ImportBundle(bundlePath string) (*ConfigBundle, error) {
	// Open the tarball
	file, err := os.Open(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open bundle: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	// Create gzip reader
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() {
		_ = gzReader.Close()
	}()

	// Create tar reader
	tarReader := tar.NewReader(gzReader)

	// Read bundle.json
	var bundle *ConfigBundle
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar header: %w", err)
		}

		if header.Name == "bundle.json" {
			data, err := io.ReadAll(tarReader)
			if err != nil {
				return nil, fmt.Errorf("failed to read bundle.json: %w", err)
			}

			bundle = &ConfigBundle{}
			if err := json.Unmarshal(data, bundle); err != nil {
				return nil, fmt.Errorf("failed to unmarshal bundle.json: %w", err)
			}
			break
		}
	}

	if bundle == nil {
		return nil, fmt.Errorf("bundle.json not found in tarball")
	}

	return bundle, nil
}

// ApplyConfigToSystem applies the configuration bundle to a target system.
func (ct *ConfigTransfer) ApplyConfigToSystem(bundle *ConfigBundle, targetRoot string) error {
	if targetRoot == "" {
		targetRoot = "/"
	}

	// Create necessary directories
	portageDir := filepath.Join(targetRoot, "etc", "portage")
	dirs := []string{
		filepath.Join(portageDir, "package.use"),
		filepath.Join(portageDir, "package.accept_keywords"),
		filepath.Join(portageDir, "package.mask"),
		filepath.Join(portageDir, "package.unmask"),
		filepath.Join(portageDir, "make.conf.d"),
		filepath.Join(portageDir, "repos.conf"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	config := bundle.Config

	// Write package.use
	if len(config.PackageUse) > 0 {
		var lines []string
		for pkg, flags := range config.PackageUse {
			lines = append(lines, fmt.Sprintf("%s %s", pkg, strings.Join(flags, " ")))
		}
		content := strings.Join(lines, "\n") + "\n"
		path := filepath.Join(portageDir, "package.use", "00-user")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write package.use: %w", err)
		}
	}

	// Write package.accept_keywords
	if len(config.PackageKeywords) > 0 {
		var lines []string
		for pkg, keywords := range config.PackageKeywords {
			lines = append(lines, fmt.Sprintf("%s %s", pkg, strings.Join(keywords, " ")))
		}
		content := strings.Join(lines, "\n") + "\n"
		path := filepath.Join(portageDir, "package.accept_keywords", "00-user")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write package.accept_keywords: %w", err)
		}
	}

	// Write package.mask
	if len(config.PackageMask) > 0 {
		content := strings.Join(config.PackageMask, "\n") + "\n"
		path := filepath.Join(portageDir, "package.mask", "00-user")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write package.mask: %w", err)
		}
	}

	// Write package.unmask
	if len(config.PackageUnmask) > 0 {
		content := strings.Join(config.PackageUnmask, "\n") + "\n"
		path := filepath.Join(portageDir, "package.unmask", "00-user")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write package.unmask: %w", err)
		}
	}

	// Write make.conf
	if len(config.MakeConf) > 0 {
		var lines []string
		for key, value := range config.MakeConf {
			lines = append(lines, fmt.Sprintf("%s=\"%s\"", key, value))
		}
		content := strings.Join(lines, "\n") + "\n"
		path := filepath.Join(portageDir, "make.conf.d", "00-user")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write make.conf: %w", err)
		}
	}

	// Write repos.conf entries
	if len(config.Repos) > 0 {
		for _, repo := range config.Repos {
			var lines []string
			lines = append(lines, fmt.Sprintf("[%s]", repo.Name))
			if repo.Location != "" {
				lines = append(lines, fmt.Sprintf("location = %s", repo.Location))
			}
			if repo.SyncType != "" {
				lines = append(lines, fmt.Sprintf("sync-type = %s", repo.SyncType))
			}
			if repo.SyncURI != "" {
				lines = append(lines, fmt.Sprintf("sync-uri = %s", repo.SyncURI))
			}
			if repo.Priority != 0 {
				lines = append(lines, fmt.Sprintf("priority = %d", repo.Priority))
			}
			content := strings.Join(lines, "\n") + "\n"
			path := filepath.Join(portageDir, "repos.conf", fmt.Sprintf("%s.conf", repo.Name))
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return fmt.Errorf("failed to write repos.conf for %s: %w", repo.Name, err)
			}
		}
	}

	return nil
}
