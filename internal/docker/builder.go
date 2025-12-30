// Package docker provides Docker-based local build functionality.
package docker

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Builder manages local Docker-based builds.
type Builder struct {
	image        string
	workspaceDir string
}

// BuildRequest represents a local Docker build request.
type BuildRequest struct {
	Package    string
	Version    string
	Arch       string
	UseFlags   []string
	OutputPath string
}

// BuildResult represents the result of a build.
type BuildResult struct {
	Success      bool
	ArtifactPath string
	BuildLog     string
	Error        error
}

// NewBuilder creates a new Docker builder.
func NewBuilder(image string) *Builder {
	workspaceDir := filepath.Join(os.TempDir(), "portage-docker-builds")
	_ = os.MkdirAll(workspaceDir, 0750)

	return &Builder{
		image:        image,
		workspaceDir: workspaceDir,
	}
}

// Build performs a build using Docker.
func (b *Builder) Build(ctx context.Context, req *BuildRequest) (*BuildResult, error) {
	buildID := fmt.Sprintf("%s-%s-%d", strings.ReplaceAll(req.Package, "/", "-"), req.Arch, time.Now().Unix())
	buildDir := filepath.Join(b.workspaceDir, buildID)

	if err := os.MkdirAll(buildDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create build directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(buildDir) }()

	scriptPath := filepath.Join(buildDir, "build.sh")
	if err := b.createBuildScript(scriptPath, req); err != nil {
		return nil, fmt.Errorf("failed to create build script: %w", err)
	}

	log.Printf("Starting Docker build for %s-%s on %s", req.Package, req.Version, req.Arch)

	containerName := fmt.Sprintf("portage-build-%d", time.Now().UnixNano())

	args := []string{
		"run",
		"--rm",
		"--name", containerName,
		"-v", fmt.Sprintf("%s:/build", buildDir),
		"-v", fmt.Sprintf("%s:/binpkgs", req.OutputPath),
		"--privileged",
		b.image,
		"/bin/bash", "/build/build.sh",
	}

	cmd := exec.CommandContext(ctx, "docker", args...)

	var output strings.Builder
	cmd.Stdout = io.MultiWriter(os.Stdout, &output)
	cmd.Stderr = io.MultiWriter(os.Stderr, &output)

	err := cmd.Run()
	buildLog := output.String()

	if err != nil {
		return &BuildResult{
			Success:  false,
			BuildLog: buildLog,
			Error:    err,
		}, nil
	}

	artifactPath, err := b.findArtifact(req)
	if err != nil {
		return &BuildResult{
			Success:  false,
			BuildLog: buildLog,
			Error:    fmt.Errorf("build succeeded but artifact not found: %w", err),
		}, nil
	}

	return &BuildResult{
		Success:      true,
		ArtifactPath: artifactPath,
		BuildLog:     buildLog,
	}, nil
}

// createBuildScript creates the build script for Docker.
func (b *Builder) createBuildScript(path string, req *BuildRequest) error {
	useFlagsStr := strings.Join(req.UseFlags, " ")

	script := fmt.Sprintf(`#!/bin/bash
set -e

echo "Starting build for %s-%s"
echo "USE flags: %s"

# Simple simulation for now (replace with actual emerge)
sleep 3
echo "Build completed"

# Create a dummy artifact for testing
mkdir -p /binpkgs
echo "Binary package content" > /binpkgs/%s-%s-%s.tbz2
`, req.Package, req.Version, useFlagsStr, strings.ReplaceAll(req.Package, "/", "-"), req.Version, req.Arch)

	if err := os.WriteFile(path, []byte(script), 0600); err != nil {
		return err
	}
	// Make script executable
	return os.Chmod(path, 0700) //nolint:gosec // Script needs exec
}

// findArtifact finds the built package artifact.
func (b *Builder) findArtifact(req *BuildRequest) (string, error) {
	pattern := filepath.Join(req.OutputPath, "*.tbz2")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no artifacts found")
	}

	var latestFile string
	var latestTime time.Time

	for _, file := range matches {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestFile = file
		}
	}

	if latestFile == "" {
		return "", fmt.Errorf("no valid artifacts found")
	}

	return latestFile, nil
}

// CheckDocker checks if Docker is available.
func CheckDocker() error {
	cmd := exec.Command("docker", "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker is not available: %w", err)
	}
	return nil
}

// PullImage pulls the Docker image if not present.
func (b *Builder) PullImage(ctx context.Context) error {
	log.Printf("Pulling Docker image: %s", b.image)
	cmd := exec.CommandContext(ctx, "docker", "pull", b.image)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
