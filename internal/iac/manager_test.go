package iac

import (
	"testing"
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

// TestProvision tests provisioning an instance.
func TestProvision(t *testing.T) {
	manager := NewManager()

	req := &ProvisionRequest{
		Provider: "aws",
		Arch:     "amd64",
		Spec: map[string]string{
			"instance_type": "t3.medium",
			"region":        "us-east-1",
		},
	}

	instance, err := manager.Provision(req)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	if instance == nil {
		t.Fatal("Expected non-nil instance")
	}

	if instance.ID == "" {
		t.Error("Instance ID should not be empty")
	}

	if instance.Provider != "aws" {
		t.Errorf("Expected provider aws, got %s", instance.Provider)
	}

	if instance.Status != "running" {
		t.Errorf("Expected status running, got %s", instance.Status)
	}

	if instance.IPAddress == "" {
		t.Error("Instance IP address should not be empty")
	}
}

// TestTerminate tests terminating an instance.
func TestTerminate(t *testing.T) {
	manager := NewManager()

	// Provision an instance first
	req := &ProvisionRequest{
		Provider: "gcp",
		Arch:     "arm64",
		Spec: map[string]string{
			"machine_type": "e2-standard-2",
		},
	}

	instance, err := manager.Provision(req)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	// Terminate the instance
	err = manager.Terminate(instance.ID)
	if err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}

	// Verify instance is removed
	_, err = manager.GetInstance(instance.ID)
	if err == nil {
		t.Error("Expected error when getting terminated instance")
	}
}

// TestGetInstance tests retrieving an instance.
func TestGetInstance(t *testing.T) {
	manager := NewManager()

	req := &ProvisionRequest{
		Provider: "azure",
		Arch:     "amd64",
	}

	instance, err := manager.Provision(req)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	retrieved, err := manager.GetInstance(instance.ID)
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	if retrieved.ID != instance.ID {
		t.Errorf("Expected instance ID %s, got %s", instance.ID, retrieved.ID)
	}
}

// TestGetInstanceNotFound tests retrieving non-existent instance.
func TestGetInstanceNotFound(t *testing.T) {
	manager := NewManager()

	_, err := manager.GetInstance("non-existent-id")
	if err == nil {
		t.Error("Expected error for non-existent instance")
	}
}

// TestListInstances tests listing all instances.
func TestListInstances(t *testing.T) {
	manager := NewManager()

	// Provision multiple instances
	providers := []string{"aws", "gcp", "azure"}
	for _, provider := range providers {
		req := &ProvisionRequest{
			Provider: provider,
			Arch:     "amd64",
		}
		_, err := manager.Provision(req)
		if err != nil {
			t.Fatalf("Provision failed: %v", err)
		}
	}

	instances := manager.ListInstances()
	if len(instances) != 3 {
		t.Errorf("Expected 3 instances, got %d", len(instances))
	}
}

// TestProvisionWithDifferentArchs tests provisioning with different architectures.
func TestProvisionWithDifferentArchs(t *testing.T) {
	manager := NewManager()

	archs := []string{"amd64", "arm64"}
	for _, arch := range archs {
		req := &ProvisionRequest{
			Provider: "aws",
			Arch:     arch,
		}

		instance, err := manager.Provision(req)
		if err != nil {
			t.Fatalf("Provision failed for arch %s: %v", arch, err)
		}

		if instance.Arch != arch {
			t.Errorf("Expected arch %s, got %s", arch, instance.Arch)
		}
	}
}
