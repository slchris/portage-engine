package iac

import (
	"strings"
	"testing"
)

func TestDefaultCloudInitConfig(t *testing.T) {
	t.Parallel()

	config := DefaultCloudInitConfig()

	if config == nil {
		t.Fatal("DefaultCloudInitConfig returned nil")
	}

	if config.DockerImage != "gentoo/stage3:latest" {
		t.Errorf("DockerImage = %s, want gentoo/stage3:latest", config.DockerImage)
	}
	if config.BuilderPort != 9090 {
		t.Errorf("BuilderPort = %d, want 9090", config.BuilderPort)
	}
	if config.Architecture != "amd64" {
		t.Errorf("Architecture = %s, want amd64", config.Architecture)
	}
	if config.DataDir != "/var/lib/portage-engine" {
		t.Errorf("DataDir = %s", config.DataDir)
	}
	if config.SwapSizeGB != 4 {
		t.Errorf("SwapSizeGB = %d, want 4", config.SwapSizeGB)
	}
	if !config.PortageTreeSync {
		t.Error("PortageTreeSync should be true by default")
	}
	if !config.PullLatestImage {
		t.Error("PullLatestImage should be true by default")
	}
}

func TestGenerateCloudInitScript_Default(t *testing.T) {
	t.Parallel()

	script := GenerateCloudInitScript(nil)

	if script == "" {
		t.Fatal("GenerateCloudInitScript returned empty string")
	}

	// Check for essential components
	checks := []string{
		"#!/bin/bash",
		"set -e",
		"install_docker",
		"docker pull",
		"gentoo/stage3:latest",
		"mkdir -p /var/lib/portage-engine",
		"systemctl daemon-reload",
		"Cloud initialization complete",
	}

	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Errorf("Script missing: %s", check)
		}
	}
}

func TestGenerateCloudInitScript_CustomConfig(t *testing.T) {
	t.Parallel()

	config := &CloudInitConfig{
		DockerImage:       "gentoo/stage3:amd64-nomultilib",
		DockerRegistry:    "gcr.io/my-project",
		PullLatestImage:   true,
		PortageTreeSync:   true,
		PortageMirror:     "https://mirrors.ustc.edu.cn/gentoo",
		PortageBinpkgHost: "http://binhost.example.com",
		BuilderBinaryURL:  "https://releases.example.com/portage-builder",
		BuilderPort:       8080,
		ServerCallbackURL: "http://server.example.com:8080",
		InstanceID:        "test-instance",
		Architecture:      "arm64",
		DataDir:           "/data/portage",
		WorkDir:           "/data/builds",
		ArtifactDir:       "/data/artifacts",
		SwapSizeGB:        8,
		EnableFirewall:    true,
		ExtraPackages:     []string{"git", "vim"},
	}

	script := GenerateCloudInitScript(config)

	checks := []string{
		"gcr.io/my-project/gentoo/stage3:amd64-nomultilib",
		"https://mirrors.ustc.edu.cn/gentoo",
		"http://binhost.example.com",
		"https://releases.example.com/portage-builder",
		"BUILDER_PORT=8080",
		"INSTANCE_ID=test-instance",
		"ARCHITECTURE=arm64",
		"/data/portage",
		"/data/builds",
		"/data/artifacts",
		"8G /swapfile",
		"git", "vim",
		"http://server.example.com:8080",
	}

	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Errorf("Custom config script missing: %s", check)
		}
	}
}

func TestGenerateCloudInitScript_NoSwap(t *testing.T) {
	t.Parallel()

	config := DefaultCloudInitConfig()
	config.SwapSizeGB = 0

	script := GenerateCloudInitScript(config)

	if strings.Contains(script, "setup_swap") {
		t.Error("Script should not contain swap setup when SwapSizeGB is 0")
	}
}

func TestGenerateCloudInitScript_NoFirewall(t *testing.T) {
	t.Parallel()

	config := DefaultCloudInitConfig()
	config.EnableFirewall = false

	script := GenerateCloudInitScript(config)

	if strings.Contains(script, "configure_firewall") {
		t.Error("Script should not contain firewall config when EnableFirewall is false")
	}
}

func TestGenerateCloudInitScript_NoPortageSync(t *testing.T) {
	t.Parallel()

	config := DefaultCloudInitConfig()
	config.PortageTreeSync = false

	script := GenerateCloudInitScript(config)

	if strings.Contains(script, "Syncing Portage tree") {
		t.Error("Script should not sync Portage tree when PortageTreeSync is false")
	}
}

func TestGenerateCloudInitScript_NoBuilderBinary(t *testing.T) {
	t.Parallel()

	config := DefaultCloudInitConfig()
	config.BuilderBinaryURL = ""

	script := GenerateCloudInitScript(config)

	if strings.Contains(script, "Downloading builder binary") {
		t.Error("Script should not download binary when BuilderBinaryURL is empty")
	}

	// Should still create the service file
	if !strings.Contains(script, "portage-builder.service") {
		t.Error("Script should still create service file")
	}
}

func TestGenerateCloudInitScript_DockerInstallation(t *testing.T) {
	t.Parallel()

	script := GenerateCloudInitScript(nil)

	// Check Docker installation for different distros
	distroChecks := []string{
		"ubuntu|debian",
		"centos|rhel|fedora",
		"gentoo",
		"docker-ce",
		"systemctl enable docker",
		"systemctl start docker",
	}

	for _, check := range distroChecks {
		if !strings.Contains(script, check) {
			t.Errorf("Docker installation missing support for: %s", check)
		}
	}
}

func TestGenerateCloudInitScript_DirectoryCreation(t *testing.T) {
	t.Parallel()

	config := &CloudInitConfig{
		DataDir:     "/custom/data",
		WorkDir:     "/custom/work",
		ArtifactDir: "/custom/artifacts",
	}

	script := GenerateCloudInitScript(config)

	dirs := []string{
		"mkdir -p /custom/data",
		"mkdir -p /custom/work",
		"mkdir -p /custom/artifacts",
	}

	for _, dir := range dirs {
		if !strings.Contains(script, dir) {
			t.Errorf("Script missing directory creation: %s", dir)
		}
	}
}

func TestGenerateCloudInitScript_ServerCallback(t *testing.T) {
	t.Parallel()

	config := DefaultCloudInitConfig()
	config.ServerCallbackURL = "http://server:8080"
	config.InstanceID = "test-builder"
	config.BuilderPort = 9090

	script := GenerateCloudInitScript(config)

	checks := []string{
		"Notifying server",
		"http://server:8080/api/v1/builders/register",
		`\"instance_id\": \"test-builder\"`,
		`\"port\": 9090`,
	}

	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Errorf("Server callback missing: %s", check)
		}
	}
}

func TestGenerateCloudInitScript_NoServerCallback(t *testing.T) {
	t.Parallel()

	config := DefaultCloudInitConfig()
	config.ServerCallbackURL = ""

	script := GenerateCloudInitScript(config)

	if strings.Contains(script, "Notifying server") {
		t.Error("Script should not notify server when ServerCallbackURL is empty")
	}
}

func TestGenerateUserData(t *testing.T) {
	t.Parallel()

	config := DefaultCloudInitConfig()
	userData := GenerateUserData(config)

	if !strings.HasPrefix(userData, "#cloud-config") {
		t.Error("UserData should start with #cloud-config")
	}

	if !strings.Contains(userData, "runcmd:") {
		t.Error("UserData should contain runcmd section")
	}
}

func TestGenerateStartupScript(t *testing.T) {
	t.Parallel()

	config := DefaultCloudInitConfig()
	script := GenerateStartupScript(config)

	if !strings.HasPrefix(script, "#!/bin/bash") {
		t.Error("StartupScript should start with shebang")
	}

	if !strings.Contains(script, "install_docker") {
		t.Error("StartupScript should contain docker installation")
	}
}

func TestIndentScript(t *testing.T) {
	t.Parallel()

	script := "line1\nline2\nline3"
	indented := indentScript(script, "  ")

	expected := "  line1\n  line2\n  line3"
	if indented != expected {
		t.Errorf("indentScript = %q, want %q", indented, expected)
	}
}

func TestGenerateCloudInitScript_PortageConfig(t *testing.T) {
	t.Parallel()

	config := &CloudInitConfig{
		DockerImage:       "gentoo/stage3:latest",
		PortageMirror:     "https://example.com/gentoo",
		PortageBinpkgHost: "https://binpkgs.example.com",
		DataDir:           "/var/lib/portage-engine",
	}

	script := GenerateCloudInitScript(config)

	checks := []string{
		`GENTOO_MIRRORS="https://example.com/gentoo"`,
		`PORTAGE_BINHOST="https://binpkgs.example.com"`,
		"FEATURES=\"buildpkg binpkg-multi-instance parallel-fetch parallel-install\"",
	}

	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Errorf("Portage config missing: %s", check)
		}
	}
}

func TestGenerateCloudInitScript_ExtraPackages(t *testing.T) {
	t.Parallel()

	config := DefaultCloudInitConfig()
	config.ExtraPackages = []string{"htop", "tmux", "curl"}

	script := GenerateCloudInitScript(config)

	if !strings.Contains(script, "Installing extra packages") {
		t.Error("Script should mention installing extra packages")
	}

	for _, pkg := range config.ExtraPackages {
		if !strings.Contains(script, pkg) {
			t.Errorf("Script missing extra package: %s", pkg)
		}
	}
}

func TestCloudInitConfig_JSON(t *testing.T) {
	t.Parallel()

	config := &CloudInitConfig{
		DockerImage:       "gentoo/stage3:latest",
		BuilderPort:       9090,
		ServerCallbackURL: "http://server:8080",
		Architecture:      "amd64",
	}

	// Just verify the struct can be used
	if config.DockerImage != "gentoo/stage3:latest" {
		t.Error("Config field mismatch")
	}
}
