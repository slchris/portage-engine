// Package builder provides build execution capabilities.
package builder

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BuildOptions controls binary-package format and GPG signing for a build.
type BuildOptions struct {
	// Format is the BINPKG_FORMAT Portage produces: "gpkg" (default) or "xpak".
	Format string
	// SignKeyID, when non-empty, enables Gentoo-native binpkg signing
	// (FEATURES="binpkg-signing", BINPKG_GPG_SIGNING_KEY=...). Only GPKG output
	// can be signed.
	SignKeyID string
	// SignGnupgHome is the GNUPGHOME containing the signing key inside the build
	// environment (BINPKG_GPG_SIGNING_GPG_HOME).
	SignGnupgHome string
	// SignHostGnupgHome is the host directory holding the signing keyring; the
	// Docker executor bind-mounts it into the container at SignGnupgHome.
	SignHostGnupgHome string
}

// signingEnabled reports whether native binpkg signing should be configured.
func (o BuildOptions) signingEnabled() bool {
	return o.SignKeyID != "" && o.Format != "xpak"
}

// BuildExecutor handles the actual package build process.
type BuildExecutor struct {
	workDir        string
	artifactDir    string
	configTransfer *ConfigTransfer
	opts           BuildOptions
}

// NewBuildExecutor creates a new build executor with default (gpkg, unsigned)
// options.
func NewBuildExecutor(workDir, artifactDir string) *BuildExecutor {
	return NewBuildExecutorWithOptions(workDir, artifactDir, BuildOptions{Format: "gpkg"})
}

// NewBuildExecutorWithOptions creates a build executor with explicit options.
func NewBuildExecutorWithOptions(workDir, artifactDir string, opts BuildOptions) *BuildExecutor {
	if opts.Format == "" {
		opts.Format = "gpkg"
	}
	return &BuildExecutor{
		workDir:        workDir,
		artifactDir:    artifactDir,
		configTransfer: NewConfigTransfer(workDir),
		opts:           opts,
	}
}

// ExecuteBuild executes a build with the given configuration bundle.
func (be *BuildExecutor) ExecuteBuild(
	ctx context.Context,
	bundle *ConfigBundle,
	job *BuildJob,
) error {
	// Reject any bundle whose fields contain shell metacharacters or option
	// injection before constructing any command.
	if err := validateBundle(bundle); err != nil {
		return fmt.Errorf("invalid build request: %w", err)
	}

	// Create build workspace
	buildID := job.ID
	buildWorkDir := filepath.Join(be.workDir, buildID)
	if err := os.MkdirAll(buildWorkDir, 0750); err != nil {
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
	execCmd.Env = append(os.Environ(), be.buildEnvironment(pkg, bundle, be.nativePkgDir())...)

	job.appendLog(fmt.Sprintf("Building package: %s\n", pkg.Atom))
	job.appendLog(fmt.Sprintf("Command: %s\n", strings.Join(cmd, " ")))

	startTime := time.Now()
	err := execCmd.Run()
	duration := time.Since(startTime)

	job.appendLog(fmt.Sprintf("Build duration: %s\n", duration))
	job.appendLog(fmt.Sprintf("STDOUT:\n%s\n", stdout.String()))
	if stderr.Len() > 0 {
		job.appendLog(fmt.Sprintf("STDERR:\n%s\n", stderr.String()))
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

	// Add options to automatically resolve dependency conflicts
	cmd = append(cmd, "--autounmask")          // Automatically unmask packages
	cmd = append(cmd, "--autounmask-write")    // Write unmask changes to config
	cmd = append(cmd, "--autounmask-continue") // Continue after writing changes
	cmd = append(cmd, "--backtrack=50")        // Increase backtrack for complex deps

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

// buildEnvironment constructs the environment variables for the build. pkgDir is
// the PKGDIR emerge should write binary packages to (a host path for native
// builds, an in-container path for Docker builds).
func (be *BuildExecutor) buildEnvironment(pkg PackageSpec, bundle *ConfigBundle, pkgDir string) []string {
	env := []string{}

	// Collect any user-supplied FEATURES so we extend rather than clobber them.
	features := map[string]bool{"buildpkg": true}

	appendUserEnv := func(m map[string]string) {
		for key, value := range m {
			if key == "FEATURES" {
				for _, f := range strings.Fields(value) {
					features[f] = true
				}
				continue
			}
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
	}
	if bundle.Config != nil {
		appendUserEnv(bundle.Config.Environment)
	}
	appendUserEnv(pkg.Environment)

	// Select the binary package format (gpkg by default; only gpkg is signable).
	env = append(env, fmt.Sprintf("BINPKG_FORMAT=%s", be.opts.Format))

	// Configure Gentoo-native binpkg signing when a signing key is available.
	// This makes emerge itself produce a signed .gpkg.tar — the only signature
	// a stock emerge will verify — instead of an external detached .sig.
	if be.opts.signingEnabled() {
		features["binpkg-signing"] = true
		features["gpg-keepalive"] = true
		env = append(env, fmt.Sprintf("BINPKG_GPG_SIGNING_KEY=%s", be.opts.SignKeyID))
		if be.opts.SignGnupgHome != "" {
			env = append(env, fmt.Sprintf("BINPKG_GPG_SIGNING_GPG_HOME=%s", be.opts.SignGnupgHome))
		}
	}

	// Emit FEATURES in sorted order for determinism.
	featureList := make([]string, 0, len(features))
	for f := range features {
		featureList = append(featureList, f)
	}
	sort.Strings(featureList)
	env = append(env, fmt.Sprintf("FEATURES=%s", strings.Join(featureList, " ")))

	env = append(env, fmt.Sprintf("PKGDIR=%s", pkgDir))

	return env
}

// nativePkgDir returns the PKGDIR for native builds (a host path under workDir).
func (be *BuildExecutor) nativePkgDir() string {
	return filepath.Join(be.workDir, "packages")
}

// containerPkgDir is the in-container PKGDIR the Docker executor uses; it matches
// the path artifacts are copied from after the build.
const containerPkgDir = "/var/cache/binpkgs"

// collectArtifacts collects built package artifacts.
func (be *BuildExecutor) collectArtifacts(
	pkg PackageSpec,
	_ string,
	job *BuildJob,
) error {
	// Find binary package in PKGDIR
	pkgDir := be.nativePkgDir()

	// The package structure is typically:
	//   GPKG:  PKGDIR/category/package/package-version.gpkg.tar
	//   XPAK:  PKGDIR/category/package-version.tbz2
	var foundPackages []string

	err := filepath.Walk(pkgDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Match modern GPKG (.gpkg.tar) and legacy XPAK (.tbz2) so artifacts are
		// found regardless of the configured format.
		if !info.IsDir() && (strings.HasSuffix(info.Name(), ".gpkg.tar") || strings.HasSuffix(info.Name(), ".tbz2")) {
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

		job.setArtifactURL(destPath)
		job.appendLog(fmt.Sprintf("Artifact collected: %s\n", destPath))
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

// NewDockerBuildExecutor creates a new Docker-based build executor with default
// (gpkg, unsigned) options.
func NewDockerBuildExecutor(workDir, artifactDir, dockerImage string, runtime ContainerRuntime) *DockerBuildExecutor {
	return NewDockerBuildExecutorWithOptions(workDir, artifactDir, dockerImage, runtime, BuildOptions{Format: "gpkg"})
}

// NewDockerBuildExecutorWithOptions creates a Docker executor with explicit options.
func NewDockerBuildExecutorWithOptions(workDir, artifactDir, dockerImage string, runtime ContainerRuntime, opts BuildOptions) *DockerBuildExecutor {
	if runtime == nil {
		runtime = NewDockerRuntime()
	}
	return &DockerBuildExecutor{
		BuildExecutor:    NewBuildExecutorWithOptions(workDir, artifactDir, opts),
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
	// Reject any bundle whose fields contain shell metacharacters or option
	// injection before constructing any command.
	if err := validateBundle(bundle); err != nil {
		return fmt.Errorf("invalid build request: %w", err)
	}

	// Create build workspace
	buildID := job.ID
	buildWorkDir := filepath.Join(dbe.workDir, buildID)
	if err := os.MkdirAll(buildWorkDir, 0750); err != nil {
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
	}
	// Mount the signing keyring read-only at a staging path. GnuPG needs a
	// writable, 0700 GNUPGHOME, so the apply step copies it to a writable
	// location before use.
	if dbe.opts.signingEnabled() && dbe.opts.SignHostGnupgHome != "" {
		createArgs = append(createArgs,
			"-v", fmt.Sprintf("%s:%s:ro", dbe.opts.SignHostGnupgHome, dbe.opts.SignGnupgHome+"-src"))
	}
	createArgs = append(createArgs,
		"-w", "/workspace",
		dbe.dockerImage,
		"/bin/bash", "-c", "sleep infinity",
	)

	if err := dbe.containerRuntime.Create(ctx, createArgs); err != nil {
		job.appendLog(fmt.Sprintf("Failed to create container: %v\n", err))
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

	// Apply configuration. The make.conf override fragment is appended to the
	// real /etc/portage/make.conf (Portage does not source make.conf.d/), so it
	// is not left as a stray copied file but appended explicitly.
	_, err = dbe.containerRuntime.Exec(ctx, containerName, []string{
		"/bin/bash", "-c",
		"cp -r /tmp/config/etc/portage/* /etc/portage/ 2>/dev/null; " +
			"rm -f /etc/portage/make.conf.portage-engine; " +
			"if [ -f /tmp/config/" + makeConfFragmentPath + " ]; then " +
			"cat /tmp/config/" + makeConfFragmentPath + " >> /etc/portage/make.conf; fi",
	})
	if err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	// Prepare a writable GNUPGHOME for binpkg-signing: GnuPG requires a 0700,
	// writable home, but the host keyring is mounted read-only.
	if dbe.opts.signingEnabled() && dbe.opts.SignHostGnupgHome != "" {
		src := dbe.opts.SignGnupgHome + "-src"
		dst := dbe.opts.SignGnupgHome
		_, err = dbe.containerRuntime.Exec(ctx, containerName, []string{
			"/bin/bash", "-c",
			fmt.Sprintf("rm -rf %s && cp -a %s %s && chmod 700 %s && chmod -R go-rwx %s",
				dst, src, dst, dst, dst),
		})
		if err != nil {
			return fmt.Errorf("failed to prepare signing keyring: %w", err)
		}
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
	// Construct emerge command as an argv slice. The container runtime passes
	// it directly to `docker exec` (no shell), so none of the atom/USE/keyword
	// values can be interpreted as shell metacharacters.
	emergeCmd := dbe.constructEmergeCommand(pkg, bundle, "")

	// Environment variables are passed via `docker exec -e KEY=VALUE`, again
	// avoiding any shell interpretation of the values.
	envVars := dbe.buildEnvironment(pkg, bundle, containerPkgDir)

	job.appendLog(fmt.Sprintf("Building package in container: %s\n", pkg.Atom))
	job.appendLog(fmt.Sprintf("Command: %s\n", strings.Join(emergeCmd, " ")))

	startTime := time.Now()
	output, err := dbe.containerRuntime.ExecEnv(ctx, containerName, envVars, emergeCmd)
	duration := time.Since(startTime)

	job.appendLog(fmt.Sprintf("Build duration: %s\n", duration))
	job.appendLog(fmt.Sprintf("Output:\n%s\n", string(output)))

	if err != nil {
		return fmt.Errorf("emerge failed in container: %w", err)
	}

	// Copy artifacts from the container's PKGDIR (matches the PKGDIR env above).
	src := fmt.Sprintf("%s:%s/.", containerName, containerPkgDir)
	if err := dbe.containerRuntime.Copy(ctx, src, dbe.artifactDir); err != nil {
		job.appendLog(fmt.Sprintf("Warning: Failed to copy artifacts: %v\n", err))
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
