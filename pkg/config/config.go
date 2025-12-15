// Package config provides configuration management for Portage Engine.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ServerConfig represents the server configuration.
type ServerConfig struct {
	Port                 int
	BinpkgPath           string
	MaxWorkers           int
	BuildMode            string
	StorageType          string
	StorageLocalDir      string
	StorageS3Bucket      string
	StorageS3Region      string
	StorageS3Prefix      string
	StorageHTTPBase      string
	GPGEnabled           bool
	GPGKeyID             string
	GPGKeyPath           string
	CloudProvider        string
	CloudAliyunRegion    string
	CloudAliyunZone      string
	CloudAliyunAK        string
	CloudAliyunSK        string
	CloudGCPProject      string
	CloudGCPRegion       string
	CloudGCPZone         string
	CloudGCPKeyFile      string
	CloudGCPMachineType  string
	CloudGCPDiskSizeGB   int
	CloudGCPDiskType     string
	CloudGCPImageFamily  string
	CloudGCPImageProject string
	CloudGCPNetwork      string
	CloudGCPSubnetwork   string
	CloudGCPPreemptible  bool
	CloudGCPStateDir     string
	CloudGCPAllowedIPs   []string
	CloudInstanceTTL     int // Instance TTL in minutes, 0 means no auto-termination
	CloudAWSRegion       string
	CloudAWSZone         string
	CloudAWSAccessKey    string
	CloudAWSSecretKey    string
	CloudSSHKeyPath      string
	CloudSSHUser         string
	ServerCallbackURL    string
	RemoteBuilders       []string
	MetricsEnabled       bool
	MetricsPort          string
	MetricsPassword      string
	LogEnabled           bool
	LogLevel             string
	LogDir               string
	LogMaxSizeMB         int
	LogMaxAgeDays        int
	LogMaxBackups        int
	LogEnableConsole     bool
	LogEnableFile        bool
}

// DashboardConfig represents the dashboard configuration.
type DashboardConfig struct {
	Port             int
	ServerURL        string
	AuthEnabled      bool
	JWTSecret        string
	AllowAnonymous   bool
	MetricsEnabled   bool
	MetricsPort      string
	MetricsPassword  string
	LogEnabled       bool
	LogLevel         string
	LogDir           string
	LogMaxSizeMB     int
	LogMaxAgeDays    int
	LogMaxBackups    int
	LogEnableConsole bool
	LogEnableFile    bool
}

// BuilderConfig represents the builder configuration.
type BuilderConfig struct {
	Port               int
	Workers            int
	InstanceID         string
	Architecture       string
	UseDocker          bool
	DockerImage        string // Docker image for builds (e.g., gentoo/stage3:latest)
	WorkDir            string
	ArtifactDir        string
	DataDir            string
	PersistenceEnabled bool
	RetentionDays      int
	GPGEnabled         bool
	GPGKeyID           string
	GPGKeyPath         string
	StorageType        string
	StorageLocalDir    string
	StorageS3Bucket    string
	StorageS3Region    string
	StorageS3Prefix    string
	StorageHTTPBase    string
	ServerURL          string
	NotifyConfig       string
	MetricsEnabled     bool
	MetricsPort        string
	MetricsPassword    string
	LogEnabled         bool
	LogLevel           string
	LogDir             string
	LogMaxSizeMB       int
	LogMaxAgeDays      int
	LogMaxBackups      int
	LogEnableConsole   bool
	LogEnableFile      bool

	// Portage mirror settings (for Gentoo builds in Docker)
	SyncMirror      string // Mirror URL for portage sync (rsync or git)
	DistfilesMirror string // Mirror URL for distfiles download

	// Portage paths on host (mounted into Docker container)
	PortageReposPath string // Path to portage repos (default: /var/db/repos)
	PortageConfPath  string // Path to portage config (default: /etc/portage)
	MakeConfPath     string // Path to make.conf (default: /etc/portage/make.conf)
}

// loadEnvFile loads key=value pairs from a .conf file.
func loadEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	env := make(map[string]string)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on first =
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			env[key] = value
		}
	}

	return env, scanner.Err()
}

// getEnvString gets string value from env map with fallback to system env.
func getEnvString(env map[string]string, key, defaultValue string) string {
	if val, ok := env[key]; ok && val != "" {
		return val
	}
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}

// getEnvInt gets int value from env map with fallback to system env.
func getEnvInt(env map[string]string, key string, defaultValue int) int {
	val := getEnvString(env, key, "")
	if val == "" {
		return defaultValue
	}
	if i, err := strconv.Atoi(val); err == nil {
		return i
	}
	return defaultValue
}

// getEnvBool gets bool value from env map with fallback to system env.
func getEnvBool(env map[string]string, key string, defaultValue bool) bool {
	val := getEnvString(env, key, "")
	if val == "" {
		return defaultValue
	}
	val = strings.ToLower(val)
	return val == "true" || val == "1" || val == "yes"
}

// LoadServerConfig loads server configuration from a file.
func LoadServerConfig(path string) (*ServerConfig, error) {
	// Set defaults
	config := &ServerConfig{
		Port:            8080,
		BinpkgPath:      "/var/cache/binpkgs",
		MaxWorkers:      5,
		BuildMode:       "remote",
		StorageType:     "local",
		StorageLocalDir: "/var/cache/binpkgs",
		GPGEnabled:      false,
		CloudProvider:   "gcp",
		CloudGCPProject: "portage-engine",
		CloudGCPRegion:  "us-central1",
		CloudGCPZone:    "us-central1-a",
	}

	// If config file doesn't exist, return defaults
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Config file not found, using defaults: %s\n", path)
		return config, nil
	}

	env, err := loadEnvFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config.Port = getEnvInt(env, "SERVER_PORT", config.Port)
	config.BinpkgPath = getEnvString(env, "BINPKG_PATH", config.BinpkgPath)
	config.MaxWorkers = getEnvInt(env, "MAX_WORKERS", config.MaxWorkers)
	config.BuildMode = getEnvString(env, "BUILD_MODE", config.BuildMode)

	config.StorageType = getEnvString(env, "STORAGE_TYPE", config.StorageType)
	config.StorageLocalDir = getEnvString(env, "STORAGE_LOCAL_DIR", config.StorageLocalDir)
	config.StorageS3Bucket = getEnvString(env, "STORAGE_S3_BUCKET", "")
	config.StorageS3Region = getEnvString(env, "STORAGE_S3_REGION", "")
	config.StorageS3Prefix = getEnvString(env, "STORAGE_S3_PREFIX", "")
	config.StorageHTTPBase = getEnvString(env, "STORAGE_HTTP_BASE", "")

	config.GPGEnabled = getEnvBool(env, "GPG_ENABLED", config.GPGEnabled)
	config.GPGKeyID = getEnvString(env, "GPG_KEY_ID", "")
	config.GPGKeyPath = getEnvString(env, "GPG_KEY_PATH", "")

	config.CloudProvider = getEnvString(env, "CLOUD_DEFAULT_PROVIDER", config.CloudProvider)
	config.CloudAliyunRegion = getEnvString(env, "CLOUD_ALIYUN_REGION", "cn-hangzhou")
	config.CloudAliyunZone = getEnvString(env, "CLOUD_ALIYUN_ZONE", "cn-hangzhou-a")
	config.CloudAliyunAK = getEnvString(env, "CLOUD_ALIYUN_ACCESS_KEY", "")
	config.CloudAliyunSK = getEnvString(env, "CLOUD_ALIYUN_SECRET_KEY", "")
	config.CloudGCPProject = getEnvString(env, "CLOUD_GCP_PROJECT", config.CloudGCPProject)
	config.CloudGCPRegion = getEnvString(env, "CLOUD_GCP_REGION", config.CloudGCPRegion)
	config.CloudGCPZone = getEnvString(env, "CLOUD_GCP_ZONE", config.CloudGCPZone)
	config.CloudGCPKeyFile = getEnvString(env, "CLOUD_GCP_KEY_FILE", "")
	config.CloudGCPMachineType = getEnvString(env, "CLOUD_GCP_MACHINE_TYPE", "n1-standard-4")
	config.CloudGCPDiskSizeGB = getEnvInt(env, "CLOUD_GCP_DISK_SIZE_GB", 100)
	config.CloudGCPDiskType = getEnvString(env, "CLOUD_GCP_DISK_TYPE", "pd-ssd")
	config.CloudGCPImageFamily = getEnvString(env, "CLOUD_GCP_IMAGE_FAMILY", "ubuntu-2204-lts")
	config.CloudGCPImageProject = getEnvString(env, "CLOUD_GCP_IMAGE_PROJECT", "ubuntu-os-cloud")
	config.CloudGCPNetwork = getEnvString(env, "CLOUD_GCP_NETWORK", "default")
	config.CloudGCPSubnetwork = getEnvString(env, "CLOUD_GCP_SUBNETWORK", "")
	config.CloudGCPPreemptible = getEnvBool(env, "CLOUD_GCP_PREEMPTIBLE", false)
	config.CloudGCPStateDir = getEnvString(env, "CLOUD_GCP_STATE_DIR", "")
	if allowedIPs := getEnvString(env, "CLOUD_GCP_ALLOWED_IPS", ""); allowedIPs != "" {
		config.CloudGCPAllowedIPs = strings.Split(allowedIPs, ",")
		for i := range config.CloudGCPAllowedIPs {
			config.CloudGCPAllowedIPs[i] = strings.TrimSpace(config.CloudGCPAllowedIPs[i])
		}
	}
	config.CloudInstanceTTL = getEnvInt(env, "CLOUD_INSTANCE_TTL", 60) // Default 60 minutes
	config.CloudAWSRegion = getEnvString(env, "CLOUD_AWS_REGION", "us-east-1")
	config.CloudAWSZone = getEnvString(env, "CLOUD_AWS_ZONE", "us-east-1a")
	config.CloudAWSAccessKey = getEnvString(env, "CLOUD_AWS_ACCESS_KEY", "")
	config.CloudAWSSecretKey = getEnvString(env, "CLOUD_AWS_SECRET_KEY", "")
	config.CloudSSHKeyPath = getEnvString(env, "CLOUD_SSH_KEY_PATH", "")
	config.CloudSSHUser = getEnvString(env, "CLOUD_SSH_USER", "root")
	config.ServerCallbackURL = getEnvString(env, "SERVER_CALLBACK_URL", "")

	config.MetricsEnabled = getEnvBool(env, "METRICS_ENABLED", false)
	config.MetricsPort = getEnvString(env, "METRICS_PORT", "2112")
	config.MetricsPassword = getEnvString(env, "METRICS_PASSWORD", "")

	config.LogEnabled = getEnvBool(env, "LOG_ENABLED", true)
	config.LogLevel = getEnvString(env, "LOG_LEVEL", "INFO")
	config.LogDir = getEnvString(env, "LOG_DIR", "/var/log/portage-engine")
	config.LogMaxSizeMB = getEnvInt(env, "LOG_MAX_SIZE_MB", 100)
	config.LogMaxAgeDays = getEnvInt(env, "LOG_MAX_AGE_DAYS", 30)
	config.LogMaxBackups = getEnvInt(env, "LOG_MAX_BACKUPS", 10)
	config.LogEnableConsole = getEnvBool(env, "LOG_ENABLE_CONSOLE", true)
	config.LogEnableFile = getEnvBool(env, "LOG_ENABLE_FILE", true)

	// Parse remote builders
	if builders := getEnvString(env, "REMOTE_BUILDERS", ""); builders != "" {
		config.RemoteBuilders = strings.Split(builders, ",")
		for i := range config.RemoteBuilders {
			config.RemoteBuilders[i] = strings.TrimSpace(config.RemoteBuilders[i])
		}
	}

	return config, nil
}

// LoadDashboardConfig loads dashboard configuration from a file.
func LoadDashboardConfig(path string) (*DashboardConfig, error) {
	// Set defaults
	config := &DashboardConfig{
		Port:           8081,
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    true,
		JWTSecret:      "change-me-in-production",
		AllowAnonymous: true,
	}

	// If config file doesn't exist, return defaults
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Config file not found, using defaults: %s\n", path)
		return config, nil
	}

	env, err := loadEnvFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config.Port = getEnvInt(env, "DASHBOARD_PORT", config.Port)
	config.ServerURL = getEnvString(env, "SERVER_URL", config.ServerURL)
	config.AuthEnabled = getEnvBool(env, "AUTH_ENABLED", config.AuthEnabled)
	config.JWTSecret = getEnvString(env, "JWT_SECRET", config.JWTSecret)
	config.AllowAnonymous = getEnvBool(env, "ALLOW_ANONYMOUS", config.AllowAnonymous)

	config.MetricsEnabled = getEnvBool(env, "METRICS_ENABLED", false)
	config.MetricsPort = getEnvString(env, "METRICS_PORT", "2112")
	config.MetricsPassword = getEnvString(env, "METRICS_PASSWORD", "")

	config.LogEnabled = getEnvBool(env, "LOG_ENABLED", true)
	config.LogLevel = getEnvString(env, "LOG_LEVEL", "INFO")
	config.LogDir = getEnvString(env, "LOG_DIR", "/var/log/portage-engine")
	config.LogMaxSizeMB = getEnvInt(env, "LOG_MAX_SIZE_MB", 100)
	config.LogMaxAgeDays = getEnvInt(env, "LOG_MAX_AGE_DAYS", 30)
	config.LogMaxBackups = getEnvInt(env, "LOG_MAX_BACKUPS", 10)
	config.LogEnableConsole = getEnvBool(env, "LOG_ENABLE_CONSOLE", true)
	config.LogEnableFile = getEnvBool(env, "LOG_ENABLE_FILE", true)

	return config, nil
}

// LoadBuilderConfig loads builder configuration from a file.
func LoadBuilderConfig(path string) (*BuilderConfig, error) {
	// Set defaults
	config := &BuilderConfig{
		Port:               9090,
		Workers:            2,
		UseDocker:          true,
		DockerImage:        "gentoo/stage3:latest",
		WorkDir:            "/var/tmp/portage-builds",
		ArtifactDir:        "/var/tmp/portage-artifacts",
		DataDir:            "/var/lib/portage-engine",
		PersistenceEnabled: true,
		RetentionDays:      7,
		GPGEnabled:         false,
		StorageType:        "local",
		StorageLocalDir:    "/var/binpkgs",
		// Portage defaults
		PortageReposPath: "/var/db/repos",
		PortageConfPath:  "/etc/portage",
		MakeConfPath:     "/etc/portage/make.conf",
	}

	// If config file doesn't exist, return defaults
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Config file not found, using defaults: %s\n", path)
		return config, nil
	}

	env, err := loadEnvFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config.Port = getEnvInt(env, "BUILDER_PORT", config.Port)
	config.Workers = getEnvInt(env, "BUILDER_WORKERS", config.Workers)
	config.InstanceID = getEnvString(env, "INSTANCE_ID", "")
	config.Architecture = getEnvString(env, "ARCHITECTURE", "")
	config.UseDocker = getEnvBool(env, "USE_DOCKER", config.UseDocker)
	config.DockerImage = getEnvString(env, "DOCKER_IMAGE", config.DockerImage)
	config.WorkDir = getEnvString(env, "BUILD_WORK_DIR", config.WorkDir)
	config.ArtifactDir = getEnvString(env, "BUILD_ARTIFACT_DIR", config.ArtifactDir)
	config.DataDir = getEnvString(env, "DATA_DIR", config.DataDir)
	config.PersistenceEnabled = getEnvBool(env, "PERSISTENCE_ENABLED", config.PersistenceEnabled)
	config.RetentionDays = getEnvInt(env, "RETENTION_DAYS", config.RetentionDays)

	config.GPGEnabled = getEnvBool(env, "GPG_ENABLED", config.GPGEnabled)
	config.GPGKeyID = getEnvString(env, "GPG_KEY_ID", "")
	config.GPGKeyPath = getEnvString(env, "GPG_KEY_PATH", "")

	config.StorageType = getEnvString(env, "STORAGE_TYPE", config.StorageType)
	config.StorageLocalDir = getEnvString(env, "STORAGE_LOCAL_DIR", config.StorageLocalDir)
	config.StorageS3Bucket = getEnvString(env, "STORAGE_S3_BUCKET", "")
	config.StorageS3Region = getEnvString(env, "STORAGE_S3_REGION", "")
	config.StorageS3Prefix = getEnvString(env, "STORAGE_S3_PREFIX", "")
	config.StorageHTTPBase = getEnvString(env, "STORAGE_HTTP_BASE", "")

	config.ServerURL = getEnvString(env, "SERVER_URL", "")
	config.NotifyConfig = getEnvString(env, "NOTIFY_CONFIG", "")

	// Portage mirror settings
	config.SyncMirror = getEnvString(env, "SYNC_MIRROR", config.SyncMirror)
	config.DistfilesMirror = getEnvString(env, "DISTFILES_MIRROR", config.DistfilesMirror)

	// Portage path settings
	config.PortageReposPath = getEnvString(env, "PORTAGE_REPOS_PATH", config.PortageReposPath)
	config.PortageConfPath = getEnvString(env, "PORTAGE_CONF_PATH", config.PortageConfPath)
	config.MakeConfPath = getEnvString(env, "MAKE_CONF_PATH", config.MakeConfPath)

	config.MetricsEnabled = getEnvBool(env, "METRICS_ENABLED", false)
	config.MetricsPort = getEnvString(env, "METRICS_PORT", "2112")
	config.MetricsPassword = getEnvString(env, "METRICS_PASSWORD", "")

	config.LogEnabled = getEnvBool(env, "LOG_ENABLED", true)
	config.LogLevel = getEnvString(env, "LOG_LEVEL", "INFO")
	config.LogDir = getEnvString(env, "LOG_DIR", "/var/log/portage-engine")
	config.LogMaxSizeMB = getEnvInt(env, "LOG_MAX_SIZE_MB", 100)
	config.LogMaxAgeDays = getEnvInt(env, "LOG_MAX_AGE_DAYS", 30)
	config.LogMaxBackups = getEnvInt(env, "LOG_MAX_BACKUPS", 10)
	config.LogEnableConsole = getEnvBool(env, "LOG_ENABLE_CONSOLE", true)
	config.LogEnableFile = getEnvBool(env, "LOG_ENABLE_FILE", true)

	return config, nil
}
