// Package builder provides build execution capabilities.
package builder

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// BuildExecutor handles the actual package build process.
type BuildExecutor struct {
	workDir        string
	artifactDir    string
	configTransfer *ConfigTransfer
}

// NewBuildExecutor creates a new build executor.
func NewBuildExecutor(workDir, artifactDir string) *BuildExecutor {
	return &BuildExecutor{
		workDir:        workDir,
		artifactDir:    artifactDir,
		configTransfer: NewConfigTransfer(workDir),
	}
}

// ExecuteBuild executes a build with the given configuration bundle.
func (be *BuildExecutor) ExecuteBuild(
	ctx context.Context,
	bundle *ConfigBundle,
	job *BuildJob,
) error {
	// Create build workspace
	buildID := job.ID
	buildWorkDir := filepath.Join(be.workDir, buildID)
	if err := os.MkdirAll(buildWorkDir, 0755); err != nil {
		return fmt.Errorf("failed to create build workspace: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(buildWorkDir)
	}()

	// Apply configuration to build environment
	if err := be.configTransfer.ApplyConfigToSystem(bundle, buildWorkDir); err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	// Build each package
	for _, pkg := range bundle.Packages.Packages {
		if err := be.buildPackage(ctx, pkg, bundle, buildWorkDir, job); err != nil {
			return fmt.Errorf("failed to build package %s: %w", pkg.Atom, err)
		}
	}

	return nil
}

// buildPackage builds a single package.
func (be *BuildExecutor) buildPackage(
	ctx context.Context,
	pkg PackageSpec,
	bundle *ConfigBundle,
	buildWorkDir string,
	job *BuildJob,
) error {
	// Construct emerge command
	cmd := be.constructEmergeCommand(pkg, bundle, buildWorkDir)

	// Execute build
	var stdout, stderr bytes.Buffer
	execCmd := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr
	execCmd.Dir = buildWorkDir

	// Set environment variables
	execCmd.Env = append(os.Environ(), be.buildEnvironment(pkg, bundle)...)

	job.Log += fmt.Sprintf("Building package: %s\n", pkg.Atom)
	job.Log += fmt.Sprintf("Command: %s\n", strings.Join(cmd, " "))

	startTime := time.Now()
	err := execCmd.Run()
	duration := time.Since(startTime)

	job.Log += fmt.Sprintf("Build duration: %s\n", duration)
	job.Log += fmt.Sprintf("STDOUT:\n%s\n", stdout.String())
	if stderr.Len() > 0 {
		job.Log += fmt.Sprintf("STDERR:\n%s\n", stderr.String())
	}

	if err != nil {
		return fmt.Errorf("emerge failed: %w", err)
	}

	// Find and collect built packages
	if err := be.collectArtifacts(pkg, buildWorkDir, job); err != nil {
		return fmt.Errorf("failed to collect artifacts: %w", err)
	}

	return nil
}

// constructEmergeCommand constructs the emerge command for a package.
func (be *BuildExecutor) constructEmergeCommand(
	pkg PackageSpec,
	_ *ConfigBundle,
	_ string,
) []string {
	cmd := []string{"emerge"}

	// Add global options
	cmd = append(cmd, "--buildpkg")      // Build binary package
	cmd = append(cmd, "--usepkg=n")      // Don't use existing binpkgs
	cmd = append(cmd, "--oneshot")       // Don't add to world file
	cmd = append(cmd, "--verbose")       // Verbose output
	cmd = append(cmd, "--quiet-build=n") // Show build output

	// Add package-specific USE flags if provided
	if len(pkg.UseFlags) > 0 {
		useFlags := strings.Join(pkg.UseFlags, " ")
		cmd = append(cmd, fmt.Sprintf("--use=%s", useFlags))
	}

	// Add keywords if provided
	if len(pkg.Keywords) > 0 {
		keywords := strings.Join(pkg.Keywords, " ")
		cmd = append(cmd, fmt.Sprintf("--accept-keywords=%s", keywords))
	}

	// Add the package atom
	if pkg.Version != "" {
		cmd = append(cmd, fmt.Sprintf("%s-%s", pkg.Atom, pkg.Version))
	} else {
		cmd = append(cmd, pkg.Atom)
	}

	return cmd
}

// buildEnvironment constructs the environment variables for the build.
func (be *BuildExecutor) buildEnvironment(pkg PackageSpec, bundle *ConfigBundle) []string {
	env := []string{}

	// Add global environment variables from bundle
	for key, value := range bundle.Config.Environment {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	// Add package-specific environment variables
	for key, value := range pkg.Environment {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	// Set FEATURES for binary package generation
	env = append(env, "FEATURES=buildpkg")

	// Set PKGDIR to work directory
	pkgDir := filepath.Join(be.workDir, "packages")
	env = append(env, fmt.Sprintf("PKGDIR=%s", pkgDir))

	return env
}

// collectArtifacts collects built package artifacts.
func (be *BuildExecutor) collectArtifacts(
	pkg PackageSpec,
	_ string,
	job *BuildJob,
) error {
	// Find binary package in PKGDIR
	pkgDir := filepath.Join(be.workDir, "packages")

	// The package structure is typically: PKGDIR/category/package-version.tbz2
	// We need to find the exact file
	var foundPackages []string

	err := filepath.Walk(pkgDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".tbz2") {
			// Check if this package matches the atom
			if strings.Contains(path, strings.ReplaceAll(pkg.Atom, "/", string(os.PathSeparator))) {
				foundPackages = append(foundPackages, path)
			}
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to search for packages: %w", err)
	}

	if len(foundPackages) == 0 {
		return fmt.Errorf("no binary packages found for %s", pkg.Atom)
	}

	// Copy artifacts to artifact directory
	for _, pkgPath := range foundPackages {
		destPath := filepath.Join(be.artifactDir, filepath.Base(pkgPath))
		if err := be.copyFile(pkgPath, destPath); err != nil {
			return fmt.Errorf("failed to copy artifact: %w", err)
		}

		job.ArtifactURL = destPath
		job.Log += fmt.Sprintf("Artifact collected: %s\n", destPath)
	}

	return nil
}

// copyFile copies a file from src to dst.
func (be *BuildExecutor) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		_ = sourceFile.Close()
	}()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = destFile.Close()
	}()

	if _, err := sourceFile.WriteTo(destFile); err != nil {
		return err
	}

	return destFile.Sync()
}

// DockerBuildExecutor handles Docker-based builds.
type DockerBuildExecutor struct {
	*BuildExecutor
	dockerImage      string
	containerRuntime ContainerRuntime
}

// NewDockerBuildExecutor creates a new Docker-based build executor.
func NewDockerBuildExecutor(workDir, artifactDir, dockerImage string, runtime ContainerRuntime) *DockerBuildExecutor {
	if runtime == nil {
		runtime = NewDockerRuntime()
	}
	return &DockerBuildExecutor{
		BuildExecutor:    NewBuildExecutor(workDir, artifactDir),
		dockerImage:      dockerImage,
		containerRuntime: runtime,
	}
}

// ExecuteBuild executes a build inside a Docker container.
func (dbe *DockerBuildExecutor) ExecuteBuild(
	ctx context.Context,
	bundle *ConfigBundle,
	job *BuildJob,
) error {
	// Create build workspace
	buildID := job.ID
	buildWorkDir := filepath.Join(dbe.workDir, buildID)
	if err := os.MkdirAll(buildWorkDir, 0755); err != nil {
		return fmt.Errorf("failed to create build workspace: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(buildWorkDir)
	}()

	// Export configuration bundle
	bundlePath := filepath.Join(buildWorkDir, "config-bundle.tar.gz")
	if err := dbe.configTransfer.ExportBundle(bundle, bundlePath); err != nil {
		return fmt.Errorf("failed to export bundle: %w", err)
	}

	// Prepare container
	containerName := fmt.Sprintf("portage-build-%s", buildID)

	// Create container
	createArgs := []string{
		"--name", containerName,
		"-v", fmt.Sprintf("%s:/workspace", buildWorkDir),
		"-v", fmt.Sprintf("%s:/artifacts", dbe.artifactDir),
		"-w", "/workspace",
		dbe.dockerImage,
		"/bin/bash", "-c", "sleep infinity",
	}

	if err := dbe.containerRuntime.Create(ctx, createArgs); err != nil {
		job.Log += fmt.Sprintf("Failed to create container: %v\n", err)
		return fmt.Errorf("failed to create container: %w", err)
	}

	// Start container
	if err := dbe.containerRuntime.Start(ctx, containerName); err != nil {
		_ = dbe.cleanupContainer(ctx, containerName)
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Ensure cleanup
	defer func() {
		_ = dbe.cleanupContainer(ctx, containerName)
	}()

	// Extract configuration bundle inside container
	_, err := dbe.containerRuntime.Exec(ctx, containerName, []string{
		"/bin/bash", "-c",
		"mkdir -p /tmp/config && tar -xzf /workspace/config-bundle.tar.gz -C /tmp/config",
	})
	if err != nil {
		return fmt.Errorf("failed to extract config bundle: %w", err)
	}

	// Apply configuration
	_, err = dbe.containerRuntime.Exec(ctx, containerName, []string{
		"/bin/bash", "-c",
		"cp -r /tmp/config/etc/portage/* /etc/portage/",
	})
	if err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	// Build packages
	for _, pkg := range bundle.Packages.Packages {
		if err := dbe.buildPackageInDocker(ctx, pkg, bundle, containerName, job); err != nil {
			return fmt.Errorf("failed to build package %s: %w", pkg.Atom, err)
		}
	}

	return nil
}

// buildPackageInDocker builds a package inside container.
func (dbe *DockerBuildExecutor) buildPackageInDocker(
	ctx context.Context,
	pkg PackageSpec,
	bundle *ConfigBundle,
	containerName string,
	job *BuildJob,
) error {
	// Construct emerge command
	emergeCmd := dbe.constructEmergeCommand(pkg, bundle, "")
	cmdStr := strings.Join(emergeCmd, " ")

	// Set environment variables
	envVars := dbe.buildEnvironment(pkg, bundle)
	envStr := strings.Join(envVars, " ")

	// Execute build in container
	fullCmd := fmt.Sprintf("%s %s", envStr, cmdStr)

	job.Log += fmt.Sprintf("Building package in container: %s\n", pkg.Atom)
	job.Log += fmt.Sprintf("Command: %s\n", fullCmd)

	startTime := time.Now()
	output, err := dbe.containerRuntime.Exec(ctx, containerName, []string{
		"/bin/bash", "-c", fullCmd,
	})
	duration := time.Since(startTime)

	job.Log += fmt.Sprintf("Build duration: %s\n", duration)
	job.Log += fmt.Sprintf("Output:\n%s\n", string(output))

	if err != nil {
		return fmt.Errorf("emerge failed in container: %w", err)
	}

	// Copy artifacts from container
	src := fmt.Sprintf("%s:/var/cache/binpkgs/.", containerName)
	if err := dbe.containerRuntime.Copy(ctx, src, dbe.artifactDir); err != nil {
		job.Log += fmt.Sprintf("Warning: Failed to copy artifacts: %v\n", err)
	}

	return nil
}

// cleanupContainer stops and removes a container.
func (dbe *DockerBuildExecutor) cleanupContainer(ctx context.Context, containerName string) error {
	// Stop container
	_ = dbe.containerRuntime.Stop(ctx, containerName)

	// Remove container
	return dbe.containerRuntime.Remove(ctx, containerName)
}
