package config

import (
	"os"
	"testing"
)

// TestLoadServerConfig tests loading server configuration.
func TestLoadServerConfig(t *testing.T) {
	tmpFile := "/tmp/test-server.conf"
	configData := `# Test server config
SERVER_PORT=9999
BINPKG_PATH=/test/binpkgs
MAX_WORKERS=10
BUILD_MODE=cloud
STORAGE_TYPE=s3
STORAGE_S3_BUCKET=test-bucket
GPG_ENABLED=true
GPG_KEY_ID=ABCD1234
CLOUD_DEFAULT_PROVIDER=aws
REMOTE_BUILDERS=http://builder1:9090,http://builder2:9090
`

	if err := os.WriteFile(tmpFile, []byte(configData), 0600); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	cfg, err := LoadServerConfig(tmpFile)
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}

	if cfg.Port != 9999 {
		t.Errorf("Expected Port=9999, got %d", cfg.Port)
	}

	if cfg.BinpkgPath != "/test/binpkgs" {
		t.Errorf("Expected BinpkgPath=/test/binpkgs, got %s", cfg.BinpkgPath)
	}

	if cfg.MaxWorkers != 10 {
		t.Errorf("Expected MaxWorkers=10, got %d", cfg.MaxWorkers)
	}

	if cfg.BuildMode != "cloud" {
		t.Errorf("Expected BuildMode=cloud, got %s", cfg.BuildMode)
	}

	if cfg.StorageType != "s3" {
		t.Errorf("Expected StorageType=s3, got %s", cfg.StorageType)
	}

	if cfg.StorageS3Bucket != "test-bucket" {
		t.Errorf("Expected StorageS3Bucket=test-bucket, got %s", cfg.StorageS3Bucket)
	}

	if !cfg.GPGEnabled {
		t.Error("Expected GPGEnabled=true, got false")
	}

	if cfg.GPGKeyID != "ABCD1234" {
		t.Errorf("Expected GPGKeyID=ABCD1234, got %s", cfg.GPGKeyID)
	}

	if cfg.CloudProvider != "aws" {
		t.Errorf("Expected CloudProvider=aws, got %s", cfg.CloudProvider)
	}

	if len(cfg.RemoteBuilders) != 2 {
		t.Errorf("Expected 2 remote builders, got %d", len(cfg.RemoteBuilders))
	}

	if cfg.RemoteBuilders[0] != "http://builder1:9090" {
		t.Errorf("Expected first builder=http://builder1:9090, got %s", cfg.RemoteBuilders[0])
	}
}

// TestLoadDashboardConfig tests loading dashboard configuration.
func TestLoadDashboardConfig(t *testing.T) {
	tmpFile := "/tmp/test-dashboard.conf"
	configData := `# Test dashboard config
DASHBOARD_PORT=7777
SERVER_URL=http://test-server:8080
AUTH_ENABLED=false
JWT_SECRET=test-secret
ALLOW_ANONYMOUS=false
`

	if err := os.WriteFile(tmpFile, []byte(configData), 0600); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	cfg, err := LoadDashboardConfig(tmpFile)
	if err != nil {
		t.Fatalf("LoadDashboardConfig failed: %v", err)
	}

	if cfg.Port != 7777 {
		t.Errorf("Expected Port=7777, got %d", cfg.Port)
	}

	if cfg.ServerURL != "http://test-server:8080" {
		t.Errorf("Expected ServerURL=http://test-server:8080, got %s", cfg.ServerURL)
	}

	if cfg.AuthEnabled {
		t.Error("Expected AuthEnabled=false, got true")
	}

	if cfg.JWTSecret != "test-secret" {
		t.Errorf("Expected JWTSecret=test-secret, got %s", cfg.JWTSecret)
	}

	if cfg.AllowAnonymous {
		t.Error("Expected AllowAnonymous=false, got true")
	}
}

// TestLoadBuilderConfig tests loading builder configuration.
func TestLoadBuilderConfig(t *testing.T) {
	tmpFile := "/tmp/test-builder.conf"
	configData := `# Test builder config
BUILDER_PORT=6666
BUILDER_WORKERS=8
USE_DOCKER=false
DOCKER_IMAGE=custom/gentoo:test
BUILD_WORK_DIR=/custom/work
BUILD_ARTIFACT_DIR=/custom/artifacts
GPG_ENABLED=true
GPG_KEY_ID=TEST1234
STORAGE_TYPE=http
STORAGE_HTTP_BASE=https://storage.test.com
NOTIFY_CONFIG=/path/to/notify.json
`

	if err := os.WriteFile(tmpFile, []byte(configData), 0600); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	cfg, err := LoadBuilderConfig(tmpFile)
	if err != nil {
		t.Fatalf("LoadBuilderConfig failed: %v", err)
	}

	if cfg.Port != 6666 {
		t.Errorf("Expected Port=6666, got %d", cfg.Port)
	}

	if cfg.Workers != 8 {
		t.Errorf("Expected Workers=8, got %d", cfg.Workers)
	}

	if cfg.UseDocker {
		t.Error("Expected UseDocker=false, got true")
	}

	if cfg.DockerImage != "custom/gentoo:test" {
		t.Errorf("Expected DockerImage=custom/gentoo:test, got %s", cfg.DockerImage)
	}

	if cfg.WorkDir != "/custom/work" {
		t.Errorf("Expected WorkDir=/custom/work, got %s", cfg.WorkDir)
	}

	if cfg.ArtifactDir != "/custom/artifacts" {
		t.Errorf("Expected ArtifactDir=/custom/artifacts, got %s", cfg.ArtifactDir)
	}

	if !cfg.GPGEnabled {
		t.Error("Expected GPGEnabled=true, got false")
	}

	if cfg.GPGKeyID != "TEST1234" {
		t.Errorf("Expected GPGKeyID=TEST1234, got %s", cfg.GPGKeyID)
	}

	if cfg.StorageType != "http" {
		t.Errorf("Expected StorageType=http, got %s", cfg.StorageType)
	}

	if cfg.StorageHTTPBase != "https://storage.test.com" {
		t.Errorf("Expected StorageHTTPBase=https://storage.test.com, got %s", cfg.StorageHTTPBase)
	}

	if cfg.NotifyConfig != "/path/to/notify.json" {
		t.Errorf("Expected NotifyConfig=/path/to/notify.json, got %s", cfg.NotifyConfig)
	}
}

// TestLoadConfigDefaults tests that default values are used when config file doesn't exist.
func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadServerConfig("/nonexistent/config.conf")
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("Expected default Port=8080, got %d", cfg.Port)
	}

	if cfg.MaxWorkers != 5 {
		t.Errorf("Expected default MaxWorkers=5, got %d", cfg.MaxWorkers)
	}
}

// TestLoadEnvFile tests the env file parsing.
func TestLoadEnvFile(t *testing.T) {
	tmpFile := "/tmp/test-env.conf"
	configData := `# Comment line
KEY1=value1

KEY2=value2

# Another comment
KEY3=value with spaces
EMPTY_KEY=
`

	if err := os.WriteFile(tmpFile, []byte(configData), 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	env, err := loadEnvFile(tmpFile)
	if err != nil {
		t.Fatalf("loadEnvFile failed: %v", err)
	}

	if env["KEY1"] != "value1" {
		t.Errorf("Expected KEY1=value1, got %s", env["KEY1"])
	}

	if env["KEY2"] != "value2" {
		t.Errorf("Expected KEY2=value2, got %s", env["KEY2"])
	}

	if env["KEY3"] != "value with spaces" {
		t.Errorf("Expected KEY3='value with spaces', got %s", env["KEY3"])
	}

	if env["EMPTY_KEY"] != "" {
		t.Errorf("Expected EMPTY_KEY='', got %s", env["EMPTY_KEY"])
	}
}
