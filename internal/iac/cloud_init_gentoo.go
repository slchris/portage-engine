package iac

import (
	"fmt"
	"strings"
)

// cloud_init_gentoo.go generates the deployment script for a NATIVE Gentoo VM
// (cloned from the Gentoo cloud-init template) — no Docker. It configures
// make.conf for the build (mirror binhost + gpkg signing), sets up the signing
// trust store with the getuto + check-trustdb + chown-nobody recipe that makes
// portage's post-sign self-verification pass on a real VM (it cannot inside a
// container), then installs and starts the builder in native mode.

// GenerateGentooNativeScript returns the bootstrap script for a native Gentoo
// build node. The builder binary and (optionally) the signing secret key are
// staged by deployBuilder at /opt/portage-builder/portage-builder and
// /tmp/pe-gpg-secret.asc before this runs.
func GenerateGentooNativeScript(config *CloudInitConfig) string {
	arch := config.Architecture
	if arch == "" {
		arch = "amd64"
	}
	var sb strings.Builder

	sb.WriteString(`#!/bin/bash
set -euo pipefail
log() { echo "[gentoo-native] $*"; }
export DEBIAN_FRONTEND=noninteractive

log "Configuring native Gentoo build node..."
mkdir -p /etc/portage-engine /var/log/portage-engine /var/tmp/portage-builds /var/tmp/portage-artifacts /var/lib/portage-engine
`)

	// make.conf: mirror + binhost + build FEATURES. The template already sets
	// GENTOO_MIRRORS/profile; we append build-farm settings idempotently.
	binhost := ""
	if config.PortageBinpkgHost != "" {
		binhost = config.PortageBinpkgHost
	} else if config.ServerCallbackURL != "" {
		binhost = strings.TrimRight(config.ServerCallbackURL, "/") + "/binpkgs"
	}
	fmt.Fprintf(&sb, `
# --- Portage Engine build settings (idempotent) ---
sed -i '/# PE-BUILD-BEGIN/,/# PE-BUILD-END/d' /etc/portage/make.conf 2>/dev/null || true
cat >> /etc/portage/make.conf <<'MAKECONF'
# PE-BUILD-BEGIN
FEATURES="${FEATURES} buildpkg"
`)
	if binhost != "" {
		fmt.Fprintf(&sb, "PORTAGE_BINHOST=%s\n", heredocEscape(binhost))
	}
	if config.MakeConfExtra != "" {
		sb.WriteString(heredocEscape(config.MakeConfExtra) + "\n")
	}
	sb.WriteString("# PE-BUILD-END\nMAKECONF\n")

	// Signing setup (native VM): the whole point of moving off containers.
	if config.GPGKeyID != "" {
		fmt.Fprintf(&sb, `
log "Setting up binpkg signing (key %s)..."
export GNUPGHOME=/root/.gnupg
mkdir -p "$GNUPGHOME" && chmod 700 "$GNUPGHOME"
if [ -f /tmp/pe-gpg-secret.asc ]; then
    gpg --batch --yes --import /tmp/pe-gpg-secret.asc
    gpg --export --armor %s > /tmp/pe-pub.asc
    rm -f /tmp/pe-gpg-secret.asc
fi
# Enable in-emerge signing in make.conf.
sed -i '/# PE-SIGN-BEGIN/,/# PE-SIGN-END/d' /etc/portage/make.conf 2>/dev/null || true
cat >> /etc/portage/make.conf <<'SIGNCONF'
# PE-SIGN-BEGIN
BINPKG_FORMAT="gpkg"
FEATURES="${FEATURES} binpkg-signing gpg-keepalive"
BINPKG_GPG_SIGNING_GPG_HOME="/root/.gnupg"
BINPKG_GPG_SIGNING_KEY="%s"
# PE-SIGN-END
SIGNCONF
# Build the verify trust store. getuto seeds /etc/portage/gnupg with the
# Gentoo release keys; we add our signing key with ultimate ownertrust, then
# --check-trustdb so the ultimate validity is precomputed (portage verifies
# with --no-auto-check-trustdb and would otherwise see the key as untrusted).
getuto 2>/dev/null || true
if [ -f /tmp/pe-pub.asc ]; then
    gpg --homedir /etc/portage/gnupg --batch --yes --import /tmp/pe-pub.asc 2>/dev/null || true
    gpg --homedir /etc/portage/gnupg --with-colons --list-keys 2>/dev/null | awk -F: '/^fpr:/{print $10":6:"}' | gpg --homedir /etc/portage/gnupg --batch --yes --import-ownertrust 2>/dev/null || true
    gpg --homedir /etc/portage/gnupg --check-trustdb 2>/dev/null || true
    rm -f /tmp/pe-pub.asc
fi
# Post-sign verification runs as the 'nobody' user (GPG_VERIFY_USER_DROP): the
# store must be owned by nobody, mode 700, or gpg refuses it.
chown -R nobody:nobody /etc/portage/gnupg
find /etc/portage/gnupg -type d -exec chmod 700 {} \; 2>/dev/null || true
log "Signing store ready"
`, config.GPGKeyID, config.GPGKeyID, config.GPGKeyID)
	}

	// builder.conf (native mode: USE_DOCKER=false, host portage paths).
	tokenLine := ""
	if config.BuilderToken != "" {
		tokenLine = fmt.Sprintf("BUILDER_TOKEN=%s\n", heredocEscape(config.BuilderToken))
	}
	fmt.Fprintf(&sb, `
log "Writing builder configuration (native mode)..."
INSTANCE_ID_VAL=$(hostname)
cat > /etc/portage-engine/builder.conf <<BUILDERCONF
BUILDER_PORT=%d
INSTANCE_ID=${INSTANCE_ID_VAL}
ARCHITECTURE=%s
USE_DOCKER=false
BUILD_WORK_DIR=/var/tmp/portage-builds
BUILD_ARTIFACT_DIR=/var/tmp/portage-artifacts
DATA_DIR=/var/lib/portage-engine
PERSISTENCE_ENABLED=true
RETENTION_DAYS=7
SERVER_URL=%s
%sPORTAGE_REPOS_PATH=/var/db/repos
PORTAGE_CONF_PATH=/etc/portage
MAKE_CONF_PATH=/etc/portage/make.conf
BUILDERCONF

`, config.BuilderPort, heredocEscape(arch), heredocEscape(config.ServerCallbackURL), tokenLine)

	// systemd unit (no docker dependency).
	sb.WriteString(`log "Installing systemd service..."
cat > /etc/systemd/system/portage-builder.service <<'SERVICEUNIT'
[Unit]
Description=Portage Builder Service (native)
After=network-online.target
Wants=network-online.target

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
if [ -x /opt/portage-builder/portage-builder ]; then
    log "Starting builder service..."
    systemctl enable portage-builder
    systemctl restart portage-builder
    log "Native Gentoo builder started"
else
    log "ERROR: builder binary missing at /opt/portage-builder/portage-builder"
    exit 1
fi
`)
	return sb.String()
}
