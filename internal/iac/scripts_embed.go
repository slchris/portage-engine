package iac

import _ "embed"

// dockerInstallScript is the standard get.docker.com installer (vendored so
// deployments do not depend on external network). deployBuilder pushes it to
// instances when a docker download mirror is configured; the script honors
// DOWNLOAD_URL to install from that mirror.
//
//go:embed docker-install.sh
var dockerInstallScript []byte
