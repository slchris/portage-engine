// Package builder provides local and remote build capabilities.
package builder

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/slchris/portage-engine/internal/gpg"
	"github.com/slchris/portage-engine/internal/notification"
	"github.com/slchris/portage-engine/pkg/config"
)

// LocalBuildRequest represents a local package build request.
type LocalBuildRequest struct {
	PackageName  string            `json:"package_name"`
	Version      string            `json:"version"`
	Arch         string            `json:"arch,omitempty"`
	UseFlags     map[string]string `json:"use_flags"`
	Environment  map[string]string `json:"environment"`
	ConfigBundle *ConfigBundle     `json:"config_bundle,omitempty"`
	PackageSpecs []PackageSpec     `json:"package_specs,omitempty"`
}

// BuildJob represents a build job with its status.
type BuildJob struct {
	ID          string                 `json:"id"`
	Request     *LocalBuildRequest     `json:"request"`
	Status      string                 `json:"status"` // queued, building, success, failed
	StartTime   time.Time              `json:"start_time"`
	EndTime     time.Time              `json:"end_time"`
	Log         string                 `json:"log"`
	ArtifactURL string                 `json:"artifact_url"`
	Error       string                 `json:"error,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// LocalBuilder handles build jobs locally using Docker or native builds.
type LocalBuilder struct {
	workers          int
	jobQueue         chan *BuildJob
	jobs             map[string]*BuildJob
	jobsMutex        sync.RWMutex
	signer           *gpg.Signer
	gpgClient        *GPGKeyClient
	storageUpload    *StorageUploader
	workDir          string
	artifactDir      string
	useDocker        bool
	dockerImage      string
	containerRuntime ContainerRuntime
	executor         *BuildExecutor
	dockerExecutor   *DockerBuildExecutor
	notifier         *notification.Notifier
	jobStore         *JobStore
	persister        *JobPersister
	instanceID       string
	architecture     string
	pkgMgr           PackageManager
	cfg              *config.BuilderConfig
}

// NewLocalBuilder creates a new local builder instance.
func NewLocalBuilder(workers int, signer *gpg.Signer, cfg *config.BuilderConfig) *LocalBuilder {
	workDir := os.Getenv("BUILD_WORK_DIR")
	if workDir == "" {
		if cfg != nil && cfg.WorkDir != "" {
			workDir = cfg.WorkDir
		} else {
			workDir = "/var/tmp/portage-builds"
		}
	}

	artifactDir := os.Getenv("BUILD_ARTIFACT_DIR")
	if artifactDir == "" {
		if cfg != nil && cfg.ArtifactDir != "" {
			artifactDir = cfg.ArtifactDir
		} else {
			artifactDir = "/var/tmp/portage-artifacts"
		}
	}

	useDocker := os.Getenv("USE_DOCKER") == "true"
	if cfg != nil && cfg.UseDocker {
		useDocker = cfg.UseDocker
	}

	dockerImage := os.Getenv("DOCKER_IMAGE")
	if dockerImage == "" {
		if cfg != nil && cfg.DockerImage != "" {
			dockerImage = cfg.DockerImage
		} else {
			dockerImage = "gentoo/stage3:latest"
		}
	}

	// Initialize container runtime
	containerRuntimeName := os.Getenv("CONTAINER_RUNTIME")
	if containerRuntimeName == "" {
		if cfg != nil && cfg.ContainerRuntime != "" {
			containerRuntimeName = cfg.ContainerRuntime
		} else {
			containerRuntimeName = "docker"
		}
	}
	containerRuntime := NewContainerRuntime(containerRuntimeName)
	log.Printf("Container runtime: %s", containerRuntime.Name())

	_ = os.MkdirAll(workDir, 0750)
	_ = os.MkdirAll(artifactDir, 0750)

	// Verify directories are writable
	testFile := filepath.Join(workDir, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		log.Printf("WARNING: Work directory %s is not writable: %v", workDir, err)
		log.Printf("Please ensure the directory exists and is owned by the service user")
	} else {
		_ = os.Remove(testFile)
	}

	testFile = filepath.Join(artifactDir, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		log.Printf("WARNING: Artifact directory %s is not writable: %v", artifactDir, err)
		log.Printf("Please ensure the directory exists and is owned by the service user")
	} else {
		_ = os.Remove(testFile)
	}

	// Load notification config if exists
	var notifier *notification.Notifier
	notifyConfigPath := os.Getenv("NOTIFY_CONFIG")
	if notifyConfigPath == "" {
		if cfg != nil && cfg.NotifyConfig != "" {
			notifyConfigPath = cfg.NotifyConfig
		} else {
			notifyConfigPath = "configs/notification.json"
		}
	}
	notifyConfig, err := notification.LoadConfig(notifyConfigPath)
	if err == nil {
		notifier = notification.NewNotifier(notifyConfig)
		log.Printf("Notification system loaded from %s", notifyConfigPath)
	} else {
		log.Printf("Notification config not loaded (optional): %v", err)
	}

	// Initialize GPG key client if server URL is configured
	var gpgClient *GPGKeyClient
	if cfg != nil && cfg.ServerURL != "" {
		gpgClient = NewGPGKeyClient(cfg.ServerURL)
		if cfg.GPGHome != "" {
			gpgClient = gpgClient.WithGnupgHome(cfg.GPGHome)
		}
		log.Printf("GPG key client initialized with server URL: %s", cfg.ServerURL)

		// Auto-sync GPG key from server if enabled
		if cfg.GPGAutoSync && cfg.GPGEnabled {
			gpgKeyPath := cfg.GPGKeyPath
			if gpgKeyPath == "" {
				gpgKeyPath = filepath.Join(cfg.GPGHome, "server-public.asc")
			}
			if err := gpgClient.FetchAndImportGPGKey(gpgKeyPath); err != nil {
				log.Printf("Failed to sync GPG key from server: %v", err)
			} else {
				keyID, err := gpgClient.GetKeyID(gpgKeyPath)
				if err != nil {
					log.Printf("Failed to get GPG key ID: %v", err)
				} else {
					cfg.GPGKeyID = keyID
					log.Printf("GPG key synced from server: %s", keyID)
				}
			}
		}
	}

	// Initialize storage uploader
	var storageUpload *StorageUploader
	if cfg != nil {
		storageType := cfg.StorageType
		if storageType == "" {
			storageType = "local"
		}

		uploader, err := NewStorageUploader(
			storageType,
			cfg.StorageLocalDir,
			cfg.StorageS3Bucket,
			cfg.StorageS3Region,
			cfg.StorageS3Prefix,
			cfg.StorageHTTPBase,
		)
		if err != nil {
			log.Printf("Failed to initialize storage uploader: %v", err)
		} else {
			storageUpload = uploader
			log.Printf("Storage uploader initialized with type: %s, enabled: %v", storageType, storageUpload.IsEnabled())
		}
	}

	// Initialize job store for persistence
	var jobStore *JobStore
	var persister *JobPersister
	var loadedJobs map[string]*BuildJob

	persistenceEnabled := true
	dataDir := "/var/lib/portage-engine"
	retentionDays := 7

	if cfg != nil {
		persistenceEnabled = cfg.PersistenceEnabled
		if cfg.DataDir != "" {
			dataDir = cfg.DataDir
		}
		if cfg.RetentionDays > 0 {
			retentionDays = cfg.RetentionDays
		}
	}

	if persistenceEnabled {
		var err error
		jobStore, err = NewJobStore(dataDir)
		if err != nil {
			log.Printf("Failed to initialize job store: %v (persistence disabled)", err)
		} else {
			loadedJobs, err = jobStore.Load()
			if err != nil {
				log.Printf("Failed to load persisted jobs: %v", err)
				loadedJobs = make(map[string]*BuildJob)
			} else {
				log.Printf("Loaded %d persisted jobs from %s", len(loadedJobs), dataDir)
			}
		}
	}

	if loadedJobs == nil {
		loadedJobs = make(map[string]*BuildJob)
	}

	// Get instance ID and architecture from config or auto-detect
	instanceID := ""
	architecture := ""
	if cfg != nil {
		instanceID = cfg.InstanceID
		architecture = cfg.Architecture
	}
	if instanceID == "" {
		// Auto-generate instance ID from hostname
		if hostname, err := os.Hostname(); err == nil {
			instanceID = hostname
		} else {
			instanceID = uuid.New().String()[:8]
		}
	}
	if architecture == "" {
		// Auto-detect architecture
		architecture = detectArchitecture()
	}

	// Ensure cfg is not nil for PackageManager
	if cfg == nil {
		cfg = &config.BuilderConfig{
			PortageReposPath: "/var/db/repos",
			PortageConfPath:  "/etc/portage",
			MakeConfPath:     "/etc/portage/make.conf",
		}
	}

	lb := &LocalBuilder{
		workers:          workers,
		jobQueue:         make(chan *BuildJob, 100),
		jobs:             loadedJobs,
		signer:           signer,
		gpgClient:        gpgClient,
		storageUpload:    storageUpload,
		workDir:          workDir,
		artifactDir:      artifactDir,
		useDocker:        useDocker,
		dockerImage:      dockerImage,
		containerRuntime: containerRuntime,
		executor:         NewBuildExecutor(workDir, artifactDir),
		dockerExecutor:   NewDockerBuildExecutor(workDir, artifactDir, dockerImage, containerRuntime),
		notifier:         notifier,
		jobStore:         jobStore,
		persister:        nil,
		instanceID:       instanceID,
		architecture:     architecture,
		pkgMgr:           NewPackageManager(cfg),
		cfg:              cfg,
	}

	log.Printf("Builder initialized: instance_id=%s, architecture=%s", instanceID, architecture)

	// Initialize persister if job store is available
	if jobStore != nil {
		retentionDuration := time.Duration(retentionDays) * 24 * time.Hour
		persister = NewJobPersister(jobStore, lb.getJobsSnapshot, 30*time.Second, retentionDuration)
		persister.Start()
		lb.persister = persister
		log.Printf("Job persistence enabled with %d day retention", retentionDays)
	}

	// Start worker pool
	for i := 0; i < workers; i++ {
		go lb.worker(i)
	}

	return lb

}

// SubmitBuild submits a new build job.
func (lb *LocalBuilder) SubmitBuild(req *LocalBuildRequest) (string, error) {
	jobID := uuid.New().String()

	job := &BuildJob{
		ID:        jobID,
		Request:   req,
		Status:    "queued",
		StartTime: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	lb.jobsMutex.Lock()
	lb.jobs[jobID] = job
	lb.jobsMutex.Unlock()

	lb.jobQueue <- job

	return jobID, nil
}

// GetJobStatus returns the status of a build job.
func (lb *LocalBuilder) GetJobStatus(jobID string) (*BuildJob, error) {
	lb.jobsMutex.RLock()
	defer lb.jobsMutex.RUnlock()

	job, exists := lb.jobs[jobID]
	if !exists {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	return job, nil
}

// ListJobs returns all build jobs.
func (lb *LocalBuilder) ListJobs() []*BuildJob {
	lb.jobsMutex.RLock()
	defer lb.jobsMutex.RUnlock()

	jobs := make([]*BuildJob, 0, len(lb.jobs))
	for _, job := range lb.jobs {
		jobs = append(jobs, job)
	}

	return jobs
}

// getJobsSnapshot returns a copy of the jobs map for persistence.
func (lb *LocalBuilder) getJobsSnapshot() map[string]*BuildJob {
	lb.jobsMutex.RLock()
	defer lb.jobsMutex.RUnlock()

	snapshot := make(map[string]*BuildJob, len(lb.jobs))
	for id, job := range lb.jobs {
		snapshot[id] = job
	}
	return snapshot
}

// Shutdown gracefully shuts down the builder and persists jobs.
func (lb *LocalBuilder) Shutdown() {
	if lb.persister != nil {
		lb.persister.Stop()
	}
}

// GetStatus returns builder status.
func (lb *LocalBuilder) GetStatus() map[string]interface{} {
	lb.jobsMutex.RLock()
	defer lb.jobsMutex.RUnlock()

	queued := 0
	building := 0
	completed := 0
	failed := 0

	for _, job := range lb.jobs {
		switch job.Status {
		case "queued":
			queued++
		case "building":
			building++
		case "success":
			completed++
		case "failed":
			failed++
		}
	}

	// Get system resource information
	sysInfo := GetSystemInfo()

	// Determine status based on current load
	status := "online"
	if building >= lb.workers {
		status = "busy"
	}

	return map[string]interface{}{
		"instance_id":    lb.instanceID,
		"architecture":   lb.architecture,
		"status":         status,
		"workers":        lb.workers,
		"capacity":       lb.workers,
		"current_load":   building,
		"queued":         queued,
		"building":       building,
		"completed":      completed,
		"failed":         failed,
		"total":          len(lb.jobs),
		"success_builds": completed,
		"failed_builds":  failed,
		"total_builds":   completed + failed,
		"use_docker":     lb.useDocker,
		"docker_image":   lb.dockerImage,
		"cpu_usage":      sysInfo.CPUUsage,
		"memory_usage":   sysInfo.MemoryUsage,
		"disk_usage":     sysInfo.DiskUsage,
		"cpu_count":      sysInfo.CPUCount,
		"memory_total":   sysInfo.MemoryTotal,
		"memory_used":    sysInfo.MemoryUsed,
		"disk_total":     sysInfo.DiskTotal,
		"disk_used":      sysInfo.DiskUsed,
		"enabled":        true,
	}
}

// worker processes build jobs from the queue.
func (lb *LocalBuilder) worker(id int) {
	log.Printf("Worker %d started", id)

	for job := range lb.jobQueue {
		log.Printf("Worker %d processing job %s", id, job.ID)

		lb.jobsMutex.Lock()
		job.Status = "building"
		lb.jobsMutex.Unlock()

		var err error
		// Check if this is a new-style config bundle build
		if job.Request.ConfigBundle != nil {
			err = lb.executeConfigBundleBuild(job)
		} else {
			// Legacy build method
			if lb.useDocker {
				err = lb.executeDockerBuild(job)
			} else {
				err = lb.executeNativeBuild(job)
			}
		}

		lb.jobsMutex.Lock()
		job.EndTime = time.Now()
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
			// Append log to error for visibility in API
			if job.Log != "" {
				job.Error = fmt.Sprintf("%s\n\nBuild Log:\n%s", job.Error, job.Log)
			}
			log.Printf("Worker %d: Job %s failed: %v", id, job.ID, err)
		} else {
			job.Status = "success"
			log.Printf("Worker %d: Job %s completed successfully", id, job.ID)
		}
		lb.jobsMutex.Unlock()

		// Persist job state immediately after completion
		lb.saveJobState()

		// Send notification
		lb.sendNotification(job)
	}
}

// saveJobState saves the current job state to persistent storage.
func (lb *LocalBuilder) saveJobState() {
	if lb.persister != nil {
		if err := lb.persister.SaveNow(); err != nil {
			log.Printf("Failed to save job state: %v", err)
		}
	}
}

// executeConfigBundleBuild executes a build using configuration bundle.
func (lb *LocalBuilder) executeConfigBundleBuild(job *BuildJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	bundle := job.Request.ConfigBundle

	// If no package specs provided, create from legacy request
	if bundle.Packages == nil || len(bundle.Packages.Packages) == 0 {
		bundle.Packages = &BuildPackageSpec{
			Packages: []PackageSpec{
				{
					Atom:    job.Request.PackageName,
					Version: job.Request.Version,
				},
			},
		}
		// Convert legacy UseFlags map to slice
		if len(job.Request.UseFlags) > 0 {
			var useFlags []string
			for flag, enabled := range job.Request.UseFlags {
				if enabled == "true" || enabled == "1" {
					useFlags = append(useFlags, flag)
				} else {
					useFlags = append(useFlags, "-"+flag)
				}
			}
			bundle.Packages.Packages[0].UseFlags = useFlags
		}
	}

	var err error
	if lb.useDocker {
		err = lb.dockerExecutor.ExecuteBuild(ctx, bundle, job)
	} else {
		err = lb.executor.ExecuteBuild(ctx, bundle, job)
	}

	return err
}

// generateBuildScript creates a Gentoo build script for Docker container.
func (lb *LocalBuilder) generateBuildScript(pkgAtom string, useFlags string, gpgKeyID string) string {
	features := "buildpkg"
	gpgSetup := ""
	if gpgKeyID != "" {
		features = "buildpkg binpkg-signing"
		gpgSetup = fmt.Sprintf(`
# Setup GPG for package signing with secret key
export GNUPGHOME=/root/.gnupg
mkdir -p $GNUPGHOME
chmod 700 $GNUPGHOME

# Import secret key for signing (required for binpkg-signing)
if [ -f /gpg-keys/secret.asc ]; then
    gpg --batch --yes --import /gpg-keys/secret.asc 2>/dev/null || true
    echo "GPG secret key imported for signing"
fi

# Import public key as well
if [ -f /gpg-keys/public.asc ]; then
    gpg --batch --yes --import /gpg-keys/public.asc 2>/dev/null || true
fi

# Set trust level for the key
gpg --batch --yes --list-keys
echo -e "5\ny\n" | gpg --batch --yes --command-fd 0 --edit-key "%s" trust quit 2>/dev/null || true

# Configure portage for GPG signing
cat >> /etc/portage/make.conf << 'GPGEOF'
FEATURES="${FEATURES} binpkg-signing"
BINPKG_GPG_SIGNING_GPG_HOME="/root/.gnupg"
BINPKG_GPG_SIGNING_KEY="%s"
GPGEOF
`, gpgKeyID, gpgKeyID)
	}

	// Build emerge command with automatic dependency conflict resolution
	emergeOpts := "--usepkg=n --autounmask --autounmask-write --autounmask-continue --backtrack=50"

	return fmt.Sprintf(`#!/bin/bash
set -e
export USE="%s"
export FEATURES="%s"
%s
echo "Starting Gentoo package build for %s"

# Run emerge with automatic dependency resolution
# First attempt: try with autounmask options
if ! emerge %s %s; then
    echo "First emerge attempt failed, applying autounmask changes..."
    # Dispatch any pending config updates
    etc-update --automode -5 2>/dev/null || true
    dispatch-conf --use-rcs 2>/dev/null || true
    # Retry emerge after applying changes
    emerge %s %s || exit 1
fi

echo "Build completed, copying artifacts..."
find /var/cache/binpkgs -type f -name '*.gpkg.tar' -exec cp {} /output/ \; 2>/dev/null || find /var/cache/binpkgs -type f -name '*.tbz2' -exec cp {} /output/ \; 2>/dev/null || true
ls -lh /output/
`, useFlags, features, gpgSetup, pkgAtom, emergeOpts, pkgAtom, emergeOpts, pkgAtom)
}

// executeDockerBuild performs the build using Docker container.
func (lb *LocalBuilder) executeDockerBuild(job *BuildJob) error {
	req := job.Request

	jobWorkDir := filepath.Join(lb.workDir, job.ID)
	if err := os.MkdirAll(jobWorkDir, 0750); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(jobWorkDir) }()

	pkgAtom := req.PackageName
	if req.Version != "" {
		pkgAtom = fmt.Sprintf("=%s-%s", req.PackageName, req.Version)
	}

	// Build USE flags string (Gentoo-specific, but harmless for Debian)
	useFlags := ""
	for flag, enabled := range req.UseFlags {
		if enabled == "true" || enabled == "1" {
			useFlags += flag + " "
		} else {
			useFlags += "-" + flag + " "
		}
	}

	// Get GPG key ID for signing if enabled
	gpgKeyID := ""
	if lb.cfg != nil && lb.cfg.GPGEnabled && lb.cfg.GPGKeyID != "" {
		gpgKeyID = lb.cfg.GPGKeyID
	}

	// Create build script based on package manager
	script := lb.generateBuildScript(pkgAtom, useFlags, gpgKeyID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	outputDir := filepath.Join(jobWorkDir, "output")
	_ = os.MkdirAll(outputDir, 0750)

	// Prepare GPG key directory for mounting with both public and secret keys
	gpgKeyDir := ""
	if gpgKeyID != "" && lb.signer != nil && lb.signer.IsEnabled() {
		// Create temporary directory for GPG keys
		gpgKeyDir = filepath.Join(jobWorkDir, "gpg-keys")
		if err := os.MkdirAll(gpgKeyDir, 0700); err != nil {
			return fmt.Errorf("failed to create GPG key directory: %w", err)
		}
		// Export both public and secret keys for container signing
		_, _, err := lb.signer.ExportKeyPair(gpgKeyDir)
		if err != nil {
			log.Printf("Warning: failed to export GPG keys: %v", err)
			gpgKeyDir = ""
		} else {
			log.Printf("GPG keys exported to %s for container signing", gpgKeyDir)
		}
	}

	// Build container run arguments
	args := []string{"--rm", "-i",
		"-v", outputDir + ":/output",
	}

	// Mount GPG keys if available
	if gpgKeyDir != "" {
		args = append(args, "-v", gpgKeyDir+":/gpg-keys:ro")
	}

	// Add package manager specific mounts
	if lb.cfg != nil {
		mounts := lb.pkgMgr.GetDockerMounts(lb.cfg)
		for _, m := range mounts {
			args = append(args, "-v", m.String())
		}

		// Add environment variables
		envVars := lb.pkgMgr.GetEnvVars(lb.cfg)
		for k, v := range envVars {
			args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
		}
	} else {
		// Fallback to default Gentoo mounts for backward compatibility
		args = append(args,
			"-v", "/var/db/repos:/var/db/repos:ro",
			"-v", "/etc/portage/make.conf:/etc/portage/make.conf:ro",
			"-v", "/etc/portage/repos.conf:/etc/portage/repos.conf:ro",
		)
	}

	args = append(args, lb.dockerImage, "/bin/bash", "-c", script)

	// Run container with script
	output, err := lb.containerRuntime.Run(ctx, args)
	job.Log = string(output)

	if err != nil {
		log.Printf("Container build failed for job %s: %v", job.ID, err)
		return fmt.Errorf("container build failed: %w", err)
	}

	log.Printf("Container build completed for job %s, output size: %d bytes", job.ID, len(output))

	// Ensure all filesystem writes are flushed
	_ = exec.Command("sync").Run()

	// Give container time to fully unmount volumes and sync filesystem
	time.Sleep(10 * time.Second)

	// Wait for Docker filesystem to sync and retry if needed
	var artifact string
	for i := 0; i < 10; i++ {
		if i > 0 {
			// Force filesystem sync before retrying
			_ = exec.Command("sync").Run()
			time.Sleep(2 * time.Second)
		}

		artifact, err = lb.findLatestPackage(outputDir, req.PackageName)
		if err == nil {
			break
		}

		if i < 4 {
			log.Printf("Artifact not found on attempt %d, retrying...", i+1)
		}
	}
	if err != nil {
		return fmt.Errorf("artifact not found: %w", err)
	}

	artifactName := filepath.Base(artifact)
	destPath := filepath.Join(lb.artifactDir, artifactName)

	copyCmd := exec.Command("cp", artifact, destPath)
	if err := copyCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy artifact: %w", err)
	}

	job.ArtifactURL = destPath

	if lb.signer != nil && lb.signer.IsEnabled() {
		if err := lb.signer.SignPackage(destPath); err != nil {
			log.Printf("Warning: failed to sign package: %v", err)
		} else {
			job.Metadata["signed"] = true
			log.Printf("Package signed: %s", destPath)
		}
	}

	// Upload to storage if configured
	if lb.storageUpload != nil && lb.storageUpload.IsEnabled() {
		remotePath := artifactName
		if err := lb.storageUpload.Upload(destPath, remotePath); err != nil {
			log.Printf("Warning: failed to upload artifact to storage: %v", err)
		} else {
			uploadedURL, _ := lb.storageUpload.GetURL(remotePath)
			job.ArtifactURL = uploadedURL
			job.Metadata["uploaded"] = true
			log.Printf("Artifact uploaded to storage: %s", uploadedURL)
		}
	}

	return nil
}

// executeNativeBuild performs the build natively using the system package manager.
func (lb *LocalBuilder) executeNativeBuild(job *BuildJob) error {
	req := job.Request

	jobWorkDir := filepath.Join(lb.workDir, job.ID)
	if err := os.MkdirAll(jobWorkDir, 0750); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(jobWorkDir) }()

	pkgAtom := req.PackageName
	if req.Version != "" {
		pkgAtom = fmt.Sprintf("=%s-%s", req.PackageName, req.Version)
	}

	env := os.Environ()
	for k, v := range req.Environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Add package manager specific environment variables
	if lb.cfg != nil {
		pkgEnv := lb.pkgMgr.GetEnvVars(lb.cfg)
		for k, v := range pkgEnv {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	if len(req.UseFlags) > 0 {
		useFlags := ""
		for flag, enabled := range req.UseFlags {
			if enabled == "true" || enabled == "1" {
				useFlags += flag + " "
			} else {
				useFlags += "-" + flag + " "
			}
		}
		env = append(env, fmt.Sprintf("USE=%s", useFlags))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	// Build command using package manager
	buildCmd := lb.pkgMgr.BuildCommand(pkgAtom, nil)
	cmd := exec.CommandContext(ctx, buildCmd[0], buildCmd[1:]...)
	cmd.Env = env
	cmd.Dir = jobWorkDir

	output, err := cmd.CombinedOutput()
	job.Log = string(output)

	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	// Find artifacts from package manager specific paths
	var artifact string
	artifactPaths := lb.pkgMgr.GetArtifactPaths()
	for _, pkgDir := range artifactPaths {
		artifact, err = lb.findLatestPackage(pkgDir, req.PackageName)
		if err == nil {
			break
		}
	}
	if artifact == "" {
		return fmt.Errorf("artifact not found in any of %v", artifactPaths)
	}

	artifactName := filepath.Base(artifact)
	destPath := filepath.Join(lb.artifactDir, artifactName)

	copyCmd := exec.Command("cp", artifact, destPath)
	if err := copyCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy artifact: %w", err)
	}

	job.ArtifactURL = destPath

	if lb.signer != nil && lb.signer.IsEnabled() {
		if err := lb.signer.SignPackage(destPath); err != nil {
			log.Printf("Warning: failed to sign package: %v", err)
		} else {
			job.Metadata["signed"] = true
			log.Printf("Package signed: %s", destPath)
		}
	}

	// Upload to storage if configured
	if lb.storageUpload != nil && lb.storageUpload.IsEnabled() {
		remotePath := artifactName
		if err := lb.storageUpload.Upload(destPath, remotePath); err != nil {
			log.Printf("Warning: failed to upload artifact to storage: %v", err)
		} else {
			uploadedURL, _ := lb.storageUpload.GetURL(remotePath)
			job.ArtifactURL = uploadedURL
			job.Metadata["uploaded"] = true
			log.Printf("Artifact uploaded to storage: %s", uploadedURL)
		}
	}

	return nil
}

// findLatestPackage finds the most recently created package file.
func (lb *LocalBuilder) findLatestPackage(dir, pkgName string) (string, error) {
	var matchedFiles []string
	var callbackCount int

	log.Printf("Finding packages in directory: %s for package: %s", dir, pkgName)

	// Check if directory exists and is accessible
	info, err := os.Stat(dir)
	if err != nil {
		log.Printf("ERROR: Cannot stat directory %s: %v", dir, err)
		return "", fmt.Errorf("directory not accessible: %w", err)
	}
	log.Printf("Directory stat OK: %s (isDir: %v, mode: %v)", dir, info.IsDir(), info.Mode())

	// Try to list directory contents directly
	if entries, readErr := os.ReadDir(dir); readErr != nil {
		log.Printf("ERROR: Cannot read directory %s: %v", dir, readErr)
	} else {
		log.Printf("Directory has %d entries", len(entries))
		for _, entry := range entries {
			log.Printf("  Entry: %s (isDir: %v)", entry.Name(), entry.IsDir())
		}
	}

	err = filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		callbackCount++
		if walkErr != nil {
			log.Printf("Walk error at %s: %v", path, walkErr)
			return nil
		}

		name := filepath.Base(path)
		log.Printf("Walk callback #%d: %s (isDir: %v, size: %d)", callbackCount, name, info.IsDir(), info.Size())

		if !info.IsDir() && (strings.HasSuffix(name, ".tbz2") || strings.HasSuffix(name, ".gpkg.tar")) {
			log.Printf("Package file matched: %s", name)
			matchedFiles = append(matchedFiles, path)
		}

		return nil
	})

	log.Printf("Walk completed. Callback was called %d times", callbackCount)

	if err != nil {
		log.Printf("Walk returned error: %v", err)
		return "", err
	}

	if len(matchedFiles) == 0 {
		log.Printf("No package files found in %s", dir)
		return "", fmt.Errorf("no package found")
	}

	// Return the largest file (main package is usually largest)
	var largestFile string
	var largestSize int64
	for _, f := range matchedFiles {
		if info, err := os.Stat(f); err == nil {
			if info.Size() > largestSize {
				largestSize = info.Size()
				largestFile = f
			}
		}
	}

	log.Printf("Found %d packages, returning largest: %s (%d bytes)", len(matchedFiles), largestFile, largestSize)
	return largestFile, nil
}

// sendNotification sends build completion notification.
func (lb *LocalBuilder) sendNotification(job *BuildJob) {
	if lb.notifier == nil {
		return
	}

	duration := job.EndTime.Sub(job.StartTime)
	notify := &notification.BuildNotification{
		JobID:       job.ID,
		PackageName: job.Request.PackageName,
		Version:     job.Request.Version,
		Status:      job.Status,
		StartTime:   job.StartTime,
		EndTime:     job.EndTime,
		Duration:    duration.String(),
		BuildLog:    job.Log,
		Error:       job.Error,
		ArtifactURL: job.ArtifactURL,
	}

	if err := lb.notifier.Notify(notify); err != nil {
		log.Printf("Failed to send notification for job %s: %v", job.ID, err)
	}
}

// GetArtifactPath returns the local file path of the artifact for a job.
// Returns empty string if job not found or artifact not available.
func (lb *LocalBuilder) GetArtifactPath(jobID string) (string, error) {
	lb.jobsMutex.RLock()
	job, exists := lb.jobs[jobID]
	lb.jobsMutex.RUnlock()

	if !exists {
		return "", fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status != "success" {
		return "", fmt.Errorf("job not completed successfully: status=%s", job.Status)
	}

	if job.ArtifactURL == "" {
		return "", fmt.Errorf("no artifact available for job: %s", jobID)
	}

	// Check if ArtifactURL is a local file path
	if _, err := os.Stat(job.ArtifactURL); err != nil {
		return "", fmt.Errorf("artifact file not found: %s", job.ArtifactURL)
	}

	return job.ArtifactURL, nil
}

// ArtifactInfo contains metadata about a build artifact.
type ArtifactInfo struct {
	JobID       string `json:"job_id"`
	FileName    string `json:"file_name"`
	FilePath    string `json:"file_path"`
	FileSize    int64  `json:"file_size"`
	PackageName string `json:"package_name"`
	Version     string `json:"version"`
}

// GetArtifactInfo returns metadata about the artifact for a job.
func (lb *LocalBuilder) GetArtifactInfo(jobID string) (*ArtifactInfo, error) {
	lb.jobsMutex.RLock()
	job, exists := lb.jobs[jobID]
	lb.jobsMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status != "success" {
		return nil, fmt.Errorf("job not completed successfully: status=%s", job.Status)
	}

	if job.ArtifactURL == "" {
		return nil, fmt.Errorf("no artifact available for job: %s", jobID)
	}

	// Get file info
	fileInfo, err := os.Stat(job.ArtifactURL)
	if err != nil {
		return nil, fmt.Errorf("artifact file not found: %s", job.ArtifactURL)
	}

	return &ArtifactInfo{
		JobID:       jobID,
		FileName:    filepath.Base(job.ArtifactURL),
		FilePath:    job.ArtifactURL,
		FileSize:    fileInfo.Size(),
		PackageName: job.Request.PackageName,
		Version:     job.Request.Version,
	}, nil
}
