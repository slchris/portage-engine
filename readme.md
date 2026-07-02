# Portage Engine

[![CI](https://github.com/slchris/portage-engine/actions/workflows/ci.yml/badge.svg)](https://github.com/slchris/portage-engine/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/slchris/portage-engine)](https://goreportcard.com/report/github.com/slchris/portage-engine)
[![GoDoc](https://godoc.org/github.com/slchris/portage-engine?status.svg)](https://godoc.org/github.com/slchris/portage-engine)
[![CodeQL](https://github.com/slchris/portage-engine/actions/workflows/codeql.yml/badge.svg)](https://github.com/slchris/portage-engine/actions/workflows/codeql.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A distributed binary package building and management system for Gentoo Linux with advanced configuration transfer capabilities. The system automatically provisions cloud infrastructure or Docker containers to build packages with custom USE flags and configurations when they're not available in the binary package server.

## 🎯 Key Features

- **Configuration Transfer**: Transfer complete Portage configurations (USE flags, keywords, masks, etc.) to build instances
- **Flexible Build Environments**: Support for Docker containers and cloud infrastructure (Aliyun, GCP, AWS)
- **Package Customization**: Build packages with specific USE flag combinations
- **Automated Infrastructure**: On-demand provisioning of build resources
- **RESTful API**: Complete API for integration with existing tools
- **Web Dashboard**: Real-time monitoring and management interface

## Architecture

The Portage Engine consists of five main components:

### 1. Portage Engine Server
Central server that handles package queries, build requests, and coordinates infrastructure provisioning.

**Features:**
- Package availability queries
- Build request management with configuration bundles
- Multi-cloud infrastructure provisioning (Aliyun, GCP, AWS)
- Docker-based local builds
- Binary package synchronization
- RESTful API

### 2. Configuration Transfer System
Advanced system for collecting, packaging, and applying Portage configurations.

**Features:**
- **Read system configuration**: Direct import from `/etc/portage` directory
- Collect user's Portage configuration (package.use, make.conf, etc.)
- Package configuration into portable bundles
- Transfer and apply configuration to build instances
- Support for package-specific USE flags and environment variables
- Repository configuration management
- Ensure USE flag consistency between systems

**📚 See detailed documentation**: [Using System Portage Configuration](docs/SYSTEM_CONFIG_USAGE.md)

### 3. Infrastructure as Code (IaC)
Automated cloud infrastructure provisioning system that creates build machines on-demand.

**Supported Providers:**
- Aliyun (Alibaba Cloud)
- Google Cloud Platform (GCP)
- Amazon Web Services (AWS)
- Docker containers (local builds)

### 4. Portage Client Tool
A management/request CLI. It does **not** install packages — that is done
natively by Portage against the binhost (`emerge --getbinpkg`). The client
covers the parts Portage has no native mechanism for.

**Subcommands:**
- `configure` — point Portage at the binhost (writes `binrepos.conf`)
- `build` — request the server build a package (with optional `-wait`)
- `status` — check a build job
- `bundle` — generate a Portage config bundle without building

### 5. Dashboard
Web-based monitoring and management interface for the build cluster.

**Features:**
- Real-time cluster status monitoring
- Build job tracking
- Instance management
- Authentication support (with anonymous access option)

## 📚 Documentation

- **[Usage guide](docs/USAGE.md)** — consuming packages (native `emerge
  --getbinpkg`) vs. requesting builds, authentication, and config bundles.
- **[Using system Portage configuration](docs/SYSTEM_CONFIG_USAGE.md)** —
  building with your machine's exact `/etc/portage` settings.

## 🚀 Quick Start

### Prerequisites

- Go 1.21 or later
- Docker (optional, for local container builds)
- Gentoo Linux (for client)
- Cloud provider credentials (Aliyun/GCP/AWS) (optional)

### Building from Source

```bash
# Clone the repository
git clone https://github.com/slchris/portage-engine.git
cd portage-engine

# Download dependencies
go mod download

# Build all components
make build

# Binaries will be in bin/:
# - portage-server
# - portage-dashboard
# - portage-builder
# - portage-client
```

### Quick Test

```bash
# 1. Start the server (Docker mode for local testing)
export USE_DOCKER=true
export DOCKER_IMAGE=gentoo/stage3:latest
./bin/portage-server -config configs/server.conf

# 2. In another terminal, request a build
./bin/portage-client build \
  -server=http://localhost:8080 \
  -package=dev-lang/python \
  -version=3.11 \
  -use=ssl,threads,sqlite -wait

# 3. Monitor via Dashboard (optional)
./bin/portage-dashboard -config configs/dashboard.conf
# Visit http://localhost:8081
```

## 📖 Usage Examples

### Simple Build with USE Flags

```bash
# Request a build of Python 3.11 with specific USE flags
./bin/portage-client build \
  -package=dev-lang/python \
  -version=3.11 \
  -use=ssl,threads,sqlite,readline
```

### 🆕 Build Using System Portage Configuration

**Feature**: Use your system's `/etc/portage` configuration directly!

```bash
# Request a build with your exact system configuration
./bin/portage-client build \
  -portage-dir=/etc/portage \
  -package=dev-lang/python:3.11

# This will:
# ✓ Read all your package.use settings
# ✓ Include package.accept_keywords
# ✓ Apply your make.conf settings
# ✓ Use your repository configurations
# ✓ Ensure USE flag consistency

# Generate a configuration bundle from your system (no build)
./bin/portage-client bundle \
  -portage-dir=/etc/portage \
  -package=dev-lang/python:3.11 \
  -out=python-system-config.tar.gz
```

**Benefits**:
- ✅ Guarantees USE flag consistency with your system
- ✅ No manual configuration needed
- ✅ Includes all package-specific settings
- ✅ Respects keywords and masks

See [System Configuration Usage Guide](docs/SYSTEM_CONFIG_USAGE.md) for details.

### Build with Configuration File

1. Create a configuration file `my-config.json`:

```json
{
  "package_use": {
    "dev-lang/python:3.11": ["ssl", "threads", "sqlite"],
    "sys-devel/gcc": ["openmp", "fortran"]
  },
  "make_conf": {
    "MAKEOPTS": "-j8",
    "FEATURES": "buildpkg parallel-install"
  }
}
```

2. Request the build:

```bash
./bin/portage-client build \
  -config=my-config.json \
  -package=dev-lang/python \
  -version=3.11
```

### Generate Configuration Bundle

```bash
# Create a configuration bundle without building
./bin/portage-client bundle \
  -config=my-config.json \
  -package=dev-lang/python \
  -out=python-build.tar.gz

# Inspect the bundle
tar -tzf python-build.tar.gz
```

## Configuration

### Server Configuration

Edit `configs/server.conf`:

```bash
SERVER_PORT=8080
BINPKG_PATH=/var/cache/binpkgs
MAX_WORKERS=5
CLOUD_DEFAULT_PROVIDER=gcp

# Security (strongly recommended for production)
API_KEY=your-api-key-here
CORS_ALLOWED_ORIGINS=https://dashboard.example.com
```

### Dashboard Configuration

Edit `configs/dashboard.conf`:

```bash
DASHBOARD_PORT=8081
SERVER_URL=http://localhost:8080
AUTH_ENABLED=true
# Generate with: openssl rand -hex 32
JWT_SECRET=your-strong-secret-at-least-32-chars
ALLOW_ANONYMOUS=false
```

### Client Configuration

The client is configured via flags (or environment variables); there is no
config file. The server URL is passed with `-server`, and the API key with
`-api-key` or the `PORTAGE_ENGINE_API_KEY` environment variable:

```bash
export PORTAGE_ENGINE_API_KEY=your-api-key-here
./bin/portage-client build -server=http://your-server:8080 -package=dev-lang/python
```

The consume path is configured in Portage itself (see "Consuming packages"
below), not in the client.

## Usage

### Starting the Server

```bash
./bin/portage-server -config configs/server.conf
```

### Starting the Dashboard

```bash
./bin/portage-dashboard -config configs/dashboard.conf
```

### Consuming packages (the normal path)

Portage consumes a binary host natively — there is no special client for
installing. Point Portage at the server's binhost once, then use `emerge` as
usual:

```bash
# One-time: write /etc/portage/binrepos.conf/portage-engine.conf
sudo ./bin/portage-client configure -server=http://your-server:8080

# Enable binary fetching (either flag per-invocation or FEATURES in make.conf)
emerge --getbinpkg gcc
#   or add to /etc/portage/make.conf:  FEATURES="getbinpkg"
```

With `--getbinpkg`, emerge fetches the prebuilt (GPG-signed) package from the
binhost when available and **falls back to a normal source build** when it is
not. Signatures are verified by Portage itself (`verify-signature = true`).

### Requesting a build (optional)

Portage has no native "ask the binhost to build X" mechanism. When you want the
server to build a package with specific USE flags, use the client's `build`
subcommand (this is a request tool, not the install path):

```bash
# Request a build and wait for it to finish
./bin/portage-client build -server=http://your-server:8080 \
  -package=dev-lang/python -version=3.11 -use=ssl,threads -wait

# Check a job later
./bin/portage-client status -server=http://your-server:8080 -job=<job-id>

# Generate a Portage config bundle from your system without building
./bin/portage-client bundle -portage-dir=/etc/portage \
  -package=dev-lang/python -out=python-bundle.tar.gz
```

Once the build completes, the package appears on the binhost and any client with
`--getbinpkg` will pick it up.

## API Documentation

### Package Query

**Endpoint:** `POST /api/v1/packages/query`

**Request:**
```json
{
  "name": "gcc",
  "version": "13.2.0",
  "arch": "x86_64",
  "use_flags": ["openmp", "nls"]
}
```

**Response:**
```json
{
  "found": true,
  "package": {
    "name": "gcc",
    "version": "13.2.0",
    "arch": "x86_64",
    "use_flags": ["openmp", "nls"],
    "path": "/binpkgs/x86_64/gcc-13.2.0.tbz2",
    "checksum": "sha256:..."
  }
}
```

### Request Build

**Endpoint:** `POST /api/v1/packages/request-build`

**Request:**
```json
{
  "package_name": "gcc",
  "version": "13.2.0",
  "arch": "x86_64",
  "use_flags": ["openmp", "nls"],
  "cloud_provider": "gcp",
  "machine_spec": {
    "region": "us-central1",
    "zone": "us-central1-a"
  }
}
```

**Response:**
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "queued"
}
```

### Check Build Status

**Endpoint:** `GET /api/v1/packages/status?job_id=<job_id>`

**Response:**
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "building",
  "package_name": "gcc",
  "version": "13.2.0",
  "arch": "x86_64",
  "created_at": "2025-12-11T10:00:00Z",
  "updated_at": "2025-12-11T10:05:00Z",
  "instance_id": "gcp-12345678"
}
```

## Development

### Project Structure

```
portage-engine/
├── cmd/
│   ├── server/          # Server entry point
│   ├── dashboard/       # Dashboard entry point
│   ├── builder/         # Builder daemon entry point
│   └── client/          # Client CLI (configure / build / status / bundle)
├── internal/
│   ├── server/          # Server implementation (incl. /binpkgs binhost)
│   ├── binpkg/          # Binary package store + Packages index generation
│   ├── builder/         # Build management
│   ├── iac/             # Infrastructure provisioning
│   └── dashboard/       # Dashboard implementation
├── pkg/
│   └── config/          # Configuration management
├── configs/             # Configuration files
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### Running Tests

```bash
go test ./...
```

### Code Style

This project follows Google's Go code style guide:
- Use `gofmt` for formatting
- Follow effective Go guidelines
- Write clear, concise comments
- Use meaningful variable names

## Deployment

### Docker Deployment

A single multi-stage `Dockerfile` at the repository root builds all three
binaries (server, dashboard, builder) into one Gentoo-based image:

```bash
# Build the image (contains portage-server, portage-dashboard, portage-builder)
docker build -t portage-engine .

# Or run the full example stack (server + two builders + dashboard)
docker compose up -d
```

See `docker-compose.yml` for a complete example that wires the server to two
builders and the dashboard.

### Kubernetes Deployment

Kubernetes manifests are not shipped yet (TODO). For now, deploy with Docker
Compose (above) or run the binaries directly (see the Quick Start section).

## License

MIT License

## Contributing

Contributions are welcome! Please follow these steps:

1. Fork the repository
2. Create a feature branch
3. Make your changes following the code style
4. Write tests for new functionality
5. Submit a pull request

## Support

For issues and questions:
- GitHub Issues: https://github.com/slchris/portage-engine/issues
- Documentation: https://github.com/slchris/portage-engine/wiki

## Acknowledgments

- Gentoo Linux community
- Google Go style guide
- Cloud providers (Aliyun, GCP, AWS)
