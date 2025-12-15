// Package builder provides build execution capabilities.
package builder

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ContainerRuntime defines the interface for container operations.
type ContainerRuntime interface {
	// Name returns the runtime name (docker or podman).
	Name() string
	// Run executes a container with the given arguments.
	Run(ctx context.Context, args []string) ([]byte, error)
	// Create creates a container with the given arguments.
	Create(ctx context.Context, args []string) error
	// Start starts a container by name.
	Start(ctx context.Context, containerName string) error
	// Stop stops a container by name.
	Stop(ctx context.Context, containerName string) error
	// Remove removes a container by name.
	Remove(ctx context.Context, containerName string) error
	// Exec executes a command in a running container.
	Exec(ctx context.Context, containerName string, cmd []string) ([]byte, error)
	// Copy copies files between host and container.
	Copy(ctx context.Context, src, dst string) error
	// IsAvailable checks if the runtime is available.
	IsAvailable() bool
}

// DockerRuntime implements ContainerRuntime for Docker.
type DockerRuntime struct {
	executable string
}

// NewDockerRuntime creates a new Docker runtime.
func NewDockerRuntime() *DockerRuntime {
	return &DockerRuntime{
		executable: "docker",
	}
}

// Name returns the runtime name.
func (d *DockerRuntime) Name() string {
	return "docker"
}

// Run executes a container with the given arguments.
func (d *DockerRuntime) Run(ctx context.Context, args []string) ([]byte, error) {
	cmdArgs := append([]string{"run"}, args...)
	cmd := exec.CommandContext(ctx, d.executable, cmdArgs...)
	return cmd.CombinedOutput()
}

// Create creates a container with the given arguments.
func (d *DockerRuntime) Create(ctx context.Context, args []string) error {
	cmdArgs := append([]string{"create"}, args...)
	cmd := exec.CommandContext(ctx, d.executable, cmdArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

// Start starts a container by name.
func (d *DockerRuntime) Start(ctx context.Context, containerName string) error {
	cmd := exec.CommandContext(ctx, d.executable, "start", containerName)
	return cmd.Run()
}

// Stop stops a container by name.
func (d *DockerRuntime) Stop(ctx context.Context, containerName string) error {
	cmd := exec.CommandContext(ctx, d.executable, "stop", containerName)
	return cmd.Run()
}

// Remove removes a container by name.
func (d *DockerRuntime) Remove(ctx context.Context, containerName string) error {
	cmd := exec.CommandContext(ctx, d.executable, "rm", containerName)
	return cmd.Run()
}

// Exec executes a command in a running container.
func (d *DockerRuntime) Exec(ctx context.Context, containerName string, cmdSlice []string) ([]byte, error) {
	args := append([]string{"exec", containerName}, cmdSlice...)
	cmd := exec.CommandContext(ctx, d.executable, args...)
	return cmd.CombinedOutput()
}

// Copy copies files between host and container.
func (d *DockerRuntime) Copy(ctx context.Context, src, dst string) error {
	cmd := exec.CommandContext(ctx, d.executable, "cp", src, dst)
	return cmd.Run()
}

// IsAvailable checks if Docker is available.
func (d *DockerRuntime) IsAvailable() bool {
	cmd := exec.Command(d.executable, "version")
	return cmd.Run() == nil
}

// PodmanRuntime implements ContainerRuntime for Podman.
type PodmanRuntime struct {
	executable string
}

// NewPodmanRuntime creates a new Podman runtime.
func NewPodmanRuntime() *PodmanRuntime {
	return &PodmanRuntime{
		executable: "podman",
	}
}

// Name returns the runtime name.
func (p *PodmanRuntime) Name() string {
	return "podman"
}

// Run executes a container with the given arguments.
func (p *PodmanRuntime) Run(ctx context.Context, args []string) ([]byte, error) {
	cmdArgs := append([]string{"run"}, args...)
	cmd := exec.CommandContext(ctx, p.executable, cmdArgs...)
	return cmd.CombinedOutput()
}

// Create creates a container with the given arguments.
func (p *PodmanRuntime) Create(ctx context.Context, args []string) error {
	cmdArgs := append([]string{"create"}, args...)
	cmd := exec.CommandContext(ctx, p.executable, cmdArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

// Start starts a container by name.
func (p *PodmanRuntime) Start(ctx context.Context, containerName string) error {
	cmd := exec.CommandContext(ctx, p.executable, "start", containerName)
	return cmd.Run()
}

// Stop stops a container by name.
func (p *PodmanRuntime) Stop(ctx context.Context, containerName string) error {
	cmd := exec.CommandContext(ctx, p.executable, "stop", containerName)
	return cmd.Run()
}

// Remove removes a container by name.
func (p *PodmanRuntime) Remove(ctx context.Context, containerName string) error {
	cmd := exec.CommandContext(ctx, p.executable, "rm", containerName)
	return cmd.Run()
}

// Exec executes a command in a running container.
func (p *PodmanRuntime) Exec(ctx context.Context, containerName string, cmdSlice []string) ([]byte, error) {
	args := append([]string{"exec", containerName}, cmdSlice...)
	cmd := exec.CommandContext(ctx, p.executable, args...)
	return cmd.CombinedOutput()
}

// Copy copies files between host and container.
func (p *PodmanRuntime) Copy(ctx context.Context, src, dst string) error {
	cmd := exec.CommandContext(ctx, p.executable, "cp", src, dst)
	return cmd.Run()
}

// IsAvailable checks if Podman is available.
func (p *PodmanRuntime) IsAvailable() bool {
	cmd := exec.Command(p.executable, "version")
	return cmd.Run() == nil
}

// NewContainerRuntime creates a ContainerRuntime based on the runtime name.
func NewContainerRuntime(runtimeName string) ContainerRuntime {
	runtimeName = strings.ToLower(runtimeName)
	switch runtimeName {
	case "podman":
		return NewPodmanRuntime()
	case "docker", "":
		return NewDockerRuntime()
	default:
		// Default to docker
		return NewDockerRuntime()
	}
}
