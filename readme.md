# Portage Engine

A distributed binary package building and management system for Gentoo Linux. The system automatically provisions cloud infrastructure to build packages when they're not available in the binary package server.

## Architecture

The Portage Engine consists of four main components:

### 1. Portage Engine Server
Central server that handles package queries, build requests, and coordinates infrastructure provisioning.

**Features:**
- Package availability queries
- Build request management
- Multi-cloud infrastructure provisioning (Aliyun, GCP, AWS)
- Binary package synchronization
- RESTful API

### 2. Infrastructure as Code (IaC)
Automated cloud infrastructure provisioning system that creates build machines on-demand.

**Supported Providers:**
- Aliyun (Alibaba Cloud)
- Google Cloud Platform (GCP)
- Amazon Web Services (AWS)

### 3. Portage Client Scripts
Client-side tools for package installation and configuration on Gentoo systems.

**Features:**
- Automatic package query and installation
- Build request submission
- Portage integration
- Build status monitoring

### 4. Dashboard
Web-based monitoring and management interface for the build cluster.

**Features:**
- Real-time cluster status monitoring
- Build job tracking
- Instance management
- Authentication support (with anonymous access option)

## Installation

### Prerequisites

- Go 1.21 or later
- Gentoo Linux (for client)
- Cloud provider credentials (Aliyun/GCP/AWS)

### Building from Source

```bash
# Clone the repository
git clone https://github.com/slchris/portage-engine.git
cd portage-engine

# Download dependencies
go mod download

# Build all components
make build

# Or build individually
go build -o bin/portage-server cmd/server/main.go
go build -o bin/portage-dashboard cmd/dashboard/main.go
```

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
