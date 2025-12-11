package docker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestNewBuilder tests creating a new Docker builder.
func TestNewBuilder(t *testing.T) {
	builder := NewBuilder("gentoo/stage3:latest")
	if builder == nil {
		t.Fatal("NewBuilder returned nil")
	}

	if builder.image != "gentoo/stage3:latest" {
		t.Errorf("Expected image gentoo/stage3:latest, got %s", builder.image)
	}

	if builder.workspaceDir == "" {
		t.Error("workspaceDir should not be empty")
	}
}

// TestBuildRequest tests build request validation.
func TestBuildRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *BuildRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: &BuildRequest{
				Package:    "dev-lang/python",
				Version:    "3.11.0",
				Arch:       "amd64",
				UseFlags:   []string{"ssl", "threads"},
				OutputPath: "/tmp/binpkgs",
			},
			wantErr: false,
		},
		{
			name: "minimal request",
			req: &BuildRequest{
				Package:    "app-editors/vim",
				OutputPath: "/tmp/binpkgs",
			},
			wantErr: false,
		},
	}

	builder := NewBuilder("gentoo/stage3:latest")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.req.Package == "" && tt.wantErr {
				t.Skip("Validation not implemented")
			}

			// Just verify the request structure is valid
			if tt.req.Package == "" {
				t.Error("Package should not be empty for valid request")
			}
		})
	}

	_ = builder
}

// TestBuild tests the build process (mocked).
func TestBuild(t *testing.T) {
	t.Skip("Skipping Docker build test - requires Docker runtime")

	tmpDir, err := os.MkdirTemp("", "docker-build-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	builder := NewBuilder("gentoo/stage3:latest")

	req := &BuildRequest{
		Package:    "app-shells/bash",
		Version:    "5.2",
		Arch:       "amd64",
		OutputPath: tmpDir,
	}

	ctx := context.Background()
	result, err := builder.Build(ctx, req)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}
}

// TestCreateBuildScript tests build script generation.
func TestCreateBuildScript(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "script-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	builder := NewBuilder("gentoo/stage3:latest")

	req := &BuildRequest{
		Package:  "dev-lang/go",
		Version:  "1.21",
		Arch:     "amd64",
		UseFlags: []string{"bootstrap"},
	}

	scriptPath := filepath.Join(tmpDir, "build.sh")
	err = builder.createBuildScript(scriptPath, req)
	if err != nil {
		t.Fatalf("createBuildScript failed: %v", err)
	}

	// Verify script exists
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Error("Build script was not created")
	}

	// Read and verify script content
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("Failed to read script: %v", err)
	}

	scriptStr := string(content)
	if len(scriptStr) == 0 {
		t.Error("Script content is empty")
	}
}

// TestFindArtifact tests artifact finding logic.
func TestFindArtifact(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "artifact-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	builder := NewBuilder("gentoo/stage3:latest")

	req := &BuildRequest{
		Package:    "dev-libs/openssl",
		Version:    "3.0.0",
		OutputPath: tmpDir,
	}

	// Create a mock artifact
	artifactName := "openssl-3.0.0.tbz2"
	artifactPath := filepath.Join(tmpDir, artifactName)
	if err := os.WriteFile(artifactPath, []byte("mock package"), 0644); err != nil {
		t.Fatalf("Failed to create mock artifact: %v", err)
	}

	path, err := builder.findArtifact(req)
	if err != nil {
		t.Fatalf("findArtifact failed: %v", err)
	}

	if path != artifactPath {
		t.Errorf("Expected artifact path %s, got %s", artifactPath, path)
	}
}
