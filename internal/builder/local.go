// Package builder provides local and remote build capabilities.
package builder

import (
	"archive/tar"
	"bufio"
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
//
// The mutable fields (Status, Log, ArtifactURL, Error, EndTime, Metadata) are
// written by the worker/executor goroutine while HTTP handler goroutines read
// them, so all access must go through the mu-guarded helpers below.
type BuildJob struct {
	mu          sync.Mutex         `json:"-"`
	ID          string             `json:"id"`
	Request     *LocalBuildRequest `json:"request"`
	Status      string             `json:"status"` // queued, building, success, failed
	StartTime   time.Time          `json:"start_time"`
	EndTime     time.Time          `json:"end_time"`
	Log         string             `json:"log"`
	ArtifactURL string             `json:"artifact_url"`
	// Artifacts lists every binary package produced by the build, as paths
	// relative to the artifact dir with the category preserved
	// (e.g. "app-misc/jq-1.8.1-1.gpkg.tar").
	Artifacts []string               `json:"artifacts,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// appendLog appends to the job log under the job lock.
func (j *BuildJob) appendLog(s string) {
	j.mu.Lock()
	j.Log += s
	j.mu.Unlock()
}

// setArtifacts records the full produced-artifact list under the job lock.
func (j *BuildJob) setArtifacts(rels []string) {
	j.mu.Lock()
	j.Artifacts = rels
	j.mu.Unlock()
}

// artifactsSnapshot returns a copy of the produced-artifact list.
func (j *BuildJob) artifactsSnapshot() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.Artifacts...)
}

// setArtifactURL sets the artifact URL under the job lock.
func (j *BuildJob) setArtifactURL(url string) {
	j.mu.Lock()
	j.ArtifactURL = url
	j.mu.Unlock()
}

// setLog replaces the job log under the job lock.
func (j *BuildJob) setLog(s string) {
	j.mu.Lock()
	j.Log = s
	j.mu.Unlock()
}

// snapshot returns a copy of the status and artifact URL under the job lock.
func (j *BuildJob) snapshot() (status, artifactURL string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Status, j.ArtifactURL
}

// Clone returns a deep copy of the job (without the mutex) taken under the job
// lock. Use this instead of copying a BuildJob by value, which would copy the
// mutex.
func (j *BuildJob) Clone() *BuildJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	c := &BuildJob{
		ID:          j.ID,
		Request:     j.Request,
		Status:      j.Status,
		StartTime:   j.StartTime,
		EndTime:     j.EndTime,
		Log:         j.Log,
		ArtifactURL: j.ArtifactURL,
		Artifacts:   append([]string(nil), j.Artifacts...),
		Error:       j.Error,
	}
	if j.Metadata != nil {
		c.Metadata = make(map[string]interface{}, len(j.Metadata))
		for k, v := range j.Metadata {
			c.Metadata[k] = v
		}
	}
	return c
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
	return newLocalBuilderWithConfig(workers, signer, cfg)
}

// newLocalBuilderWithConfig creates a new local builder with the given configuration.
func newLocalBuilderWithConfig(workers int, signer *gpg.Signer, cfg *config.BuilderConfig) *LocalBuilder {
	workDir := getWorkDir(cfg)
	artifactDir := getArtifactDir(cfg)
	useDocker := getUseDocker(cfg)
	dockerImage := getDockerImage(cfg)
	containerRuntimeName := getContainerRuntime(cfg)
	containerRuntime := NewContainerRuntime(containerRuntimeName)

	log.Printf("Container runtime: %s", containerRuntime.Name())

	ensureDirectories(workDir, artifactDir)
	notifier := loadNotifier(cfg)
	gpgClient := initGPGClient(cfg)
	storageUpload := initStorageUploader(cfg)
	executor := initBuildExecutor(cfg, containerRuntime, dockerImage)
	dockerExecutor := initDockerExecutor(cfg, containerRuntime, dockerImage)
	pkgMgr := initPackageManager(cfg)
	jobStore := initJobStore(cfg)

	instanceID := generateInstanceID(cfg)
	architecture := getArchitecture(cfg)

	lb := &LocalBuilder{
		workers:          workers,
		jobQueue:         make(chan *BuildJob, 100),
		jobs:             make(map[string]*BuildJob),
		signer:           signer,
		gpgClient:        gpgClient,
		storageUpload:    storageUpload,
		workDir:          workDir,
		artifactDir:      artifactDir,
		useDocker:        useDocker,
		dockerImage:      dockerImage,
		containerRuntime: containerRuntime,
		executor:         executor,
		dockerExecutor:   dockerExecutor,
		notifier:         notifier,
		jobStore:         jobStore,
		instanceID:       instanceID,
		architecture:     architecture,
		pkgMgr:           pkgMgr,
		cfg:              cfg,
	}

	if jobStore != nil {
		loadedJobs, err := jobStore.Load()
		if err != nil {
			log.Printf("Failed to load persisted jobs: %v", err)
		} else {
			lb.jobs = loadedJobs
			log.Printf("Loaded %d persisted jobs", len(loadedJobs))
			reconcileLoadedJobs(jobStore, loadedJobs)
		}

		// Construct the persister so job state actually survives restarts.
		// Without this, saveJobState()/Stop() were no-ops (lb.persister was nil).
		retention := time.Duration(cfg.RetentionDays) * 24 * time.Hour
		lb.persister = NewJobPersister(jobStore, lb.jobsSnapshot, 30*time.Second, retention)
		lb.persister.Start()
	}

	for i := 0; i < workers; i++ {
		go lb.worker(i)
	}

	return lb
}

// getWorkDir returns the work directory from config or environment.
func getWorkDir(cfg *config.BuilderConfig) string {
	workDir := os.Getenv("BUILD_WORK_DIR")
	if workDir == "" {
		if cfg != nil && cfg.WorkDir != "" {
			return cfg.WorkDir
		}
		return "/var/tmp/portage-builds"
	}
	return workDir
}

// getArtifactDir returns the artifact directory from config or environment.
func getArtifactDir(cfg *config.BuilderConfig) string {
	artifactDir := os.Getenv("BUILD_ARTIFACT_DIR")
	if artifactDir == "" {
		if cfg != nil && cfg.ArtifactDir != "" {
			return cfg.ArtifactDir
		}
		return "/var/tmp/portage-artifacts"
	}
	return artifactDir
}

// getUseDocker returns whether to use Docker from config or environment.
func getUseDocker(cfg *config.BuilderConfig) bool {
	if cfg != nil && cfg.UseDocker {
		return true
	}
	return os.Getenv("USE_DOCKER") == "true"
}

// getDockerImage returns the Docker image from config or environment.
func getDockerImage(cfg *config.BuilderConfig) string {
	dockerImage := os.Getenv("DOCKER_IMAGE")
	if dockerImage == "" {
		if cfg != nil && cfg.DockerImage != "" {
			return cfg.DockerImage
		}
		return "gentoo/stage3:latest"
	}
	return dockerImage
}

// getContainerRuntime returns the container runtime from config or environment.
func getContainerRuntime(cfg *config.BuilderConfig) string {
	containerRuntime := os.Getenv("CONTAINER_RUNTIME")
	if containerRuntime == "" {
		if cfg != nil && cfg.ContainerRuntime != "" {
			return cfg.ContainerRuntime
		}
		return "docker"
	}
	return containerRuntime
}

// ensureDirectories creates and verifies the work and artifact directories.
func ensureDirectories(workDir, artifactDir string) {
	_ = os.MkdirAll(workDir, 0750)
	_ = os.MkdirAll(artifactDir, 0750)
	verifyDirectoryWritable(workDir, "Work")
	verifyDirectoryWritable(artifactDir, "Artifact")
}

// verifyDirectoryWritable checks if a directory is writable.
func verifyDirectoryWritable(dir, dirType string) {
	testFile := filepath.Join(dir, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		log.Printf("WARNING: %s directory %s is not writable: %v", dirType, dir, err)
		log.Printf("Please ensure the directory exists and is owned by the service user")
	} else {
		_ = os.Remove(testFile)
	}
}

// loadNotifier loads the notification configuration.
func loadNotifier(cfg *config.BuilderConfig) *notification.Notifier {
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
		log.Printf("Notification system loaded from %s", notifyConfigPath)
		return notification.NewNotifier(notifyConfig)
	}
	log.Printf("Notification config not loaded (optional): %v", err)
	return nil
}

// initGPGClient initializes the GPG key client if configured.
func initGPGClient(cfg *config.BuilderConfig) *GPGKeyClient {
	if cfg == nil || cfg.ServerURL == "" {
		return nil
	}

	gpgClient := NewGPGKeyClient(cfg.ServerURL)
	if cfg.GPGHome != "" {
		gpgClient = gpgClient.WithGnupgHome(cfg.GPGHome)
	}
	log.Printf("GPG key client initialized with server URL: %s", cfg.ServerURL)

	if cfg.GPGAutoSync && cfg.GPGEnabled {
		syncGPGKey(gpgClient, cfg)
	}

	return gpgClient
}

// syncGPGKey imports the server's public key so the builder can verify
// server-signed material. It does NOT change cfg.GPGKeyID: that is the builder's
// own signing key, and signing requires a private key (the server key is public
// only). Overwriting it would break binpkg-signing.
func syncGPGKey(gpgClient *GPGKeyClient, cfg *config.BuilderConfig) {
	gpgKeyPath := filepath.Join(cfg.GPGHome, "server-public.asc")

	if err := gpgClient.FetchAndImportGPGKey(gpgKeyPath); err != nil {
		log.Printf("Failed to sync GPG key from server: %v", err)
		return
	}

	keyID, err := gpgClient.GetKeyID(gpgKeyPath)
	if err != nil {
		log.Printf("Failed to get server GPG key ID: %v", err)
		return
	}

	log.Printf("Imported server GPG public key for verification: %s", keyID)
}

// initStorageUploader initializes the storage uploader if configured.
func initStorageUploader(cfg *config.BuilderConfig) *StorageUploader {
	if cfg == nil {
		return nil
	}

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
		return nil
	}

	log.Printf("Storage uploader initialized with type: %s, enabled: %v", storageType, uploader.IsEnabled())
	return uploader
}

// containerGnupgHome is where the signing key's GNUPGHOME is mounted inside the
// build container for the config-bundle executor path.
const containerGnupgHome = "/gpg-signing"

// buildOptionsFromConfig derives the executor's format/signing options from the
// builder config. Native binpkg signing is enabled only when GPG is enabled and
// a key ID is configured, and only for the (signable) gpkg format. forDocker
// selects the in-container GNUPGHOME path (the keyring is bind-mounted) vs the
// real host path used by the native executor.
func buildOptionsFromConfig(cfg *config.BuilderConfig, forDocker bool) BuildOptions {
	format := "gpkg"
	if cfg != nil && cfg.BinpkgFormat != "" {
		format = cfg.BinpkgFormat
	}
	opts := BuildOptions{Format: format}
	if cfg != nil && cfg.GPGEnabled && cfg.GPGKeyID != "" && format != "xpak" {
		opts.SignKeyID = cfg.GPGKeyID
		opts.SignHostGnupgHome = cfg.GPGHome
		if forDocker {
			// The Docker executor bind-mounts the host keyring into the container
			// at containerGnupgHome; emerge inside the container reads it there.
			opts.SignGnupgHome = containerGnupgHome
		} else {
			// The native executor runs emerge directly on the host, so it must
			// point BINPKG_GPG_SIGNING_GPG_HOME at the real host keyring.
			opts.SignGnupgHome = cfg.GPGHome
		}
	}
	return opts
}

// initBuildExecutor initializes the build executor.
func initBuildExecutor(cfg *config.BuilderConfig, _ ContainerRuntime, _ string) *BuildExecutor {
	workDir := getWorkDir(cfg)
	artifactDir := getArtifactDir(cfg)
	return NewBuildExecutorWithOptions(workDir, artifactDir, buildOptionsFromConfig(cfg, false))
}

// initDockerExecutor initializes the Docker build executor.
func initDockerExecutor(cfg *config.BuilderConfig, containerRuntime ContainerRuntime, dockerImage string) *DockerBuildExecutor {
	workDir := getWorkDir(cfg)
	artifactDir := getArtifactDir(cfg)
	return NewDockerBuildExecutorWithOptions(workDir, artifactDir, dockerImage, containerRuntime, buildOptionsFromConfig(cfg, true))
}

// initPackageManager initializes the package manager.
func initPackageManager(cfg *config.BuilderConfig) PackageManager {
	if cfg == nil {
		cfg = &config.BuilderConfig{
			PortageReposPath: "/var/db/repos",
			PortageConfPath:  "/etc/portage",
			MakeConfPath:     "/etc/portage/make.conf",
		}
	}
	return NewPackageManager(cfg)
}

// initJobStore initializes the job store and loads persisted jobs.
func initJobStore(cfg *config.BuilderConfig) *JobStore {
	if cfg == nil || !cfg.PersistenceEnabled {
		return nil
	}

	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = "/var/lib/portage-engine"
	}

	jobStore, err := NewJobStore(dataDir)
	if err != nil {
		log.Printf("Failed to initialize job store: %v (persistence disabled)", err)
		return nil
	}

	return jobStore
}

// reconcileLoadedJobs marks any persisted jobs that were left in the
// "building" state (e.g. due to a crash) as "failed", since the build
// process did not survive the restart. The reconciled state is persisted.
func reconcileLoadedJobs(jobStore *JobStore, jobs map[string]*BuildJob) {
	reconciled := 0
	for _, job := range jobs {
		if job.Status == "building" || job.Status == "queued" {
			job.Status = "failed"
			if job.Error == "" {
				job.Error = "build interrupted by builder restart"
			}
			if job.EndTime.IsZero() {
				job.EndTime = time.Now()
			}
			reconciled++
		}
	}

	if reconciled == 0 {
		return
	}

	log.Printf("Reconciled %d interrupted job(s) to failed on startup", reconciled)
	if err := jobStore.Save(jobs); err != nil {
		log.Printf("Failed to persist reconciled jobs: %v", err)
	}
}

// generateInstanceID generates or retrieves the instance ID.
func generateInstanceID(cfg *config.BuilderConfig) string {
	if cfg != nil && cfg.InstanceID != "" {
		return cfg.InstanceID
	}

	if hostname, err := os.Hostname(); err == nil {
		return hostname
	}

	return uuid.New().String()[:8]
}

// getArchitecture detects or retrieves the system architecture.
func getArchitecture(cfg *config.BuilderConfig) string {
	if cfg != nil && cfg.Architecture != "" {
		return cfg.Architecture
	}
	return detectArchitecture()
}

// SubmitBuild submits a new build job.
func (lb *LocalBuilder) SubmitBuild(req *LocalBuildRequest) (string, error) {
	// Validate every untrusted field before the request can reach any build
	// path (the legacy Docker shell script, the native emerge argv, or the
	// config-bundle executor). This is the single choke point that closes shell
	// injection and emerge option injection.
	if err := validateLocalBuildRequest(req); err != nil {
		return "", fmt.Errorf("invalid build request: %w", err)
	}

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

	// Non-blocking send: if the queue is full, reject the job instead of
	// blocking the calling (HTTP handler) goroutine indefinitely.
	select {
	case lb.jobQueue <- job:
		return jobID, nil
	default:
		lb.jobsMutex.Lock()
		delete(lb.jobs, jobID)
		lb.jobsMutex.Unlock()
		return "", fmt.Errorf("builder queue full")
	}
}

// GetJobStatus returns the status of a build job.
func (lb *LocalBuilder) GetJobStatus(jobID string) (*BuildJob, error) {
	lb.jobsMutex.RLock()
	defer lb.jobsMutex.RUnlock()

	job, exists := lb.jobs[jobID]
	if !exists {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	// Return a clone so callers (e.g. JSON-encoding HTTP handlers) never read
	// the live job's mutable fields while the worker is writing them.
	return job.Clone(), nil
}

// ListJobs returns all build jobs.
func (lb *LocalBuilder) ListJobs() []*BuildJob {
	lb.jobsMutex.RLock()
	defer lb.jobsMutex.RUnlock()

	jobs := make([]*BuildJob, 0, len(lb.jobs))
	for _, job := range lb.jobs {
		// Clone so concurrent worker writes don't race the caller's reads.
		jobs = append(jobs, job.Clone())
	}

	return jobs
}

// jobsSnapshot returns a deep copy of the jobs map for persistence. Clone()
// copies each job's fields under its lock (without the mutex), so this is safe
// to call while workers are mutating jobs.
func (lb *LocalBuilder) jobsSnapshot() map[string]*BuildJob {
	lb.jobsMutex.RLock()
	defer lb.jobsMutex.RUnlock()
	out := make(map[string]*BuildJob, len(lb.jobs))
	for k, v := range lb.jobs {
		out[k] = v.Clone()
	}
	return out
}

// Shutdown gracefully shuts down the builder and persists jobs.
func (lb *LocalBuilder) Shutdown() {
	if lb.persister != nil {
		lb.persister.Stop()
	}
}

// ActiveJobs returns the number of jobs currently queued or building, for
// heartbeat/capacity reporting to the central server.
func (lb *LocalBuilder) ActiveJobs() int {
	lb.jobsMutex.RLock()
	defer lb.jobsMutex.RUnlock()

	n := 0
	for _, job := range lb.jobs {
		if job.Status == "queued" || job.Status == "building" {
			n++
		}
	}
	return n
}

// GetStatus returns a snapshot of the builder's job counts and mode.
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

		job.mu.Lock()
		job.Status = "building"
		job.mu.Unlock()

		// Persist the "building" transition so a crash mid-build can be
		// reconciled on the next startup instead of leaving a stuck job.
		lb.saveJobState()

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

		job.mu.Lock()
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
		job.mu.Unlock()

		// Persist job state immediately after completion
		lb.saveJobState()

		// Notify asynchronously: notification channels (SMTP/webhook/Slack/
		// Telegram) run serially with timeouts up to ~30s each, which must not
		// stall the build worker. A clone is passed so the notifier never races
		// with later writes to the live job.
		go lb.sendNotification(job.Clone())
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
	buildFeatures := "-userpriv -usersandbox"
	if lb.cfg != nil && lb.cfg.BuildFeatures != "" {
		buildFeatures = lb.cfg.BuildFeatures
	}
	buildFeaturesLine := ""
	if strings.TrimSpace(buildFeatures) != "" {
		buildFeaturesLine = fmt.Sprintf("FEATURES=\"${FEATURES} %s\"", buildFeatures)
	}
	gpgSetup := ""
	if gpgKeyID != "" {
		features = "buildpkg binpkg-signing gpg-keepalive"
		gpgSetup = fmt.Sprintf(`
# Setup GPG for package signing with the secret key.
export GNUPGHOME=/root/.gnupg
mkdir -p $GNUPGHOME
chmod 700 $GNUPGHOME
# No TTY in the build container: force loopback pinentry and provide the
# lock dir portage's signing wrapper flocks on.
mkdir -p /run/lock
echo "allow-loopback-pinentry" >> $GNUPGHOME/gpg-agent.conf
echo "pinentry-mode loopback" >> $GNUPGHOME/gpg.conf
gpgconf --kill gpg-agent 2>/dev/null || true

# Import secret key for signing (required for binpkg-signing).
if [ -f /gpg-keys/secret.asc ]; then
    gpg --batch --yes --import /gpg-keys/secret.asc 2>/dev/null || true
    echo "GPG secret key imported for signing"
fi
if [ -f /gpg-keys/public.asc ]; then
    gpg --batch --yes --import /gpg-keys/public.asc 2>/dev/null || true
fi
echo -e "5\ny\n" | gpg --batch --yes --command-fd 0 --edit-key "%s" trust quit 2>/dev/null || true

# Configure portage for GPG signing in the now-writable make.conf. Neutralize
# the trust helper HERE (make.conf, not env — portage reads it from config):
# its default is getuto, which portage re-runs as root right before verifying
# and resets /etc/portage/gnupg back to root ownership, breaking the portage
# user's post-sign verification. We pre-build the store ourselves below.
cat >> /etc/portage/make.conf <<'GPGEOF'
BINPKG_FORMAT="gpkg"
BINPKG_GPG_SIGNING_GPG_HOME="/root/.gnupg"
BINPKG_GPG_SIGNING_KEY="%s"
PORTAGE_TRUST_HELPER="/bin/true"
%s
GPGEOF

# Build the trust store portage checks the fresh signature against. getuto
# creates /etc/portage/gnupg (root-owned, seeded with the Gentoo release keys);
# we add our own signing key + ultimate ownertrust into it. With userpriv off
# (see FEATURES above) the post-sign verification runs as root and uses this
# root-owned store directly — do NOT chown it to portage, which would make the
# root verifier hit "unsafe ownership". PORTAGE_TRUST_HELPER is neutralized so
# portage does not rebuild the store (dropping our key) right before verifying.
if command -v getuto >/dev/null 2>&1; then
    getuto 2>/dev/null || true
fi
mkdir -p /etc/portage/gnupg && chmod 700 /etc/portage/gnupg
if [ -f /gpg-keys/public.asc ]; then
    gpg --homedir /etc/portage/gnupg --batch --yes --import /gpg-keys/public.asc 2>/dev/null || true
    gpg --homedir /etc/portage/gnupg --with-colons --list-keys 2>/dev/null | awk -F: '/^fpr:/{print $10":6:"}' | gpg --homedir /etc/portage/gnupg --batch --yes --import-ownertrust 2>/dev/null || true
fi
`, gpgKeyID, gpgKeyID, buildFeaturesLine)
	}

	// Build emerge command with automatic dependency conflict resolution
	emergeOpts := "--usepkg=n --autounmask --autounmask-write --autounmask-continue --backtrack=50"

	return fmt.Sprintf(`#!/bin/bash
set -e
export USE="%s"
export FEATURES="%s"

# /etc/portage is bind-mounted read-only at /tmp/pconf; copy it to a writable
# /etc/portage so signing config and getuto's trust store can be created.
if [ -d /tmp/pconf ]; then
    mkdir -p /etc/portage
    cp -a /tmp/pconf/. /etc/portage/ 2>/dev/null || true
fi
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
cd /var/cache/binpkgs && find . -type f \( -name '*.gpkg.tar' -o -name '*.tbz2' \) | while read -r f; do rel="${f#./}"; mkdir -p "/output/$(dirname "$rel")"; cp "$f" "/output/$rel"; done; cd /
ls -lh /output/
`, useFlags, features, gpgSetup, pkgAtom, emergeOpts, pkgAtom, emergeOpts, pkgAtom)
}

// executeDockerBuild performs the build using Docker container.
func (lb *LocalBuilder) executeDockerBuild(job *BuildJob) error {
	jobWorkDir, err := lb.prepareJobWorkDir(job.ID)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(jobWorkDir) }()

	script := lb.prepareDockerBuildScript(job)
	outputDir := filepath.Join(jobWorkDir, "output")
	_ = os.MkdirAll(outputDir, 0750)

	gpgKeyDir := lb.prepareGPGKeys(jobWorkDir)
	args := lb.buildDockerArgs(outputDir, gpgKeyDir)
	args = append(args, lb.dockerImage, "/bin/bash", "-c", script)

	if err := lb.runDockerBuild(job, args); err != nil {
		return err
	}

	return lb.collectAndUploadArtifact(job, outputDir)
}

// prepareJobWorkDir creates and returns the job-specific work directory.
func (lb *LocalBuilder) prepareJobWorkDir(jobID string) (string, error) {
	jobWorkDir := filepath.Join(lb.workDir, jobID)
	if err := os.MkdirAll(jobWorkDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create work directory: %w", err)
	}
	return jobWorkDir, nil
}

// prepareDockerBuildScript generates the build script for Docker.
func (lb *LocalBuilder) prepareDockerBuildScript(job *BuildJob) string {
	req := job.Request
	pkgAtom := req.PackageName
	if req.Version != "" {
		pkgAtom = fmt.Sprintf("=%s-%s", req.PackageName, req.Version)
	}

	useFlags := buildUseFlagsString(req.UseFlags)
	gpgKeyID := lb.getGPGKeyID()

	return lb.generateBuildScript(pkgAtom, useFlags, gpgKeyID)
}

// buildUseFlagsString constructs the USE flags string.
func buildUseFlagsString(useFlags map[string]string) string {
	var flags string
	for flag, enabled := range useFlags {
		if enabled == "true" || enabled == "1" {
			flags += flag + " "
		} else {
			flags += "-" + flag + " "
		}
	}
	return flags
}

// getGPGKeyID returns the GPG key ID if signing is enabled.
func (lb *LocalBuilder) getGPGKeyID() string {
	if lb.cfg != nil && lb.cfg.GPGEnabled && lb.cfg.GPGKeyID != "" {
		return lb.cfg.GPGKeyID
	}
	return ""
}

// prepareGPGKeys exports GPG keys for container signing.
func (lb *LocalBuilder) prepareGPGKeys(jobWorkDir string) string {
	gpgKeyID := lb.getGPGKeyID()
	if gpgKeyID == "" || lb.signer == nil || !lb.signer.IsEnabled() {
		return ""
	}

	gpgKeyDir := filepath.Join(jobWorkDir, "gpg-keys")
	if err := os.MkdirAll(gpgKeyDir, 0700); err != nil {
		log.Printf("Warning: failed to create GPG key directory: %v", err)
		return ""
	}

	_, _, err := lb.signer.ExportKeyPair(gpgKeyDir)
	if err != nil {
		log.Printf("Warning: failed to export GPG keys: %v", err)
		return ""
	}

	log.Printf("GPG keys exported to %s for container signing", gpgKeyDir)
	return gpgKeyDir
}

// buildDockerArgs constructs the Docker run arguments.
func (lb *LocalBuilder) buildDockerArgs(outputDir, gpgKeyDir string) []string {
	args := []string{"--rm", "-i", "-v", outputDir + ":/output"}

	if gpgKeyDir != "" {
		args = append(args, "-v", gpgKeyDir+":/gpg-keys:ro")
	}

	if lb.cfg != nil {
		args = lb.addPackageManagerMounts(args)
		args = lb.addEnvironmentVars(args)
	} else {
		args = lb.addDefaultGentooMounts(args)
	}

	return args
}

// addPackageManagerMounts adds package manager specific mounts.
func (lb *LocalBuilder) addPackageManagerMounts(args []string) []string {
	mounts := lb.pkgMgr.GetDockerMounts(lb.cfg)
	for _, m := range mounts {
		args = append(args, "-v", m.String())
	}
	return args
}

// addEnvironmentVars adds package manager specific environment variables.
func (lb *LocalBuilder) addEnvironmentVars(args []string) []string {
	envVars := lb.pkgMgr.GetEnvVars(lb.cfg)
	for k, v := range envVars {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	return args
}

// addDefaultGentooMounts adds default Gentoo mounts for backward compatibility.
func (lb *LocalBuilder) addDefaultGentooMounts(args []string) []string {
	return append(args,
		"-v", "/var/db/repos:/var/db/repos:ro",
		"-v", "/etc/portage/make.conf:/etc/portage/make.conf:ro",
		"-v", "/etc/portage/repos.conf:/etc/portage/repos.conf:ro",
	)
}

// runDockerBuild executes the Docker build command.
func (lb *LocalBuilder) runDockerBuild(job *BuildJob, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	output, err := lb.containerRuntime.Run(ctx, args)
	job.setLog(string(output))

	if err != nil {
		log.Printf("Container build failed for job %s: %v", job.ID, err)
		return fmt.Errorf("container build failed: %w", err)
	}

	log.Printf("Container build completed for job %s, output size: %d bytes", job.ID, len(output))
	return nil
}

// collectAndUploadArtifact finds, copies, signs, and uploads the artifact.
func (lb *LocalBuilder) collectAndUploadArtifact(job *BuildJob, outputDir string) error {
	// Ensure filesystem is synced
	_ = exec.Command("sync").Run()
	time.Sleep(10 * time.Second)

	rels, err := lb.waitForArtifacts(outputDir)
	if err != nil {
		return err
	}

	// Copy every produced package into the artifact dir, category preserved.
	for _, rel := range rels {
		dest := filepath.Join(lb.artifactDir, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
			return fmt.Errorf("failed to create artifact dir: %w", err)
		}
		if err := exec.Command("cp", filepath.Join(outputDir, rel), dest).Run(); err != nil {
			return fmt.Errorf("failed to copy artifact %s: %w", rel, err)
		}
	}

	primary := primaryArtifact(rels, job.Request.PackageName, func(rel string) int64 {
		if info, err := os.Stat(filepath.Join(lb.artifactDir, rel)); err == nil {
			return info.Size()
		}
		return 0
	})
	destPath := filepath.Join(lb.artifactDir, primary)

	job.setArtifactURL(destPath)
	job.setArtifacts(rels)

	// In native mode the gpkg is already signed in-emerge (binpkg-signing);
	// detect that rather than adding a redundant detached signature. In docker
	// mode the container signs and the Go signer (if configured) is a fallback.
	if gpkgIsSigned(destPath) {
		if job.Metadata == nil {
			job.Metadata = map[string]interface{}{}
		}
		job.Metadata["signed"] = true
	} else {
		for _, rel := range rels {
			lb.signArtifact(job, filepath.Join(lb.artifactDir, rel))
		}
	}
	lb.uploadArtifact(job, destPath)

	return nil
}

// gpkgIsSigned reports whether a .gpkg.tar carries an embedded OpenPGP
// signature (a *.sig member), i.e. it was produced with binpkg-signing.
func gpkgIsSigned(path string) bool {
	f, err := os.Open(path) // #nosec G304 -- builder's own artifact dir.
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err != nil {
			return false
		}
		if strings.HasSuffix(hdr.Name, ".sig") {
			return true
		}
	}
}

// waitForArtifacts scans the container output dir for every produced binary
// package, returning paths relative to outputDir (category preserved).
func (lb *LocalBuilder) waitForArtifacts(outputDir string) ([]string, error) {
	var rels []string
	for i := 0; i < 10; i++ {
		if i > 0 {
			_ = exec.Command("sync").Run()
			time.Sleep(2 * time.Second)
		}
		rels = rels[:0]
		_ = filepath.Walk(outputDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info.IsDir() {
				return nil
			}
			name := filepath.Base(path)
			if strings.HasSuffix(name, ".gpkg.tar") || strings.HasSuffix(name, ".tbz2") {
				if rel, err := filepath.Rel(outputDir, path); err == nil {
					rels = append(rels, rel)
				}
			}
			return nil
		})
		if len(rels) > 0 {
			return rels, nil
		}
		if i < 4 {
			log.Printf("No artifacts found on attempt %d, retrying...", i+1)
		}
	}
	return nil, fmt.Errorf("no artifacts found in %s", outputDir)
}

// primaryArtifact picks the artifact belonging to the requested package
// (matching "<pn>-<digit>" and, when present, the category directory);
// falls back to the largest file when nothing matches.
func primaryArtifact(rels []string, pkgName string, sizeOf func(string) int64) string {
	if len(rels) == 0 {
		return ""
	}
	category, pn := "", pkgName
	if idx := strings.LastIndex(pkgName, "/"); idx >= 0 {
		category, pn = pkgName[:idx], pkgName[idx+1:]
	}
	var matches []string
	for _, rel := range rels {
		base := filepath.Base(rel)
		if !strings.HasPrefix(base, pn+"-") || len(base) <= len(pn)+1 {
			continue
		}
		if c := base[len(pn)+1]; c < '0' || c > '9' {
			continue // e.g. "jq-extras-1.0" must not match "jq"
		}
		if category != "" && strings.Contains(rel, "/") && !strings.HasPrefix(rel, category+"/") {
			continue
		}
		matches = append(matches, rel)
	}
	pool := matches
	if len(pool) == 0 {
		pool = rels
	}
	best, bestSize := pool[0], int64(-1)
	for _, rel := range pool {
		if s := sizeOf(rel); s > bestSize {
			best, bestSize = rel, s
		}
	}
	return best
}

// signArtifact signs the artifact if a signer is available.
func (lb *LocalBuilder) signArtifact(job *BuildJob, artifactPath string) {
	if lb.signer != nil && lb.signer.IsEnabled() {
		if err := lb.signer.SignPackage(artifactPath); err != nil {
			log.Printf("Warning: failed to sign package: %v", err)
		} else {
			job.Metadata["signed"] = true
			log.Printf("Package signed: %s", artifactPath)
		}
	}
}

// uploadArtifact uploads the artifact to storage if configured.
func (lb *LocalBuilder) uploadArtifact(job *BuildJob, artifactPath string) {
	if lb.storageUpload != nil && lb.storageUpload.IsEnabled() {
		artifactName := filepath.Base(artifactPath)
		remotePath := artifactName
		if err := lb.storageUpload.Upload(artifactPath, remotePath); err != nil {
			log.Printf("Warning: failed to upload artifact to storage: %v", err)
		} else {
			uploadedURL, _ := lb.storageUpload.GetURL(remotePath)
			job.setArtifactURL(uploadedURL)
			job.Metadata["uploaded"] = true
			log.Printf("Artifact uploaded to storage: %s", uploadedURL)
		}
	}
}

// executeNativeBuild performs the build natively using the system package manager.
func (lb *LocalBuilder) executeNativeBuild(job *BuildJob) error {
	jobWorkDir, err := lb.prepareJobWorkDir(job.ID)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(jobWorkDir) }()

	// Build into a per-job PKGDIR so artifact collection sees only this build's
	// packages (the host /var/cache/binpkgs accumulates across jobs).
	pkgDir := filepath.Join(jobWorkDir, "binpkgs")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return err
	}

	pkgAtom, env := lb.prepareNativeBuildEnv(job)
	env = append(env, "PKGDIR="+pkgDir)

	if err := lb.runNativeBuild(job, pkgAtom, env, jobWorkDir); err != nil {
		return err
	}

	// Same collector as the docker path: copy every produced gpkg into the
	// artifact dir (category preserved), pick the requested package as primary.
	return lb.collectAndUploadArtifact(job, pkgDir)
}

// prepareNativeBuildEnv prepares the package atom and environment variables.
func (lb *LocalBuilder) prepareNativeBuildEnv(job *BuildJob) (string, []string) {
	req := job.Request
	pkgAtom := req.PackageName
	if req.Version != "" {
		pkgAtom = fmt.Sprintf("=%s-%s", req.PackageName, req.Version)
	}

	env := os.Environ()

	for k, v := range req.Environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	if lb.cfg != nil {
		pkgEnv := lb.pkgMgr.GetEnvVars(lb.cfg)
		for k, v := range pkgEnv {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	if len(req.UseFlags) > 0 {
		useFlags := buildUseFlagsString(req.UseFlags)
		env = append(env, fmt.Sprintf("USE=%s", useFlags))
	}

	return pkgAtom, env
}

// runNativeBuild executes the native build command.
func (lb *LocalBuilder) runNativeBuild(job *BuildJob, pkgAtom string, env []string, workDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	buildCmd := lb.pkgMgr.BuildCommand(pkgAtom, nil)
	cmd := exec.CommandContext(ctx, buildCmd[0], buildCmd[1:]...)
	cmd.Env = env
	cmd.Dir = workDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("build failed to start: %w", err)
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		job.appendLog(sc.Text() + "\n")
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	return nil
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

	status, artifactURL := job.snapshot()
	if status != "success" {
		return "", fmt.Errorf("job not completed successfully: status=%s", status)
	}

	if artifactURL == "" {
		return "", fmt.Errorf("no artifact available for job: %s", jobID)
	}

	// Check if ArtifactURL is a local file path
	if _, err := os.Stat(artifactURL); err != nil {
		return "", fmt.Errorf("artifact file not found: %s", artifactURL)
	}

	return artifactURL, nil
}

// GetArtifactPathByRel returns the absolute path of one produced artifact,
// validated against the job's recorded artifact list (no path traversal).
func (lb *LocalBuilder) GetArtifactPathByRel(jobID, rel string) (string, error) {
	lb.jobsMutex.RLock()
	job, exists := lb.jobs[jobID]
	lb.jobsMutex.RUnlock()
	if !exists {
		return "", fmt.Errorf("job not found: %s", jobID)
	}
	for _, known := range job.artifactsSnapshot() {
		if known == rel {
			p := filepath.Join(lb.artifactDir, rel)
			if _, err := os.Stat(p); err != nil {
				return "", fmt.Errorf("artifact file not found: %s", rel)
			}
			return p, nil
		}
	}
	return "", fmt.Errorf("artifact %q not produced by job %s", rel, jobID)
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

	status, artifactURL := job.snapshot()
	if status != "success" {
		return nil, fmt.Errorf("job not completed successfully: status=%s", status)
	}

	if artifactURL == "" {
		return nil, fmt.Errorf("no artifact available for job: %s", jobID)
	}

	// Get file info
	fileInfo, err := os.Stat(artifactURL)
	if err != nil {
		return nil, fmt.Errorf("artifact file not found: %s", artifactURL)
	}

	return &ArtifactInfo{
		JobID:       jobID,
		FileName:    filepath.Base(artifactURL),
		FilePath:    artifactURL,
		FileSize:    fileInfo.Size(),
		PackageName: job.Request.PackageName,
		Version:     job.Request.Version,
	}, nil
}

// VerifyInstall proves the freshly built binary package is actually
// installable: a pristine container resolves the atom from the configured
// binhost (--usepkgonly, no source fallback) and installs it. Output is
// returned for the job log; a non-nil error means verification failed.
//
// The portage config is copied into the container (not mounted at
// /etc/portage) so getuto can create /etc/portage/gnupg — a read-only mount
// there breaks portage's trust helper. binpkg-request-signature is also
// subtracted until the signing chain is wired end-to-end.
// verifyInstallNative confirms the freshly built binpkg installs from the
// binhost on a native Gentoo VM (no Docker): it emerges the package into a
// throwaway --root using only binary packages, so signature/trust and
// dependency resolution are exercised without touching the host system.
func (lb *LocalBuilder) verifyInstallNative(pkgAtom, binhostURL string, requireSignature bool) (string, error) {
	root, err := os.MkdirTemp("", "pe-verify-root")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(root) }()

	// Seed the throwaway root with the trust anchors portage needs to verify a
	// signed binpkg: the Gentoo release keys (so its trust helper getuto can
	// initialize the store in the root) and the host's already-configured
	// signing store (which trusts our key). Without this getuto fails because
	// the empty root has no /usr/share/openpgp-keys/gentoo-release.asc.
	seed := fmt.Sprintf(
		"mkdir -p %[1]s/usr/share/openpgp-keys %[1]s/etc/portage && "+
			"cp -a /usr/share/openpgp-keys/. %[1]s/usr/share/openpgp-keys/ 2>/dev/null || true; "+
			"cp -a /etc/portage/gnupg %[1]s/etc/portage/gnupg && "+
			"chown -R nobody:nobody %[1]s/etc/portage/gnupg",
		root)
	if out, err := exec.Command("bash", "-c", seed).CombinedOutput(); err != nil {
		return string(out), fmt.Errorf("failed to seed verify root: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	args := []string{
		"--root=" + root,
		"--usepkgonly=y",
		"--getbinpkg=y",
		"--oneshot",
		"--color=n", "-q",
		"--",
		pkgAtom,
	}
	cmd := exec.CommandContext(ctx, "emerge", args...)
	env := os.Environ()
	if binhostURL != "" {
		env = append(env, "PORTAGE_BINHOST="+binhostURL)
	}
	feature := "-binpkg-request-signature"
	if requireSignature {
		feature = "binpkg-request-signature"
	}
	env = append(env, "FEATURES="+feature)
	cmd.Env = env

	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("native install verification failed: %w", err)
	}
	return string(out), nil
}

// VerifyInstall confirms a freshly built package installs from the binhost:
// natively via a seeded throwaway --root, or in a pristine docker container.
func (lb *LocalBuilder) VerifyInstall(pkgAtom, binhostURL, gpgPubkey string, requireSignature bool) (string, error) {
	// pkgAtom reaches a shell (docker script) and an emerge argv; validate it
	// against the atom allowlist — as SubmitBuild does for builds — so the
	// verify endpoint cannot be used for shell/option injection (it rejects a
	// leading dash and every shell metacharacter).
	if !atomPattern.MatchString(pkgAtom) {
		return "", fmt.Errorf("invalid package atom %q", pkgAtom)
	}
	if lb.cfg == nil || !lb.cfg.UseDocker {
		return lb.verifyInstallNative(pkgAtom, binhostURL, requireSignature)
	}

	reposPath := lb.cfg.PortageReposPath
	if reposPath == "" {
		reposPath = "/var/db/repos"
	}
	confPath := lb.cfg.PortageConfPath
	if confPath == "" {
		confPath = "/etc/portage"
	}

	features := "-binpkg-request-signature"
	if requireSignature {
		features = "binpkg-request-signature"
	}
	args := []string{"--rm",
		"-v", reposPath + ":/var/db/repos:ro",
		"-v", confPath + ":/tmp/pconf:ro",
		"-e", "FEATURES=" + features,
	}
	if binhostURL != "" {
		args = append(args, "-e", "PORTAGE_BINHOST="+binhostURL)
	}

	// The binhost signing pubkey rides in on a bind mount and is imported into
	// the container's portage keyring so signed packages verify.
	keyImport := ""
	if gpgPubkey != "" {
		keyDir, err := os.MkdirTemp("", "pe-verify-key")
		if err != nil {
			return "", fmt.Errorf("failed to stage verify pubkey: %w", err)
		}
		defer func() { _ = os.RemoveAll(keyDir) }()
		keyFile := filepath.Join(keyDir, "pubkey.asc")
		if err := os.WriteFile(keyFile, []byte(gpgPubkey), 0600); err != nil {
			return "", fmt.Errorf("failed to write verify pubkey: %w", err)
		}
		args = append(args, "-v", keyFile+":/tmp/pe-pubkey.asc:ro")
		keyImport = "gpg --homedir /etc/portage/gnupg --batch --yes --import /tmp/pe-pubkey.asc >/dev/null 2>&1 || true; " +
			"gpg --homedir /etc/portage/gnupg --with-colons --list-keys 2>/dev/null | awk -F: '/^fpr:/{print $10\":6:\"}' | gpg --homedir /etc/portage/gnupg --batch --yes --import-ownertrust >/dev/null 2>&1 || true; "
	}

	script := "cp -a /tmp/pconf/. /etc/portage/ 2>/dev/null || true; " +
		"getuto >/dev/null 2>&1 || true; " +
		keyImport +
		"emerge --color=n -q --getbinpkg=y --usepkgonly=y " + pkgAtom
	args = append(args, lb.dockerImage, "sh", "-c", script)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	out, err := lb.containerRuntime.Run(ctx, args)
	if err != nil {
		return string(out), fmt.Errorf("install verification failed: %w", err)
	}
	return string(out), nil
}
