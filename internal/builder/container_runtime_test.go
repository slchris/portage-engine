package builder

import (
	"context"
	"testing"
)

func TestNewContainerRuntime(t *testing.T) {
	tests := []struct {
		name        string
		runtimeName string
		wantName    string
	}{
		{
			name:        "docker explicit",
			runtimeName: "docker",
			wantName:    "docker",
		},
		{
			name:        "podman",
			runtimeName: "podman",
			wantName:    "podman",
		},
		{
			name:        "empty defaults to docker",
			runtimeName: "",
			wantName:    "docker",
		},
		{
			name:        "unknown defaults to docker",
			runtimeName: "unknown",
			wantName:    "docker",
		},
		{
			name:        "Docker uppercase",
			runtimeName: "Docker",
			wantName:    "docker",
		},
		{
			name:        "PODMAN uppercase",
			runtimeName: "PODMAN",
			wantName:    "podman",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := NewContainerRuntime(tt.runtimeName)
			if runtime.Name() != tt.wantName {
				t.Errorf("NewContainerRuntime(%q).Name() = %q, want %q",
					tt.runtimeName, runtime.Name(), tt.wantName)
			}
		})
	}
}

func TestDockerRuntime(t *testing.T) {
	runtime := NewDockerRuntime()

	if runtime.Name() != "docker" {
		t.Errorf("Name() = %q, want %q", runtime.Name(), "docker")
	}

	if runtime.executable != "docker" {
		t.Errorf("executable = %q, want %q", runtime.executable, "docker")
	}
}

func TestPodmanRuntime(t *testing.T) {
	runtime := NewPodmanRuntime()

	if runtime.Name() != "podman" {
		t.Errorf("Name() = %q, want %q", runtime.Name(), "podman")
	}

	if runtime.executable != "podman" {
		t.Errorf("executable = %q, want %q", runtime.executable, "podman")
	}
}

func TestContainerRuntimeInterface(_ *testing.T) {
	// Test that both runtimes implement the interface
	var _ ContainerRuntime = &DockerRuntime{}
	var _ ContainerRuntime = &PodmanRuntime{}
}

func TestDockerRuntimeMethods(t *testing.T) {
	runtime := NewDockerRuntime()
	ctx := context.Background()

	// Test Stop with non-existent container (should fail gracefully)
	err := runtime.Stop(ctx, "nonexistent-container-12345")
	if err == nil {
		t.Log("Stop returned nil for non-existent container (expected on some systems)")
	}

	// Test Remove with non-existent container
	err = runtime.Remove(ctx, "nonexistent-container-12345")
	if err == nil {
		t.Log("Remove returned nil for non-existent container (expected on some systems)")
	}
}

func TestPodmanRuntimeMethods(t *testing.T) {
	runtime := NewPodmanRuntime()
	ctx := context.Background()

	// Test Stop with non-existent container (should fail gracefully)
	err := runtime.Stop(ctx, "nonexistent-container-12345")
	if err == nil {
		t.Log("Stop returned nil for non-existent container (expected on some systems)")
	}

	// Test Remove with non-existent container
	err = runtime.Remove(ctx, "nonexistent-container-12345")
	if err == nil {
		t.Log("Remove returned nil for non-existent container (expected on some systems)")
	}
}
