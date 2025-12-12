package iac

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNewManager tests creating a new IaC manager.
func TestNewManager(t *testing.T) {
	manager := NewManager()
	if manager == nil {
		t.Fatal("NewManager returned nil")
	}

	if manager.instances == nil {
		t.Error("instances map not initialized")
	}

	if manager.workspaceDir == "" {
		t.Error("workspaceDir should not be empty")
	}
}

func TestGetOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		m            map[string]string
		key          string
		defaultValue string
		want         string
	}{
		{
			name:         "key exists",
			m:            map[string]string{"region": "us-west-1"},
			key:          "region",
			defaultValue: "us-east-1",
			want:         "us-west-1",
		},
		{
			name:         "key missing",
			m:            map[string]string{},
			key:          "region",
			defaultValue: "us-east-1",
			want:         "us-east-1",
		},
		{
			name:         "nil map",
			m:            nil,
			key:          "region",
			defaultValue: "us-east-1",
			want:         "us-east-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getOrDefault(tt.m, tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getOrDefault() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUpdateHeartbeat(t *testing.T) {
	manager := NewManager()

	// Add a test instance
	instanceID := "test-instance"
	manager.instances[instanceID] = &Instance{
		ID:            instanceID,
		Provider:      "gcp",
		Status:        "provisioning",
		LastHeartbeat: time.Now().Add(-10 * time.Minute),
	}

	// Update heartbeat
	err := manager.UpdateHeartbeat(instanceID)
	if err != nil {
		t.Errorf("UpdateHeartbeat() error = %v", err)
	}

	// Verify heartbeat was updated
	instance := manager.instances[instanceID]
	if time.Since(instance.LastHeartbeat) > 5*time.Second {
		t.Error("LastHeartbeat not updated")
	}
	if instance.Status != "running" {
		t.Errorf("Status = %v, want running", instance.Status)
	}

	// Test non-existent instance
	err = manager.UpdateHeartbeat("non-existent")
	if err == nil {
		t.Error("UpdateHeartbeat() expected error for non-existent instance")
	}
}

func TestCheckStaleInstances(t *testing.T) {
	manager := NewManager()

	// Add test instances
	manager.instances["fresh"] = &Instance{
		ID:            "fresh",
		Provider:      "gcp",
		Status:        "running",
		LastHeartbeat: time.Now(),
	}
	manager.instances["stale"] = &Instance{
		ID:            "stale",
		Provider:      "gcp",
		Status:        "running",
		LastHeartbeat: time.Now().Add(-15 * time.Minute),
	}
	manager.instances["already-error"] = &Instance{
		ID:            "already-error",
		Provider:      "gcp",
		Status:        "error",
		LastHeartbeat: time.Now().Add(-20 * time.Minute),
	}

	// Check stale instances - should return all instances with LastHeartbeat > timeout
	stale := manager.CheckStaleInstances(10 * time.Minute)
	if len(stale) != 2 {
		t.Errorf("CheckStaleInstances() returned %d instances, want 2", len(stale))
	}

	// Verify the returned instances are the stale ones
	staleIDs := make(map[string]bool)
	for _, inst := range stale {
		staleIDs[inst.ID] = true
	}
	if !staleIDs["stale"] || !staleIDs["already-error"] {
		t.Error("CheckStaleInstances() did not return expected stale instances")
	}
}

func TestListInstances(t *testing.T) {
	manager := NewManager()

	// Test empty list
	instances := manager.ListInstances()
	if len(instances) != 0 {
		t.Errorf("ListInstances() returned %d instances, want 0", len(instances))
	}

	// Add test instances
	manager.instances["test1"] = &Instance{
		ID:       "test1",
		Provider: "gcp",
		Status:   "running",
	}
	manager.instances["test2"] = &Instance{
		ID:       "test2",
		Provider: "aws",
		Status:   "provisioning",
	}

	instances = manager.ListInstances()
	if len(instances) != 2 {
		t.Errorf("ListInstances() returned %d instances, want 2", len(instances))
	}
}

func TestGenerateFirewallConfig(t *testing.T) {
	manager := NewManager()

	tests := []struct {
		name string
		req  *ProvisionRequest
	}{
		{
			name: "aliyun firewall",
			req: &ProvisionRequest{
				Provider:        "aliyun",
				AllowedIPRanges: []string{"0.0.0.0/0"},
				BuilderPort:     8080,
			},
		},
		{
			name: "gcp firewall",
			req: &ProvisionRequest{
				Provider:        "gcp",
				AllowedIPRanges: []string{"0.0.0.0/0"},
				BuilderPort:     8080,
			},
		},
		{
			name: "aws firewall",
			req: &ProvisionRequest{
				Provider:        "aws",
				AllowedIPRanges: []string{"0.0.0.0/0"},
				BuilderPort:     8080,
			},
		},
		{
			name: "no ip ranges",
			req: &ProvisionRequest{
				Provider:    "gcp",
				BuilderPort: 8080,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := manager.generateFirewallConfig(tt.req)
			if len(tt.req.AllowedIPRanges) > 0 && len(config) == 0 {
				t.Error("generateFirewallConfig() returned empty config when firewall rules configured")
			}
		})
	}
}

func TestGenerateTerraformConfig(t *testing.T) {
	manager := NewManager()

	tests := []struct {
		name      string
		req       *ProvisionRequest
		wantEmpty bool
	}{
		{
			name: "aliyun config",
			req: &ProvisionRequest{
				Arch:     "amd64",
				Provider: "aliyun",
				Spec: map[string]string{
					"region": "cn-hangzhou",
				},
				Credentials: &CloudCredentials{
					AliyunAccessKey: "test-ak",
					AliyunSecretKey: "test-sk",
				},
			},
			wantEmpty: false,
		},
		{
			name: "gcp config",
			req: &ProvisionRequest{
				Arch:     "amd64",
				Provider: "gcp",
				Spec: map[string]string{
					"region":  "us-central1",
					"project": "test-project",
				},
				Credentials: &CloudCredentials{
					GCPKeyFile: "/path/to/key.json",
				},
			},
			wantEmpty: false,
		},
		{
			name: "aws config",
			req: &ProvisionRequest{
				Arch:     "arm64",
				Provider: "aws",
				Spec: map[string]string{
					"region": "us-west-2",
				},
				Credentials: &CloudCredentials{
					AWSAccessKey: "test-access",
					AWSSecretKey: "test-secret",
				},
			},
			wantEmpty: false,
		},
		{
			name: "invalid provider",
			req: &ProvisionRequest{
				Provider: "invalid",
			},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := manager.generateTerraformConfig(tt.req)
			if !tt.wantEmpty && len(config) == 0 {
				t.Error("generateTerraformConfig() returned empty config")
			}
			if tt.wantEmpty && len(config) > 0 {
				t.Error("generateTerraformConfig() should return empty config for invalid provider")
			}
		})
	}
}

func TestGenerateDeploymentScript(t *testing.T) {
	manager := NewManager()

	script := manager.generateDeploymentScript("http://localhost:8080/api/heartbeat", 9090)

	if len(script) == 0 {
		t.Error("generateDeploymentScript() returned empty script")
	}
}

func TestPrepareEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		req     *ProvisionRequest
		wantEnv []string
	}{
		{
			name: "aliyun credentials",
			req: &ProvisionRequest{
				Provider: "aliyun",
				Credentials: &CloudCredentials{
					AliyunAccessKey: "test-ak",
					AliyunSecretKey: "test-sk",
				},
			},
			wantEnv: []string{
				"ALICLOUD_ACCESS_KEY=test-ak",
				"ALICLOUD_SECRET_KEY=test-sk",
			},
		},
		{
			name: "gcp credentials",
			req: &ProvisionRequest{
				Provider: "gcp",
				Credentials: &CloudCredentials{
					GCPKeyFile: "/path/to/key.json",
				},
			},
			wantEnv: []string{
				"GOOGLE_APPLICATION_CREDENTIALS=/path/to/key.json",
			},
		},
		{
			name: "aws credentials",
			req: &ProvisionRequest{
				Provider: "aws",
				Credentials: &CloudCredentials{
					AWSAccessKey: "test-access",
					AWSSecretKey: "test-secret",
				},
			},
			wantEnv: []string{
				"AWS_ACCESS_KEY_ID=test-access",
				"AWS_SECRET_ACCESS_KEY=test-secret",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager()

			gotEnv := manager.prepareEnvironment(tt.req)
			if len(gotEnv) != len(tt.wantEnv) {
				t.Errorf("prepareEnvironment() returned %d env vars, want %d", len(gotEnv), len(tt.wantEnv))
			}

			// Convert to map for easier comparison
			envMap := make(map[string]bool)
			for _, e := range gotEnv {
				envMap[e] = true
			}

			for _, want := range tt.wantEnv {
				if !envMap[want] {
					t.Errorf("prepareEnvironment() missing env var %s", want)
				}
			}
		})
	}
}

func TestProvisionWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	manager := NewManager()

	req := &ProvisionRequest{
		Arch:     "amd64",
		Provider: "gcp",
		Spec: map[string]string{
			"region":  "us-central1",
			"project": "test-project",
		},
		Credentials: &CloudCredentials{
			GCPKeyFile: "/path/to/key.json",
		},
		SSH: &SSHConfig{
			KeyPath: "/path/to/ssh/key",
			User:    "root",
		},
		ServerCallback: "http://localhost:8080/api/heartbeat",
	}

	// Test config generation
	config := manager.generateTerraformConfig(req)
	if len(config) == 0 {
		t.Error("Generated config is empty")
	}

	// Verify workspace directory creation
	workspaceDir := manager.workspaceDir
	configPath := filepath.Join(workspaceDir, "test-workspace", "main.tf")

	// Create the config file to simulate what Provision() does
	testWorkspace := filepath.Join(workspaceDir, "test-workspace")
	if err := os.MkdirAll(testWorkspace, 0755); err == nil {
		if err := os.WriteFile(configPath, []byte(config), 0644); err == nil {
			// Verify file was created
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				t.Error("main.tf was not created")
			}
			// Clean up
			_ = os.RemoveAll(testWorkspace)
		}
	}
}

func TestInstanceLifecycle(t *testing.T) {
	manager := NewManager()

	instanceID := "test-instance-lifecycle"

	// Create instance
	manager.instances[instanceID] = &Instance{
		ID:              instanceID,
		Provider:        "gcp",
		Arch:            "amd64",
		Status:          "provisioning",
		LastHeartbeat:   time.Now(),
		IPAddress:       "192.168.1.100",
		BuilderEndpoint: "http://192.168.1.100:8080",
	}

	// Verify instance exists
	instances := manager.ListInstances()
	found := false
	for _, inst := range instances {
		if inst.ID == instanceID {
			found = true
			break
		}
	}
	if !found {
		t.Error("Instance not found in list")
	}

	// Update heartbeat
	if err := manager.UpdateHeartbeat(instanceID); err != nil {
		t.Errorf("UpdateHeartbeat() error = %v", err)
	}

	// Check instance is not stale
	stale := manager.CheckStaleInstances(10 * time.Minute)
	for _, inst := range stale {
		if inst.ID == instanceID {
			t.Error("Instance should not be stale")
		}
	}
}
