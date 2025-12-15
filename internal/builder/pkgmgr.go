// Package builder provides package building functionality.
package builder

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/slchris/portage-engine/pkg/config"
)

// PackageManager defines the interface for different package managers.
type PackageManager interface {
	// Name returns the package manager name.
	Name() string

	// InstallCommand returns the command to install a package.
	InstallCommand(pkg string, options []string) []string

	// BuildCommand returns the command to build a package from source.
	BuildCommand(pkg string, options []string) []string

	// SearchCommand returns the command to search for a package.
	SearchCommand(pkg string) []string

	// UpdateCommand returns the command to update package database.
	UpdateCommand() []string

	// GetDockerMounts returns the Docker volume mounts for this package manager.
	GetDockerMounts(cfg *config.BuilderConfig) []DockerMount

	// GetEnvVars returns environment variables for the build process.
	GetEnvVars(cfg *config.BuilderConfig) map[string]string

	// GetArtifactPaths returns paths where build artifacts are located.
	GetArtifactPaths() []string

	// ArtifactExtension returns the file extension for binary packages.
	ArtifactExtension() string
}

// DockerMount represents a Docker volume mount.
type DockerMount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// String returns the Docker mount string format.
func (m DockerMount) String() string {
	mount := fmt.Sprintf("%s:%s", m.Source, m.Target)
	if m.ReadOnly {
		mount += ":ro"
	}
	return mount
}

// GentooPackageManager implements PackageManager for Gentoo Linux.
type GentooPackageManager struct {
	cfg *config.BuilderConfig
}

// NewGentooPackageManager creates a new Gentoo package manager.
func NewGentooPackageManager(cfg *config.BuilderConfig) *GentooPackageManager {
	return &GentooPackageManager{cfg: cfg}
}

// Name returns the package manager name.
func (g *GentooPackageManager) Name() string {
	return "portage"
}

// InstallCommand returns the emerge install command.
func (g *GentooPackageManager) InstallCommand(pkg string, options []string) []string {
	cmd := []string{"emerge", "--ask=n", "--verbose"}
	cmd = append(cmd, options...)
	cmd = append(cmd, pkg)
	return cmd
}

// BuildCommand returns the emerge build command with binary package output.
func (g *GentooPackageManager) BuildCommand(pkg string, options []string) []string {
	cmd := []string{"emerge", "--ask=n", "--verbose", "--buildpkg", "--usepkg"}
	cmd = append(cmd, options...)
	cmd = append(cmd, pkg)
	return cmd
}

// SearchCommand returns the emerge search command.
func (g *GentooPackageManager) SearchCommand(pkg string) []string {
	return []string{"emerge", "--search", pkg}
}

// UpdateCommand returns the eix-sync or emerge --sync command.
func (g *GentooPackageManager) UpdateCommand() []string {
	return []string{"emerge", "--sync"}
}

// GetDockerMounts returns Gentoo-specific Docker mounts.
func (g *GentooPackageManager) GetDockerMounts(cfg *config.BuilderConfig) []DockerMount {
	mounts := []DockerMount{
		{
			Source:   cfg.PortageReposPath,
			Target:   "/var/db/repos",
			ReadOnly: true,
		},
		{
			Source:   cfg.PortageConfPath,
			Target:   "/etc/portage",
			ReadOnly: true,
		},
	}

	// Add make.conf mount if different from PortageConfPath
	if cfg.MakeConfPath != "" && !strings.HasPrefix(cfg.MakeConfPath, cfg.PortageConfPath) {
		mounts = append(mounts, DockerMount{
			Source:   cfg.MakeConfPath,
			Target:   "/etc/portage/make.conf",
			ReadOnly: true,
		})
	}

	return mounts
}

// GetEnvVars returns Gentoo-specific environment variables.
func (g *GentooPackageManager) GetEnvVars(cfg *config.BuilderConfig) map[string]string {
	envVars := make(map[string]string)

	// Add distfiles mirror if configured
	if cfg.DistfilesMirror != "" {
		envVars["GENTOO_MIRRORS"] = cfg.DistfilesMirror
	}

	// Add sync mirror if configured
	if cfg.SyncMirror != "" {
		envVars["PORTAGE_RSYNC_EXTRA_OPTS"] = ""
		// Note: SYNC variable is deprecated, use repos.conf instead
		// This is mainly for informational purposes
		envVars["PORTAGE_SYNC_URI"] = cfg.SyncMirror
	}

	return envVars
}

// GetArtifactPaths returns paths where Gentoo binary packages are stored.
func (g *GentooPackageManager) GetArtifactPaths() []string {
	return []string{"/var/cache/binpkgs"}
}

// ArtifactExtension returns the Gentoo binary package extension.
func (g *GentooPackageManager) ArtifactExtension() string {
	return ".gpkg.tar"
}

// DebianPackageManager implements PackageManager for Debian-based systems.
type DebianPackageManager struct {
	cfg *config.BuilderConfig
}

// NewDebianPackageManager creates a new Debian package manager.
func NewDebianPackageManager(cfg *config.BuilderConfig) *DebianPackageManager {
	return &DebianPackageManager{cfg: cfg}
}

// Name returns the package manager name.
func (d *DebianPackageManager) Name() string {
	return "apt"
}

// InstallCommand returns the apt install command.
func (d *DebianPackageManager) InstallCommand(pkg string, options []string) []string {
	cmd := []string{"apt-get", "install", "-y"}
	cmd = append(cmd, options...)
	cmd = append(cmd, pkg)
	return cmd
}

// BuildCommand returns the command to build a Debian package from source.
func (d *DebianPackageManager) BuildCommand(pkg string, _ []string) []string {
	// For Debian, building from source typically involves:
	// 1. apt-get source pkg
	// 2. cd pkg-version && dpkg-buildpackage
	// We simplify this to apt-get build-dep + build from source
	cmd := []string{"sh", "-c", fmt.Sprintf(
		"apt-get source %s && cd %s-* && dpkg-buildpackage -us -uc -b",
		pkg, pkg,
	)}
	return cmd
}

// SearchCommand returns the apt search command.
func (d *DebianPackageManager) SearchCommand(pkg string) []string {
	return []string{"apt-cache", "search", pkg}
}

// UpdateCommand returns the apt update command.
func (d *DebianPackageManager) UpdateCommand() []string {
	return []string{"apt-get", "update"}
}

// GetDockerMounts returns Debian-specific Docker mounts.
func (d *DebianPackageManager) GetDockerMounts(cfg *config.BuilderConfig) []DockerMount {
	mounts := []DockerMount{}

	// Mount apt sources if configured
	if cfg.AptSourcesPath != "" {
		mounts = append(mounts, DockerMount{
			Source:   filepath.Join(cfg.AptSourcesPath, "sources.list"),
			Target:   "/etc/apt/sources.list",
			ReadOnly: true,
		})
		mounts = append(mounts, DockerMount{
			Source:   filepath.Join(cfg.AptSourcesPath, "sources.list.d"),
			Target:   "/etc/apt/sources.list.d",
			ReadOnly: true,
		})
	}

	// Mount apt cache if configured (for reusing downloaded packages)
	if cfg.AptCachePath != "" {
		mounts = append(mounts, DockerMount{
			Source:   cfg.AptCachePath,
			Target:   "/var/cache/apt",
			ReadOnly: false, // Need write access for cache
		})
	}

	return mounts
}

// GetEnvVars returns Debian-specific environment variables.
func (d *DebianPackageManager) GetEnvVars(cfg *config.BuilderConfig) map[string]string {
	envVars := map[string]string{
		"DEBIAN_FRONTEND": "noninteractive",
	}

	// Add mirror URL if configured
	if cfg.DistfilesMirror != "" {
		envVars["APT_MIRROR"] = cfg.DistfilesMirror
	}

	return envVars
}

// GetArtifactPaths returns paths where Debian packages are stored after build.
func (d *DebianPackageManager) GetArtifactPaths() []string {
	return []string{"/var/cache/apt/archives", "/tmp/build"}
}

// ArtifactExtension returns the Debian package extension.
func (d *DebianPackageManager) ArtifactExtension() string {
	return ".deb"
}

// NewPackageManager creates a PackageManager based on the OS type.
func NewPackageManager(cfg *config.BuilderConfig) PackageManager {
	switch cfg.HostOSType {
	case config.OSTypeDebian:
		return NewDebianPackageManager(cfg)
	case config.OSTypeGentoo:
		fallthrough
	default:
		return NewGentooPackageManager(cfg)
	}
}
