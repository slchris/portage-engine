# Portage Engine

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
Command-line client for submitting build requests with custom configurations.

**Features:**
- Submit builds with configuration files
- Generate configuration bundles
- Monitor build status
- Support for batch operations

### 5. Dashboard
Web-based monitoring and management interface for the build cluster.

**Features:**
- Real-time cluster status monitoring
- Build job tracking
- Instance management
- Authentication support (with anonymous access option)

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
./bin/portage-server -config configs/server.yaml

# 2. In another terminal, submit a build
./bin/portage-client \
  -server=http://localhost:8080 \
  -package=dev-lang/python \
  -version=3.11 \
  -use=ssl,threads,sqlite

# 3. Monitor via Dashboard (optional)
./bin/portage-dashboard -config configs/dashboard.yaml
# Visit http://localhost:8081
```

## 📖 Usage Examples

### Simple Build with USE Flags

```bash
# Build Python 3.11 with specific USE flags
./bin/portage-client \
  -package=dev-lang/python \
  -version=3.11 \
  -use=ssl,threads,sqlite,readline
```

### 🆕 Build Using System Portage Configuration

**New Feature**: Use your system's `/etc/portage` configuration directly!

```bash
# Build with your exact system configuration
./bin/portage-client \
  -portage-dir=/etc/portage \
  -package=dev-lang/python:3.11

# This will:
# ✓ Read all your package.use settings
# ✓ Include package.accept_keywords
# ✓ Apply your make.conf settings
# ✓ Use your repository configurations
# ✓ Ensure USE flag consistency

# Generate a configuration bundle from your system
./bin/portage-client \
  -portage-dir=/etc/portage \
  -package=dev-lang/python:3.11 \
  -output=python-system-config.tar.gz
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

2. Submit the build:

```bash
./bin/portage-client \
  -config=my-config.json \
  -package=dev-lang/python \
  -version=3.11
```

### Generate Configuration Bundle

```bash
# Create a configuration bundle without building
./bin/portage-client \
  -config=my-config.json \
  -package=dev-lang/python \
  -output=python-build.tar.gz

# Inspect the bundle
tar -tzf python-build.tar.gz
```

For more examples, see [docs/EXAMPLES.md](docs/EXAMPLES.md)

## Configuration

### Server Configuration

Edit `configs/server.yaml`:

```yaml
port: 8080
binpkg_path: /var/cache/binpkgs
max_workers: 5
cloud_config:
  default_provider: gcp
```

### Dashboard Configuration

Edit `configs/dashboard.yaml`:

```yaml
port: 8081
server_url: http://localhost:8080
auth_enabled: true
allow_anonymous: true
```

### Client Configuration

Edit `configs/client.conf`:

```bash
PORTAGE_ENGINE_URL=http://your-server:8080
CLOUD_PROVIDER=gcp
```

## Usage

### Starting the Server

```bash
./bin/portage-server -config configs/server.yaml
```

### Starting the Dashboard

```bash
./bin/portage-dashboard -config configs/dashboard.yaml
```

### Client Usage

```bash
# Configure portage integration
sudo ./scripts/portage-client.sh configure

# Install a package (query/build/install automatically)
sudo ./scripts/portage-client.sh install gcc 13.2.0

# Query package availability
./scripts/portage-client.sh query gcc 13.2.0

# Request a build
./scripts/portage-client.sh build gcc 13.2.0

# Check build status
./scripts/portage-client.sh status <job-id>
```

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
│   └── dashboard/       # Dashboard entry point
├── internal/
│   ├── server/          # Server implementation
│   ├── binpkg/          # Binary package management
│   ├── builder/         # Build management
│   ├── iac/             # Infrastructure provisioning
│   └── dashboard/       # Dashboard implementation
├── pkg/
│   └── config/          # Configuration management
├── scripts/
│   └── portage-client.sh # Client script
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

```bash
# Build Docker images
docker build -t portage-engine-server -f Dockerfile.server .
docker build -t portage-engine-dashboard -f Dockerfile.dashboard .

# Run with Docker Compose
docker-compose up -d
```

### Kubernetes Deployment

```bash
kubectl apply -f deployments/kubernetes/
```

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
