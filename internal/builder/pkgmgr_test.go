// Package builder provides package building functionality.
package builder

import (
	"strings"
	"testing"

	"github.com/slchris/portage-engine/pkg/config"
)

func TestNewPackageManager(t *testing.T) {
	tests := []struct {
		name     string
		osType   config.OSType
		wantName string
	}{
		{
			name:     "gentoo package manager",
			osType:   config.OSTypeGentoo,
			wantName: "portage",
		},
		{
			name:     "debian package manager",
			osType:   config.OSTypeDebian,
			wantName: "apt",
		},
		{
			name:     "default to gentoo",
			osType:   "",
			wantName: "portage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.BuilderConfig{
				HostOSType: tt.osType,
			}
			pm := NewPackageManager(cfg)
			if pm.Name() != tt.wantName {
				t.Errorf("NewPackageManager() name = %v, want %v", pm.Name(), tt.wantName)
			}
		})
	}
}

func TestGentooPackageManager_Commands(t *testing.T) {
	cfg := &config.BuilderConfig{
		HostOSType:       config.OSTypeGentoo,
		PortageReposPath: "/var/db/repos",
		PortageConfPath:  "/etc/portage",
	}
	pm := NewGentooPackageManager(cfg)

	t.Run("install command", func(t *testing.T) {
		cmd := pm.InstallCommand("app-misc/hello", nil)
		if len(cmd) < 3 {
			t.Error("install command should have at least 3 elements")
		}
		if cmd[0] != "emerge" {
			t.Errorf("first element should be 'emerge', got %v", cmd[0])
		}
		if cmd[len(cmd)-1] != "app-misc/hello" {
			t.Errorf("last element should be package name, got %v", cmd[len(cmd)-1])
		}
	})

	t.Run("build command", func(t *testing.T) {
		cmd := pm.BuildCommand("app-misc/hello", nil)
		if cmd[0] != "emerge" {
			t.Errorf("first element should be 'emerge', got %v", cmd[0])
		}
		hasBuildPkg := false
		for _, arg := range cmd {
			if arg == "--buildpkg" {
				hasBuildPkg = true
				break
			}
		}
		if !hasBuildPkg {
			t.Error("build command should contain --buildpkg flag")
		}
	})

	t.Run("search command", func(t *testing.T) {
		cmd := pm.SearchCommand("hello")
		if cmd[0] != "emerge" {
			t.Errorf("first element should be 'emerge', got %v", cmd[0])
		}
		if cmd[1] != "--search" {
			t.Errorf("second element should be '--search', got %v", cmd[1])
		}
	})

	t.Run("update command", func(t *testing.T) {
		cmd := pm.UpdateCommand()
		if cmd[0] != "emerge" {
			t.Errorf("first element should be 'emerge', got %v", cmd[0])
		}
	})

	t.Run("artifact extension", func(t *testing.T) {
		ext := pm.ArtifactExtension()
		if ext != ".gpkg.tar" {
			t.Errorf("artifact extension should be '.gpkg.tar', got %v", ext)
		}
	})
}

func TestGentooPackageManager_DockerMounts(t *testing.T) {
	cfg := &config.BuilderConfig{
		HostOSType:       config.OSTypeGentoo,
		PortageReposPath: "/var/db/repos",
		PortageConfPath:  "/etc/portage",
		MakeConfPath:     "/etc/portage/make.conf",
	}
	pm := NewGentooPackageManager(cfg)

	mounts := pm.GetDockerMounts(cfg)

	if len(mounts) < 2 {
		t.Errorf("expected at least 2 mounts, got %d", len(mounts))
	}

	// Check repos mount
	foundRepos := false
	for _, m := range mounts {
		if m.Target == "/var/db/repos" {
			foundRepos = true
			if !m.ReadOnly {
				t.Error("repos mount should be read-only")
			}
			break
		}
	}
	if !foundRepos {
		t.Error("expected /var/db/repos mount")
	}

	// Check portage mount
	foundPortage := false
	for _, m := range mounts {
		if m.Target == "/etc/portage" {
			foundPortage = true
			break
		}
	}
	if !foundPortage {
		t.Error("expected /etc/portage mount")
	}
}

func TestGentooPackageManager_EnvVars(t *testing.T) {
	t.Run("with mirrors configured", func(t *testing.T) {
		cfg := &config.BuilderConfig{
			HostOSType:      config.OSTypeGentoo,
			DistfilesMirror: "https://mirrors.example.com/gentoo",
			SyncMirror:      "rsync://rsync.example.com/gentoo-portage",
		}
		pm := NewGentooPackageManager(cfg)

		envVars := pm.GetEnvVars(cfg)

		if envVars["GENTOO_MIRRORS"] != "https://mirrors.example.com/gentoo" {
			t.Errorf("GENTOO_MIRRORS not set correctly, got %v", envVars["GENTOO_MIRRORS"])
		}
		if envVars["PORTAGE_SYNC_URI"] != "rsync://rsync.example.com/gentoo-portage" {
			t.Errorf("PORTAGE_SYNC_URI not set correctly, got %v", envVars["PORTAGE_SYNC_URI"])
		}
	})

	t.Run("without mirrors configured", func(t *testing.T) {
		cfg := &config.BuilderConfig{
			HostOSType: config.OSTypeGentoo,
		}
		pm := NewGentooPackageManager(cfg)

		envVars := pm.GetEnvVars(cfg)

		if _, ok := envVars["GENTOO_MIRRORS"]; ok {
			t.Error("GENTOO_MIRRORS should not be set when not configured")
		}
	})
}

func TestDebianPackageManager_Commands(t *testing.T) {
	cfg := &config.BuilderConfig{
		HostOSType:     config.OSTypeDebian,
		DebianCodename: "bookworm",
	}
	pm := NewDebianPackageManager(cfg)

	t.Run("install command", func(t *testing.T) {
		cmd := pm.InstallCommand("nginx", nil)
		if cmd[0] != "apt-get" {
			t.Errorf("first element should be 'apt-get', got %v", cmd[0])
		}
		if cmd[1] != "install" {
			t.Errorf("second element should be 'install', got %v", cmd[1])
		}
		if cmd[len(cmd)-1] != "nginx" {
			t.Errorf("last element should be package name, got %v", cmd[len(cmd)-1])
		}
	})

	t.Run("build command", func(t *testing.T) {
		cmd := pm.BuildCommand("nginx", nil)
		if cmd[0] != "sh" {
			t.Errorf("first element should be 'sh', got %v", cmd[0])
		}
		if cmd[1] != "-c" {
			t.Errorf("second element should be '-c', got %v", cmd[1])
		}
	})

	t.Run("search command", func(t *testing.T) {
		cmd := pm.SearchCommand("nginx")
		if cmd[0] != "apt-cache" {
			t.Errorf("first element should be 'apt-cache', got %v", cmd[0])
		}
	})

	t.Run("update command", func(t *testing.T) {
		cmd := pm.UpdateCommand()
		if cmd[0] != "apt-get" {
			t.Errorf("first element should be 'apt-get', got %v", cmd[0])
		}
		if cmd[1] != "update" {
			t.Errorf("second element should be 'update', got %v", cmd[1])
		}
	})

	t.Run("artifact extension", func(t *testing.T) {
		ext := pm.ArtifactExtension()
		if ext != ".deb" {
			t.Errorf("artifact extension should be '.deb', got %v", ext)
		}
	})
}

func TestDebianPackageManager_DockerMounts(t *testing.T) {
	cfg := &config.BuilderConfig{
		HostOSType:     config.OSTypeDebian,
		AptSourcesPath: "/etc/apt",
		AptCachePath:   "/var/cache/apt",
	}
	pm := NewDebianPackageManager(cfg)

	mounts := pm.GetDockerMounts(cfg)

	// Should have sources.list, sources.list.d, and cache mounts
	if len(mounts) < 3 {
		t.Errorf("expected at least 3 mounts for Debian, got %d", len(mounts))
	}

	// Check apt cache mount is not read-only
	foundCache := false
	for _, m := range mounts {
		if m.Target == "/var/cache/apt" {
			foundCache = true
			if m.ReadOnly {
				t.Error("apt cache mount should not be read-only")
			}
			break
		}
	}
	if !foundCache {
		t.Error("expected /var/cache/apt mount")
	}
}

func TestDebianPackageManager_EnvVars(t *testing.T) {
	cfg := &config.BuilderConfig{
		HostOSType:      config.OSTypeDebian,
		DistfilesMirror: "https://mirrors.example.com/debian",
	}
	pm := NewDebianPackageManager(cfg)

	envVars := pm.GetEnvVars(cfg)

	if envVars["DEBIAN_FRONTEND"] != "noninteractive" {
		t.Error("DEBIAN_FRONTEND should be 'noninteractive'")
	}
	if envVars["APT_MIRROR"] != "https://mirrors.example.com/debian" {
		t.Errorf("APT_MIRROR not set correctly, got %v", envVars["APT_MIRROR"])
	}
}

func TestDockerMount_String(t *testing.T) {
	tests := []struct {
		name   string
		mount  DockerMount
		expect string
	}{
		{
			name: "read-write mount",
			mount: DockerMount{
				Source:   "/host/path",
				Target:   "/container/path",
				ReadOnly: false,
			},
			expect: "/host/path:/container/path",
		},
		{
			name: "read-only mount",
			mount: DockerMount{
				Source:   "/host/path",
				Target:   "/container/path",
				ReadOnly: true,
			},
			expect: "/host/path:/container/path:ro",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.mount.String()
			if result != tt.expect {
				t.Errorf("DockerMount.String() = %v, want %v", result, tt.expect)
			}
		})
	}
}

func TestGentooPackageManager_BuildCommandWithOptions(t *testing.T) {
	cfg := &config.BuilderConfig{
		HostOSType: config.OSTypeGentoo,
	}
	pm := NewGentooPackageManager(cfg)

	options := []string{"--oneshot", "--update"}
	cmd := pm.BuildCommand("app-misc/hello", options)

	cmdStr := strings.Join(cmd, " ")
	if !strings.Contains(cmdStr, "--oneshot") {
		t.Error("command should contain --oneshot option")
	}
	if !strings.Contains(cmdStr, "--update") {
		t.Error("command should contain --update option")
	}
	if !strings.Contains(cmdStr, "--buildpkg") {
		t.Error("command should contain --buildpkg flag")
	}
}

func TestDebianPackageManager_InstallCommandWithOptions(t *testing.T) {
	cfg := &config.BuilderConfig{
		HostOSType: config.OSTypeDebian,
	}
	pm := NewDebianPackageManager(cfg)

	options := []string{"--no-install-recommends"}
	cmd := pm.InstallCommand("nginx", options)

	cmdStr := strings.Join(cmd, " ")
	if !strings.Contains(cmdStr, "--no-install-recommends") {
		t.Error("command should contain --no-install-recommends option")
	}
}
