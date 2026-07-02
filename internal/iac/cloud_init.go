// Package iac manages infrastructure provisioning using Terraform.
package iac

import (
	"fmt"
	"strings"
)

// CloudInitConfig holds configuration for cloud instance initialization.
type CloudInitConfig struct {
	// Docker configuration
	DockerImage     string `json:"docker_image"`
	DockerRegistry  string `json:"docker_registry"`
	PullLatestImage bool   `json:"pull_latest_image"`

	// Portage configuration
	PortageTreeSync   bool   `json:"portage_tree_sync"`
	PortageMirror     string `json:"portage_mirror"`
	PortageBinpkgHost string `json:"portage_binpkg_host"`

	// Builder service configuration
	BuilderBinaryURL  string `json:"builder_binary_url"`
	BuilderPort       int    `json:"builder_port"`
	BuilderToken      string `json:"builder_token"`
	ServerCallbackURL string `json:"server_callback_url"`
	InstanceID        string `json:"instance_id"`
	Architecture      string `json:"architecture"`

	// Data directories
	DataDir     string `json:"data_dir"`
	WorkDir     string `json:"work_dir"`
	ArtifactDir string `json:"artifact_dir"`

	// System configuration
	SwapSizeGB     int  `json:"swap_size_gb"`
	EnableFirewall bool `json:"enable_firewall"`

	// Extra packages to install
	ExtraPackages []string `json:"extra_packages"`
}

// DefaultCloudInitConfig returns the default cloud initialization configuration.
func DefaultCloudInitConfig() *CloudInitConfig {
	return &CloudInitConfig{
		DockerImage:       "gentoo/stage3:latest",
		DockerRegistry:    "",
		PullLatestImage:   true,
		PortageTreeSync:   true,
		PortageMirror:     "https://distfiles.gentoo.org",
		PortageBinpkgHost: "",
		BuilderBinaryURL:  "",
		BuilderPort:       9090,
		ServerCallbackURL: "",
		InstanceID:        "",
		Architecture:      "amd64",
		DataDir:           "/var/lib/portage-engine",
		WorkDir:           "/var/tmp/portage-builds",
		ArtifactDir:       "/var/tmp/portage-artifacts",
		SwapSizeGB:        4,
		EnableFirewall:    true,
		ExtraPackages:     []string{},
	}
}

// shellSingleQuote wraps s in single quotes for safe use as a shell word,
// escaping any embedded single quotes.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// heredocEscape escapes $, backtick, and backslash so a value embedded in an
// UNQUOTED heredoc lands literally instead of being shell-expanded.
func heredocEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "`", "\\`", `$`, `\$`)
	return r.Replace(s)
}

// marchForArch returns a safe, portable -march baseline for the given arch.
// It never returns "native": a binhost farm must produce packages that run on
// any consumer of that arch, not just the exact CPU that happened to build them.
func marchForArch(arch string) string {
	switch arch {
	case "arm64", "aarch64":
		return "armv8-a"
	case "amd64", "x86_64", "":
		// x86-64-v2 (SSE4.2/POPCNT) is broadly compatible with modern x86-64.
		return "x86-64-v2"
	default:
		return "x86-64-v2"
	}
}

// GenerateCloudInitScript generates a comprehensive cloud-init script.
func GenerateCloudInitScript(config *CloudInitConfig) string {
	if config == nil {
		config = DefaultCloudInitConfig()
	}

	var sb strings.Builder

	// Script header
	sb.WriteString(`#!/bin/bash
set -e
set -o pipefail

# Portage Engine Cloud Init Script
# This script initializes a cloud instance as a Portage builder

export DEBIAN_FRONTEND=noninteractive
export LOG_FILE="/var/log/portage-engine-init.log"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

error_exit() {
    log "ERROR: $1"
    exit 1
}

log "Starting Portage Engine cloud initialization..."

`)

	// Detect OS and set package manager
	sb.WriteString(`# Detect OS
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS=$ID
        OS_VERSION=$VERSION_ID
    elif [ -f /etc/gentoo-release ]; then
        OS="gentoo"
    elif [ -f /etc/debian_version ]; then
        OS="debian"
    else
        OS="unknown"
    fi
    log "Detected OS: $OS"
}

detect_os

`)

	// Install Docker
	sb.WriteString(`# Install Docker
install_docker() {
    log "Installing Docker..."

    case $OS in
        ubuntu|debian)
            apt-get update -qq
            apt-get install -y -qq ca-certificates curl gnupg lsb-release

            # Add Docker's official GPG key
            install -m 0755 -d /etc/apt/keyrings
            curl -fsSL https://download.docker.com/linux/$OS/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
            chmod a+r /etc/apt/keyrings/docker.gpg

            # Add the repository
            echo \
              "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/$OS \
              $(lsb_release -cs) stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null

            apt-get update -qq
            apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
            ;;
        centos|rhel|fedora|rocky|almalinux)
            yum install -y yum-utils
            yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
            yum install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
            ;;
        gentoo)
            # Gentoo already has Docker in portage
            if ! command -v docker &> /dev/null; then
                emerge --sync || true
                emerge -u app-containers/docker || true
            fi
            ;;
        *)
            # Try generic installation
            curl -fsSL https://get.docker.com | sh
            ;;
    esac

    # Enable and start Docker
    systemctl enable docker
    systemctl start docker

    # Wait for Docker to be ready
    for i in {1..30}; do
        if docker info &>/dev/null; then
            log "Docker is ready"
            return 0
        fi
        sleep 2
    done
    error_exit "Docker failed to start"
}

if ! command -v docker &> /dev/null; then
    install_docker
else
    log "Docker already installed"
    systemctl enable docker
    systemctl start docker
fi

`)

	// Create directories
	fmt.Fprintf(&sb, `# Create directories
log "Creating directories..."
mkdir -p %s
mkdir -p %s
mkdir -p %s
mkdir -p /opt/portage-builder
mkdir -p /var/log/portage-engine

`, config.DataDir, config.WorkDir, config.ArtifactDir)

	// Setup swap
	if config.SwapSizeGB > 0 {
		fmt.Fprintf(&sb, `# Setup swap
setup_swap() {
    if [ ! -f /swapfile ]; then
        log "Creating %dGB swap file..."
        fallocate -l %dG /swapfile || dd if=/dev/zero of=/swapfile bs=1G count=%d
        chmod 600 /swapfile
        mkswap /swapfile
        swapon /swapfile
        echo '/swapfile none swap sw 0 0' >> /etc/fstab
        log "Swap configured"
    fi
}
setup_swap

`, config.SwapSizeGB, config.SwapSizeGB, config.SwapSizeGB)
	}

	// Pull Docker image
	dockerImage := config.DockerImage
	if config.DockerRegistry != "" {
		dockerImage = config.DockerRegistry + "/" + config.DockerImage
	}

	fmt.Fprintf(&sb, `# Pull Gentoo Docker image
log "Pulling Docker image: %s"
docker pull %s || error_exit "Failed to pull Docker image"
log "Docker image pulled successfully"

`, dockerImage, dockerImage)

	// Setup Portage configuration.
	//
	// NPROC and MARCH are resolved by the shell BEFORE the heredoc and the
	// heredoc is unquoted, so ${NPROC}/${MARCH} expand to concrete values.
	// Portage's make.conf parser does NOT evaluate $() command substitution, so
	// writing "$(nproc)" literally would break emerge — we must resolve it here.
	// MARCH is a fixed baseline per advertised arch (never -march=native), so
	// packages built on any instance run on any consumer of that arch.
	march := marchForArch(config.Architecture)
	binhostLine := ""
	if config.PortageBinpkgHost != "" {
		binhostLine = fmt.Sprintf("PORTAGE_BINHOST=\"%s\"\n", config.PortageBinpkgHost)
	}

	fmt.Fprintf(&sb, `# Setup Portage directories and configuration
log "Setting up Portage configuration..."

# Create portage directories
mkdir -p %s/repos/gentoo
mkdir -p %s/distfiles
mkdir -p %s/packages

# Resolve CPU count and march baseline at run time (Portage cannot evaluate $()).
NPROC="$(nproc)"
MARCH="%s"

# Create make.conf for the container (unquoted heredoc: ${NPROC}/${MARCH} expand).
cat > %s/make.conf <<MAKECONF
# Portage Engine Builder Configuration
CFLAGS="-O2 -pipe -march=${MARCH}"
CXXFLAGS="\${CFLAGS}"
MAKEOPTS="-j${NPROC}"
EMERGE_DEFAULT_OPTS="--jobs=${NPROC} --load-average=${NPROC}"

# Portage directories
PORTDIR="/var/db/repos/gentoo"
DISTDIR="/var/cache/distfiles"
PKGDIR="/var/cache/binpkgs"

# Features
FEATURES="buildpkg binpkg-multi-instance parallel-fetch parallel-install"

# Mirrors
GENTOO_MIRRORS="%s"

# Binary packages host
%sMAKECONF

log "Portage configuration created (march=${MARCH}, jobs=${NPROC})"

`, config.DataDir, config.DataDir, config.DataDir, march, config.DataDir, config.PortageMirror, binhostLine)

	// Build a real /etc/portage in DataDir so the builder's bind mount has a
	// valid profile symlink + our make.conf. Without this the container gets an
	// empty /etc/portage and every emerge fails ("make.profile is not a symlink").
	fmt.Fprintf(&sb, `# Seed a valid /etc/portage for the build container
log "Preparing container /etc/portage..."
mkdir -p %s/portage
# Copy the stage3 image's /etc/portage (profile symlink, repos.conf, etc).
docker run --rm -v %s/portage:/out %s \
    sh -c 'cp -a /etc/portage/. /out/ 2>/dev/null || true' || log "Warning: could not seed /etc/portage from image"
# Overlay our generated make.conf.
cp %s/make.conf %s/portage/make.conf
log "Container /etc/portage prepared"

`, config.DataDir, config.DataDir, dockerImage, config.DataDir, config.DataDir)

	// Sync Portage tree
	if config.PortageTreeSync {
		fmt.Fprintf(&sb, `# Sync Portage tree
log "Syncing Portage tree..."
docker run --rm \
    -v %s/repos/gentoo:/var/db/repos/gentoo \
    %s \
    emerge --sync || log "Warning: Portage sync failed, will retry later"

log "Portage tree sync complete"

`, config.DataDir, dockerImage)
	}

	// Download builder binary if URL provided
	if config.BuilderBinaryURL != "" {
		fmt.Fprintf(&sb, `# Download builder binary
log "Downloading builder binary..."
curl -fsSL -o /opt/portage-builder/portage-builder "%s" || error_exit "Failed to download builder"
chmod +x /opt/portage-builder/portage-builder
log "Builder binary downloaded"

`, config.BuilderBinaryURL)
	}

	// Create builder configuration.
	//
	// The directory must exist BEFORE the write (the script runs under `set -e`,
	// so a redirection into a missing directory would abort the whole bootstrap).
	// INSTANCE_ID is resolved by the shell (`$(hostname)` when unset) via an
	// unquoted heredoc, so the builder registers with a real ID rather than the
	// literal string "$(hostname)" (systemd EnvironmentFile does no expansion).
	//
	// PORTAGE_*_PATH / MAKE_CONF_PATH point the builder at the synced tree and
	// generated make.conf in DataDir, so the build container mounts real content
	// instead of empty host dirs (/var/db/repos, /etc/portage don't exist on the
	// Ubuntu/CentOS host).
	// INSTANCE_ID_VAL is a shell assignment: single-quote a supplied value, or
	// use the unquoted $(hostname) command substitution when unset.
	instanceIDAssign := "$(hostname)"
	if config.InstanceID != "" {
		instanceIDAssign = shellSingleQuote(config.InstanceID)
	}

	// The remaining values are embedded inside an unquoted heredoc, so they must
	// be escaped so $, `, and \ land literally (not shell-expanded).
	tokenLine := ""
	if config.BuilderToken != "" {
		tokenLine = fmt.Sprintf("BUILDER_TOKEN=%s\n", heredocEscape(config.BuilderToken))
	}

	fmt.Fprintf(&sb, `# Create builder configuration
log "Creating builder configuration..."
mkdir -p /etc/portage-engine
INSTANCE_ID_VAL=%s
cat > /etc/portage-engine/builder.conf <<BUILDERCONF
# Portage Builder Configuration
BUILDER_PORT=%d
INSTANCE_ID=${INSTANCE_ID_VAL}
ARCHITECTURE=%s
USE_DOCKER=true
DOCKER_IMAGE=%s
BUILD_WORK_DIR=%s
BUILD_ARTIFACT_DIR=%s
DATA_DIR=%s
PERSISTENCE_ENABLED=true
RETENTION_DAYS=7
SERVER_URL=%s
%sPORTAGE_REPOS_PATH=%s/repos
PORTAGE_CONF_PATH=%s/portage
MAKE_CONF_PATH=%s/make.conf
BUILDERCONF

log "Builder configuration created"

`, instanceIDAssign, config.BuilderPort, heredocEscape(config.Architecture), heredocEscape(dockerImage),
		heredocEscape(config.WorkDir), heredocEscape(config.ArtifactDir), heredocEscape(config.DataDir),
		heredocEscape(config.ServerCallbackURL), tokenLine,
		config.DataDir, config.DataDir, config.DataDir)

	// Create systemd service
	sb.WriteString(`# Create systemd service
log "Creating systemd service..."
cat > /etc/systemd/system/portage-builder.service <<'SERVICEUNIT'
[Unit]
Description=Portage Builder Service
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
EnvironmentFile=/etc/portage-engine/builder.conf
ExecStart=/opt/portage-builder/portage-builder
Restart=always
RestartSec=10
StandardOutput=append:/var/log/portage-engine/builder.log
StandardError=append:/var/log/portage-engine/builder.log

[Install]
WantedBy=multi-user.target
SERVICEUNIT

systemctl daemon-reload

`)

	// Start builder service if binary exists
	sb.WriteString(`# Start builder service
if [ -x /opt/portage-builder/portage-builder ]; then
    log "Starting builder service..."
    systemctl enable portage-builder
    systemctl start portage-builder
    log "Builder service started"
else
    log "Builder binary not found, service not started"
fi

`)

	// Configure firewall if enabled
	if config.EnableFirewall {
		fmt.Fprintf(&sb, `# Configure firewall
configure_firewall() {
    log "Configuring firewall..."

    case $OS in
        ubuntu|debian)
            if command -v ufw &> /dev/null; then
                ufw allow 22/tcp
                ufw allow %d/tcp
                ufw --force enable || true
            fi
            ;;
        centos|rhel|fedora|rocky|almalinux)
            if command -v firewall-cmd &> /dev/null; then
                firewall-cmd --permanent --add-port=22/tcp
                firewall-cmd --permanent --add-port=%d/tcp
                firewall-cmd --reload || true
            fi
            ;;
        gentoo)
            if command -v iptables &> /dev/null; then
                iptables -A INPUT -p tcp --dport 22 -j ACCEPT
                iptables -A INPUT -p tcp --dport %d -j ACCEPT
            fi
            ;;
    esac

    log "Firewall configured"
}
configure_firewall

`, config.BuilderPort, config.BuilderPort, config.BuilderPort)
	}

	// Install extra packages
	if len(config.ExtraPackages) > 0 {
		pkgs := strings.Join(config.ExtraPackages, " ")
		fmt.Fprintf(&sb, `# Install extra packages
log "Installing extra packages: %s"
case $OS in
    ubuntu|debian)
        apt-get install -y -qq %s || true
        ;;
    centos|rhel|fedora|rocky|almalinux)
        yum install -y %s || true
        ;;
    gentoo)
        emerge -u %s || true
        ;;
esac

`, pkgs, pkgs, pkgs, pkgs)
	}

	// Callback to server. INSTANCE_ID_VAL is the shell variable set in the
	// builder-config block, so the registration uses the real instance ID.
	if config.ServerCallbackURL != "" {
		fmt.Fprintf(&sb, `# Notify server that instance is ready
log "Notifying server..."
PUBLIC_IP=$(curl -s http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip -H "Metadata-Flavor: Google" 2>/dev/null || curl -s http://169.254.169.254/latest/meta-data/public-ipv4 2>/dev/null || hostname -I | awk '{print $1}')

curl -s -X POST "%s/api/v1/builders/register" \
    -H "Content-Type: application/json" \
    -d "{\"instance_id\": \"${INSTANCE_ID_VAL}\", \"ip_address\": \"${PUBLIC_IP}\", \"port\": %d, \"status\": \"ready\"}" || true

log "Server notified"

`, config.ServerCallbackURL, config.BuilderPort)
	}

	// Final status
	sb.WriteString(`# Finalize
log "Cloud initialization complete!"
log "Instance is ready to accept build jobs"

# Create ready marker
touch /var/lib/portage-engine/.ready
echo "$(date '+%Y-%m-%d %H:%M:%S')" > /var/lib/portage-engine/.ready

exit 0
`)

	return sb.String()
}

// GenerateStartupScript generates a minimal startup script for GCP metadata.
func GenerateStartupScript(config *CloudInitConfig) string {
	// For GCP, the startup script needs to be more compact
	// It downloads and runs the full init script
	return GenerateCloudInitScript(config)
}

// GenerateUserData generates cloud-init user-data for AWS/Azure.
func GenerateUserData(config *CloudInitConfig) string {
	script := GenerateCloudInitScript(config)

	return fmt.Sprintf(`#cloud-config
runcmd:
  - |
%s
`, indentScript(script, "    "))
}

// indentScript indents each line of a script.
func indentScript(script, indent string) string {
	lines := strings.Split(script, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		result = append(result, indent+line)
	}
	return strings.Join(result, "\n")
}
