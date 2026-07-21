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
	PortageTreeSync bool   `json:"portage_tree_sync"`
	PortageMirror   string `json:"portage_mirror"`
	// Mirror acceleration (all optional). AptMirror rewrites the guest's apt
	// sources; DockerDownloadMirror feeds DOWNLOAD_URL of the vendored
	// get.docker.com script; DockerRegistryMirror lands in daemon.json;
	// PortageSyncURI overrides the gentoo repo sync-uri (rsync/git).
	AptMirror            string `json:"apt_mirror"`
	DockerDownloadMirror string `json:"docker_download_mirror"`
	DockerRegistryMirror string `json:"docker_registry_mirror"`
	PortageSyncURI       string `json:"portage_sync_uri"`
	PortageSyncMethod    string `json:"portage_sync_method"`
	// MakeConfExtra is appended verbatim to the generated make.conf (dashboard
	// "build config" box: USE, ACCEPT_LICENSE, FEATURES, ...).
	MakeConfExtra     string `json:"make_conf_extra"`
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
	// GPGKeyID enables binpkg signing on the builder: the deploy step pushes
	// the secret key to /tmp/pe-gpg-secret.asc and this script imports it.
	GPGKeyID string
	// BuildFeatures is appended to the build container's make.conf FEATURES via
	// the builder's BUILD_FEATURES env (default "-userpriv -usersandbox" for
	// Docker builds; empty for a native Gentoo VM).
	BuildFeatures string
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

	if config.DockerDownloadMirror != "" {
		fmt.Fprintf(&sb, `DOCKER_DOWNLOAD_URL=%s
`, shellSingleQuote(config.DockerDownloadMirror))
	}

	// Switch the guest's apt sources to the internal mirror before anything
	// touches the network (debian cloud images ship deb822 sources).
	if config.AptMirror != "" {
		fmt.Fprintf(&sb, `# Use internal apt mirror
if [ "$OS" = "debian" ]; then
    log "Switching apt sources to mirror %s"
    rm -f /etc/apt/sources.list.d/debian.sources
    CODENAME=$(. /etc/os-release && echo "$VERSION_CODENAME")
    cat > /etc/apt/sources.list <<APTSOURCES
deb %s/debian $CODENAME main contrib non-free non-free-firmware
deb %s/debian $CODENAME-updates main contrib non-free non-free-firmware
deb %s/debian-security $CODENAME-security main contrib non-free non-free-firmware
APTSOURCES
fi

`, heredocEscape(config.AptMirror), heredocEscape(config.AptMirror), heredocEscape(config.AptMirror), heredocEscape(config.AptMirror))
	}

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
    if [ -n "${DOCKER_DOWNLOAD_URL}" ] && [ -f /tmp/docker-install.sh ]; then
        log "Installing Docker from mirror ${DOCKER_DOWNLOAD_URL}"
        DOWNLOAD_URL="${DOCKER_DOWNLOAD_URL}" sh /tmp/docker-install.sh || error_exit "mirror docker install failed"
        systemctl enable docker
        systemctl start docker
    else
        install_docker
    fi
else
    log "Docker already installed"
    systemctl enable docker
    systemctl start docker
fi

`)

	// Docker Hub registry mirror: write daemon.json and restart so image pulls
	// (gentoo/stage3) go through the internal registry. Plain-HTTP mirrors also
	// need an insecure-registries entry or dockerd refuses them.
	if config.DockerRegistryMirror != "" {
		insecureLine := ""
		if host, ok := strings.CutPrefix(config.DockerRegistryMirror, "http://"); ok {
			insecureLine = fmt.Sprintf(`, "insecure-registries": ["%s"]`, strings.TrimSuffix(host, "/"))
		}
		fmt.Fprintf(&sb, `# Configure Docker registry mirror
log "Configuring Docker registry mirror..."
mkdir -p /etc/docker
cat > /etc/docker/daemon.json <<'DOCKERDAEMON'
{ "registry-mirrors": ["%s"]%s }
DOCKERDAEMON
systemctl restart docker

`, config.DockerRegistryMirror, insecureLine)
	}

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
for i in 1 2 3; do
    docker pull %s && break
    [ $i -eq 3 ] && error_exit "Failed to pull Docker image after 3 attempts"
    log "Pull failed, retrying ($i/3)..."
    sleep 10
done
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

%s`, config.DataDir, config.DataDir, config.DataDir, march, config.DataDir, config.PortageMirror, binhostLine, makeConfExtraBlock(config))

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

	// Sync Portage tree. With a custom sync URI, write a repos.conf override
	// and rsync/git-sync from it; otherwise fetch the snapshot via
	// emerge-webrsync (honors GENTOO_MIRRORS, so an internal mirror makes this
	// a LAN download), falling back to plain emerge --sync.
	if config.PortageTreeSync {
		if config.PortageSyncMethod == "rsync" && config.PortageSyncURI != "" {
			fmt.Fprintf(&sb, `# Sync Portage tree from custom sync-uri
log "Syncing Portage tree from custom sync-uri..."
mkdir -p %s/portage/repos.conf
cat > %s/portage/repos.conf/gentoo.conf <<'REPOSCONF'
[DEFAULT]
main-repo = gentoo

[gentoo]
location = /var/db/repos/gentoo
sync-type = rsync
sync-uri = %s
auto-sync = yes
REPOSCONF
docker run --rm \
    -v %s/repos/gentoo:/var/db/repos/gentoo \
    -v %s/portage:/etc/portage \
    %s \
    emerge --sync || log "Warning: Portage sync failed, will retry later"

log "Portage tree sync complete"

`, config.DataDir, config.DataDir, config.PortageSyncURI, config.DataDir, config.DataDir, dockerImage)
		} else {
			fmt.Fprintf(&sb, `# Sync Portage tree (webrsync snapshot via GENTOO_MIRRORS)
log "Syncing Portage tree (webrsync)..."
docker run --rm \
    -v %s/repos/gentoo:/var/db/repos/gentoo \
    -e GENTOO_MIRRORS=%s \
    %s \
    sh -c 'getuto >/dev/null 2>&1 || true; emerge-webrsync' || log "Warning: webrsync failed - check GENTOO_MIRRORS/snapshots on the mirror"

log "Portage tree sync complete"

`, config.DataDir, shellSingleQuote(config.PortageMirror), dockerImage)
		}
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
	gpgLines := ""
	if config.GPGKeyID != "" {
		gpgLines = fmt.Sprintf("GPG_ENABLED=true\nGPG_KEY_ID=%s\nGPG_HOME=/var/lib/portage-engine/gpg\nGPG_AUTO_CREATE=false\n", heredocEscape(config.GPGKeyID))
	}
	if config.BuildFeatures != "" {
		gpgLines += fmt.Sprintf("BUILD_FEATURES=%s\n", heredocEscape(config.BuildFeatures))
	}

	if config.GPGKeyID != "" {
		sb.WriteString(`# Import binhost signing key (pushed by the deploy step)
if [ -f /tmp/pe-gpg-secret.asc ]; then
    log "Importing binhost signing key..."
    mkdir -p /var/lib/portage-engine/gpg
    chmod 700 /var/lib/portage-engine/gpg
    gpg --homedir /var/lib/portage-engine/gpg --batch --yes --import /tmp/pe-gpg-secret.asc || log "WARNING: GPG key import failed"
    rm -f /tmp/pe-gpg-secret.asc
fi

`)
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
%s%sPORTAGE_REPOS_PATH=%s/repos
PORTAGE_CONF_PATH=%s/portage
MAKE_CONF_PATH=%s/make.conf
BUILDERCONF

log "Builder configuration created"

`, instanceIDAssign, config.BuilderPort, heredocEscape(config.Architecture), heredocEscape(dockerImage),
		heredocEscape(config.WorkDir), heredocEscape(config.ArtifactDir), heredocEscape(config.DataDir),
		heredocEscape(config.ServerCallbackURL), tokenLine, gpgLines,
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

// makeConfExtraBlock renders the operator-supplied make.conf additions
// (dashboard build-config box), heredoc-escaped so values cannot break out of
// the generated script.
func makeConfExtraBlock(config *CloudInitConfig) string {
	if strings.TrimSpace(config.MakeConfExtra) == "" {
		return ""
	}
	return "\n# --- operator-supplied build configuration (dashboard) ---\n" + heredocEscape(config.MakeConfExtra) + "\n"
}
