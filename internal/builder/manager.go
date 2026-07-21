// Package builder manages package build requests and infrastructure.
package builder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"mime"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/slchris/portage-engine/internal/iac"
	"github.com/slchris/portage-engine/pkg/config"
)

// BuildRequest represents a package build request.
type BuildRequest struct {
	PackageName   string            `json:"package_name"`
	Version       string            `json:"version"`
	Arch          string            `json:"arch"`
	UseFlags      []string          `json:"use_flags"`
	CloudProvider string            `json:"cloud_provider"`
	MachineSpec   map[string]string `json:"machine_spec"`
	// ConfigBundle, when set, carries the full Portage configuration to build
	// with. It is forwarded verbatim to the remote builder so the build applies
	// the exact USE flags / make.conf / repos the client specified.
	ConfigBundle *ConfigBundle `json:"config_bundle,omitempty"`
}

// BuildResponse represents a build request response.
type BuildResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

// BuildStatus represents the status of a build job.
type BuildStatus struct {
	JobID        string    `json:"job_id"`
	Status       string    `json:"status"`
	PackageName  string    `json:"package_name"`
	Version      string    `json:"version"`
	Arch         string    `json:"arch"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	InstanceID   string    `json:"instance_id,omitempty"`
	Error        string    `json:"error,omitempty"`
	ArtifactPath string    `json:"artifact_path,omitempty"`
	// ArtifactURL is the web path of the stored artifact on the binhost
	// (e.g. /binpkgs/app-misc/jq-1.7-1.gpkg.tar).
	ArtifactURL string `json:"artifact_url,omitempty"`
	// Artifacts lists the web paths of every binpkg stored for this job
	// (dependencies included); ArtifactURL stays the requested package's own.
	Artifacts []string `json:"artifacts,omitempty"`
	// ArtifactPaths mirrors Artifacts with local filesystem paths (cleanup).
	ArtifactPaths []string `json:"-"`
	// Signed reports whether the builder signed the produced packages.
	Signed bool `json:"signed,omitempty"`
	// FailedStage names the pipeline stage a failed job died in
	// (provision/deploy/build/collect/verify), for accurate UI attribution.
	FailedStage string `json:"failed_stage,omitempty"`
	Log         string `json:"log,omitempty"`
}

// queuedJob pairs a build request with the job ID assigned at submission, so a
// worker processes exactly the job it dequeued instead of re-deriving one by
// scanning for a matching package (which let concurrent workers double-process
// one job and strand another).
type queuedJob struct {
	jobID string
	req   *BuildRequest
}

// Manager manages build requests and infrastructure provisioning.
type Manager struct {
	config       *config.ServerConfig
	iacMgr       *iac.Manager
	jobs         map[string]*BuildStatus
	jobsMu       sync.RWMutex
	workQueue    chan *queuedJob
	remoteBuilds map[string]string // jobID -> builderURL
	rrNext       atomic.Uint32     // round-robin cursor over RemoteBuilders

	// onArtifactStored, when set, is called after an artifact lands in the
	// binhost PKGDIR (the server uses it to refresh the Packages index).
	onArtifactStored func()

	// gpgKeyProvider, when set, supplies the binhost signing key material:
	// key ID, armored public key, armored secret key (nil when disabled).
	gpgKeyProvider func() (string, []byte, []byte)

	// cloudSettings is the runtime-adjustable cloud provisioning config,
	// swapped atomically by the settings API. Workers take a snapshot per
	// build, so an update never races an in-flight provision.
	cloudSettings atomic.Pointer[config.CloudSettings]
}

// SetArtifactStoredHook registers a callback invoked after an artifact has
// been stored into the binhost PKGDIR.
func (m *Manager) SetArtifactStoredHook(f func()) {
	m.onArtifactStored = f
}

// SetGPGKeyProvider registers the source of binhost signing key material
// (used for builder deployment, verify-side pubkey import, and mirror upload).
func (m *Manager) SetGPGKeyProvider(f func() (string, []byte, []byte)) {
	m.gpgKeyProvider = f
}

// NewManager creates a new build manager.
func NewManager(cfg *config.ServerConfig) *Manager {
	var iacOpts []iac.ManagerOption
	if cfg.CloudInstanceTTL > 0 {
		iacOpts = append(iacOpts, iac.WithDefaultTTL(time.Duration(cfg.CloudInstanceTTL)*time.Minute))
	}
	if cfg.DataDir != "" {
		// Persist instances so live VMs survive server restarts instead of
		// becoming orphans.
		iacOpts = append(iacOpts, iac.WithStateFile(filepath.Join(cfg.DataDir, "instances.json")))
	}

	mgr := &Manager{
		config:       cfg,
		iacMgr:       iac.NewManager(iacOpts...),
		jobs:         make(map[string]*BuildStatus),
		workQueue:    make(chan *queuedJob, 100),
		remoteBuilds: make(map[string]string),
	}
	mgr.cloudSettings.Store(config.CloudSettingsFromServerConfig(cfg))

	// Start the IaC cleanup routine so expired / orphaned cloud instances are
	// reclaimed (without this, TTL auto-termination never runs and leaked VMs
	// bill forever).
	mgr.iacMgr.StartCleanupRoutine()

	// Start worker goroutines
	for i := 0; i < cfg.MaxWorkers; i++ {
		go mgr.worker()
	}

	return mgr
}

// GetJobsSnapshot returns a copy of all jobs for persistence.
func (m *Manager) GetJobsSnapshot() map[string]*BuildStatus {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	snapshot := make(map[string]*BuildStatus, len(m.jobs))
	for k, v := range m.jobs {
		statusCopy := *v
		snapshot[k] = &statusCopy
	}
	return snapshot
}

// LoadJobs loads a previously persisted set of jobs into the manager.
// Only non-terminal jobs that are not too old are marked as stale.
func (m *Manager) LoadJobs(jobs map[string]*BuildStatus) {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()

	for id, job := range jobs {
		// Mark in-progress AND still-queued jobs as failed-on-restart: we cannot
		// resume in-progress work, and a persisted "queued" job cannot be
		// re-enqueued (BuildStatus does not retain the original request), so
		// leaving it "queued" would strand it forever and inflate QueuedBuilds.
		switch job.Status {
		case "queued", "claimed", "building", "provisioning", "deploying", "verifying", "forwarding":
			job.Status = "failed"
			job.Error = "server restarted before the job completed; please resubmit"
			job.UpdatedAt = time.Now()
		}
		m.jobs[id] = job
	}
}

// Shutdown gracefully shuts down the manager.
// It closes the work queue and waits for IaC cleanup.
func (m *Manager) Shutdown() {
	close(m.workQueue)
	// Give the IaC manager a chance to clean up
	m.iacMgr.StopCleanupRoutine()
}

// SubmitBuild submits a new build request.
func (m *Manager) SubmitBuild(req *BuildRequest) (string, error) {
	// Validate the untrusted package fields early (defense-in-depth: the builder
	// validates again, but rejecting here avoids provisioning/forwarding for a
	// bad request and rejects atom/option injection at the server boundary).
	if !atomPattern.MatchString(req.PackageName) {
		return "", fmt.Errorf("invalid package name %q", req.PackageName)
	}
	if req.Version != "" && !versionPattern.MatchString(req.Version) {
		return "", fmt.Errorf("invalid package version %q", req.Version)
	}
	for _, flag := range req.UseFlags {
		if !useFlagPattern.MatchString(flag) {
			return "", fmt.Errorf("invalid USE flag %q", flag)
		}
	}

	jobID := uuid.New().String()

	status := &BuildStatus{
		JobID:       jobID,
		Status:      "queued",
		PackageName: req.PackageName,
		Version:     req.Version,
		Arch:        req.Arch,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	m.jobsMu.Lock()
	m.jobs[jobID] = status
	m.jobsMu.Unlock()

	// Enqueue the job ID alongside the request so the worker processes exactly
	// this job. If the queue is full, remove the just-added job so it does not
	// linger as a permanently "queued" orphan.
	select {
	case m.workQueue <- &queuedJob{jobID: jobID, req: req}:
		return jobID, nil
	default:
		m.jobsMu.Lock()
		delete(m.jobs, jobID)
		m.jobsMu.Unlock()
		return "", fmt.Errorf("work queue is full")
	}
}

// versionRegex matches version patterns like "3.9.9" or "7.1.0-r1" in artifact filenames.
var versionRegex = regexp.MustCompile(`-(\d+\.\d+(?:\.\d+)?(?:-r\d+)?)-\d+\.gpkg\.tar$`)

// extractVersionFromArtifact tries to extract version from artifact path.
// Example: "/var/tmp/portage-artifacts/screenfetch-3.9.9-1.gpkg.tar" -> "3.9.9"
func extractVersionFromArtifact(artifactPath string) string {
	if artifactPath == "" {
		return ""
	}

	filename := filepath.Base(artifactPath)
	matches := versionRegex.FindStringSubmatch(filename)
	if len(matches) >= 2 {
		return matches[1]
	}

	return ""
}

// GetStatus returns the status of a build job.
// It first checks local jobs, then queries remote builders if not found locally.
func (m *Manager) GetStatus(jobID string) (*BuildStatus, error) {
	// Check local jobs first. Return a copy taken under the lock so callers
	// (e.g. the JSON-encoding status handler) never read the live struct while
	// updateStatus is writing it.
	m.jobsMu.RLock()
	status, exists := m.jobs[jobID]
	if exists {
		statusCopy := *status
		m.jobsMu.RUnlock()
		return &statusCopy, nil
	}
	m.jobsMu.RUnlock()

	// Not found locally, try remote builders
	if len(m.remoteBuilders()) > 0 {
		remoteStatus := m.fetchRemoteJobStatus(jobID)
		if remoteStatus != nil {
			return remoteStatus, nil
		}
	}

	return nil, fmt.Errorf("job not found: %s", jobID)
}

// fetchRemoteJobStatus fetches a specific job's status from remote builders.
func (m *Manager) fetchRemoteJobStatus(jobID string) *BuildStatus {
	client := &http.Client{Timeout: 5 * time.Second}

	for _, builderAddr := range m.remoteBuilders() {
		baseURL := normalizeBuilderURL(builderAddr)
		url := fmt.Sprintf("%s/api/v1/jobs/%s", baseURL, jobID)
		resp, err := m.builderGet(client, url)
		if err != nil {
			continue
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			continue
		}

		var job struct {
			ID      string `json:"id"`
			Request struct {
				PackageName string `json:"package_name"`
				Version     string `json:"version"`
				Arch        string `json:"arch"`
			} `json:"request"`
			Status      string    `json:"status"`
			StartTime   time.Time `json:"start_time"`
			EndTime     time.Time `json:"end_time"`
			Log         string    `json:"log"`
			ArtifactURL string    `json:"artifact_url"`
			Error       string    `json:"error"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
			_ = resp.Body.Close()
			continue
		}
		_ = resp.Body.Close()

		arch := job.Request.Arch
		if arch == "" {
			arch = "amd64" // Default architecture
		}

		version := job.Request.Version
		if version == "" {
			version = extractVersionFromArtifact(job.ArtifactURL)
		}

		status := &BuildStatus{
			JobID:        job.ID,
			PackageName:  job.Request.PackageName,
			Version:      version,
			Arch:         arch,
			Status:       job.Status,
			CreatedAt:    job.StartTime,
			UpdatedAt:    job.EndTime,
			InstanceID:   builderAddr,
			ArtifactPath: job.ArtifactURL,
			Log:          job.Log,
			Error:        job.Error,
		}

		// Normalize status names
		if status.Status == "success" {
			status.Status = "completed"
		}

		return status
	}

	return nil
}

// claimJob atomically transitions a job from "queued" to "claimed" under the
// write lock. It returns true only for the caller that performed the
// transition; a job that is missing or already past "queued" returns false.
func (m *Manager) claimJob(jobID string) bool {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()

	job, ok := m.jobs[jobID]
	if !ok || job.Status != "queued" {
		return false
	}
	job.Status = "claimed"
	job.UpdatedAt = time.Now()
	return true
}

// worker processes queued jobs from the work queue.
func (m *Manager) worker() {
	for qj := range m.workQueue {
		m.processBuild(qj)
	}
}

// processBuild processes a single queued job. It atomically claims the job
// (queued -> claimed) so that even if the same job were enqueued twice, only one
// worker proceeds — no scanning, no double-processing.
func (m *Manager) processBuild(qj *queuedJob) {
	jobID := qj.jobID
	req := qj.req

	if !m.claimJob(jobID) {
		// Already claimed (or gone) — another worker owns it, or it was removed.
		return
	}

	// Prefer a configured static remote builder.
	if len(m.remoteBuilders()) > 0 {
		m.submitToRemoteBuilder(jobID, req)
		return
	}

	// Otherwise provision a cloud builder on demand and run the build there.
	m.processCloudBuild(jobID, req)
}

// processCloudBuild provisions a cloud instance, deploys a builder on it, runs
// the build there, and tears the instance down afterward. It runs synchronously
// in the worker goroutine and always terminates the instance (via defer) so a
// build failure never leaks a billed VM.
func (m *Manager) processCloudBuild(jobID string, req *BuildRequest) {
	provReq, err := m.buildProvisionRequest(req)
	if err != nil {
		// Refuse rather than fabricate success when cloud builds are not
		// configured (previously this path faked a completed build).
		m.updateStatus(jobID, "failed", "", err.Error())
		return
	}

	// Stream provisioning/deployment progress into the job's live log so the
	// dashboard's logs page can be used to follow and debug the whole flow.
	// The first "[deploy]" line flips the job status to "deploying" so the
	// status shown in the UI tracks the pipeline stage.
	var deployingOnce sync.Once
	provReq.LogSink = func(line string) {
		m.appendJobLog(jobID, line)
		if strings.HasPrefix(line, "[deploy]") {
			deployingOnce.Do(func() { m.updateStatus(jobID, "deploying", "", "") })
		}
	}

	// Reuse a warm idle instance when one is available: skipping provision +
	// deploy turns a ~15 minute cold start into a couple of minutes.
	instance := m.iacMgr.AcquireIdleInstance(provReq.Provider, req.Arch)
	if instance != nil && !m.builderHealthy(instance) {
		// A warm instance whose builder does not answer is useless — destroy
		// it and fall through to provisioning a fresh one.
		m.appendJobLog(jobID, fmt.Sprintf("[provision] warm instance %s failed its health check — destroying it and provisioning fresh", instance.ID))
		if termErr := m.iacMgr.Terminate(instance.ID); termErr != nil {
			m.appendJobLog(jobID, fmt.Sprintf("[provision] destroy of unhealthy instance failed (cleanup will retry): %v", termErr))
		}
		instance = nil
	}
	if instance != nil {
		m.appendJobLog(jobID, fmt.Sprintf("[provision] reusing warm instance %s at %s (no provisioning needed)", instance.ID, instance.IPAddress))
		m.appendJobLog(jobID, "[deploy] builder already deployed on the reused instance")
	} else {
		m.updateStatus(jobID, "provisioning", "", "")
		m.appendJobLog(jobID, fmt.Sprintf("[build] provisioning a %s instance for %s…", provReq.Provider, req.PackageName))
		fresh, err := m.iacMgr.Provision(provReq)
		if err != nil {
			stage := "provision"
			if strings.Contains(err.Error(), "deployment failed") {
				stage = "deploy"
			}
			m.setFailedStage(jobID, stage)
			m.appendJobLog(jobID, fmt.Sprintf("[build] provisioning failed: %v", err))
			m.updateStatus(jobID, "failed", "", fmt.Sprintf("provisioning failed: %v", err))
			return
		}
		instance = fresh
		// Mark the fresh instance busy so the TTL cleanup ignores it mid-build.
		m.iacMgr.SetInstanceActiveTasks(instance.ID, 1)
	}

	// On exit, release the instance back to the warm pool instead of
	// destroying it: idle instances serve subsequent builds and the TTL
	// cleanup reclaims them after the configured idle window.
	defer func() {
		m.iacMgr.SetInstanceActiveTasks(instance.ID, 0)
		ttl := "the configured idle TTL"
		if instance.TTL > 0 {
			ttl = instance.TTL.String()
		}
		m.appendJobLog(jobID, fmt.Sprintf("[cleanup] instance %s released to the warm pool (idle for %s before auto-destroy)", instance.ID, ttl))
	}()

	if instance.BuilderEndpoint == "" {
		m.updateStatus(jobID, "failed", instance.ID, "provisioned instance has no builder endpoint")
		return
	}

	// The builder service is (re)started at the very end of the deploy script;
	// on a fast deploy (native Gentoo) it may not have bound its port yet when
	// deploy returns. Wait for /health before submitting, so the first build
	// doesn't race the builder startup with a connection-refused.
	if !m.waitForBuilderReady(jobID, instance) {
		m.setFailedStage(jobID, "deploy")
		m.updateStatus(jobID, "failed", instance.ID, "builder did not become ready after deployment")
		return
	}

	m.updateStatus(jobID, "building", instance.ID, "")
	m.appendJobLog(jobID, "[build] submitting build to the instance builder…")

	// Submit and wait for the build on the builder, then pull the resulting
	// artifact back to the server's binpkg dir.
	if err := m.runBuildOnInstance(jobID, instance, req); err != nil {
		stage := "build"
		if strings.Contains(err.Error(), "artifact retrieval failed") {
			stage = "collect"
		}
		m.setFailedStage(jobID, stage)
		m.updateStatus(jobID, "failed", instance.ID, err.Error())
		return
	}

	// Push the fresh packages (and index/pubkey) to the internal mirror when
	// one is configured; verification then exercises the mirror URL end-to-end.
	verifyBinhost := ""
	if up := newMirrorUploader(m.CloudSettings()); up != nil {
		m.appendJobLog(jobID, "[collect] uploading artifacts to the mirror "+up.base+"…")
		if err := m.uploadJobToMirror(jobID, up); err != nil {
			m.appendJobLog(jobID, "[collect] warning: mirror upload failed (verify falls back to the server binhost): "+err.Error())
		} else {
			verifyBinhost = up.binhostURL()
			m.appendJobLog(jobID, "[collect] mirror binhost ready: "+verifyBinhost)
		}
	}

	// Verify the freshly published binpkg actually installs from the binhost
	// before declaring success (a broken artifact must never sit in the repo
	// marked "success").
	if !m.CloudSettings().SkipVerifyInstall {
		if err := m.verifyOnInstance(jobID, instance, req, verifyBinhost); err != nil {
			m.setFailedStage(jobID, "verify")
			m.removeJobArtifact(jobID)
			m.updateStatus(jobID, "failed", instance.ID, fmt.Sprintf("install verification failed: %v", err))
			return
		}
		m.updateStatus(jobID, "completed", instance.ID, "")
	}
}

// waitForBuilderReady polls the instance builder's /health until it responds
// or a timeout elapses (the builder binds its port a moment after the deploy
// script's systemctl restart returns). Returns false if it never comes up.
func (m *Manager) waitForBuilderReady(jobID string, instance *iac.Instance) bool {
	baseURL := normalizeBuilderURL(instance.BuilderEndpoint)
	for attempt := 0; attempt < 24; attempt++ {
		resp, err := builderHTTPClient.Get(baseURL + "/health")
		if err == nil {
			ok := resp.StatusCode == http.StatusOK
			_ = resp.Body.Close()
			if ok {
				if attempt > 0 {
					m.appendJobLog(jobID, "[deploy] builder is up")
				}
				return true
			}
		}
		if attempt == 0 {
			m.appendJobLog(jobID, "[deploy] waiting for the builder service to come up…")
		}
		time.Sleep(5 * time.Second)
	}
	return false
}

// builderHealthy probes a warm instance's builder, tolerating a short
// restart window (e.g. a just-updated builder binary coming back up).
func (m *Manager) builderHealthy(instance *iac.Instance) bool {
	baseURL := normalizeBuilderURL(instance.BuilderEndpoint)
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(5 * time.Second)
		}
		resp, err := builderHTTPClient.Get(baseURL + "/health")
		if err != nil {
			continue
		}
		ok := resp.StatusCode == http.StatusOK
		_ = resp.Body.Close()
		if ok {
			return true
		}
	}
	return false
}

// verifyOnInstance asks the instance's builder to install the just-built
// package from the binhost in a pristine container.
func (m *Manager) verifyOnInstance(jobID string, instance *iac.Instance, req *BuildRequest, binhostURL string) error {
	m.updateStatus(jobID, "verifying", instance.ID, "")
	from := "the binhost"
	if binhostURL != "" {
		from = binhostURL
	}
	m.appendJobLog(jobID, fmt.Sprintf("[verify] installing %s from %s in a pristine container…", req.PackageName, from))

	pubkey := ""
	if m.gpgKeyProvider != nil {
		if _, pub, _ := m.gpgKeyProvider(); len(pub) > 0 {
			pubkey = string(pub)
		}
	}
	signed := false
	m.jobsMu.RLock()
	if job, ok := m.jobs[jobID]; ok {
		signed = job.Signed
	}
	m.jobsMu.RUnlock()

	baseURL := normalizeBuilderURL(instance.BuilderEndpoint)
	body, _ := json.Marshal(map[string]any{
		"package_name":      req.PackageName,
		"binhost_url":       binhostURL,
		"gpg_pubkey":        pubkey,
		"require_signature": signed && pubkey != "",
	})
	httpReq, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/verify", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setBuilderAuth(httpReq, m.config.BuilderToken)

	resp, err := artifactHTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("verification request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		OK    bool   `json:"ok"`
		Log   string `json:"log"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("verification response invalid (status %d): %w", resp.StatusCode, err)
	}
	if result.Log != "" {
		m.appendJobLog(jobID, "[verify] "+strings.ReplaceAll(strings.TrimSpace(result.Log), "\n", "\n[verify] "))
	}
	if !result.OK {
		return fmt.Errorf("%s", result.Error)
	}
	m.appendJobLog(jobID, "[verify] install verification passed")
	return nil
}

// removeJobArtifact deletes a job's stored artifact from the binhost (used
// when verification fails) and refreshes the index.
func (m *Manager) removeJobArtifact(jobID string) {
	m.jobsMu.Lock()
	var paths, rels []string
	if job, ok := m.jobs[jobID]; ok {
		paths = append(paths, job.ArtifactPaths...)
		if len(paths) == 0 && job.ArtifactPath != "" {
			paths = append(paths, job.ArtifactPath)
		}
		for _, w := range job.Artifacts {
			rels = append(rels, strings.TrimPrefix(w, "/binpkgs/"))
		}
		if len(rels) == 0 && job.ArtifactURL != "" {
			rels = append(rels, strings.TrimPrefix(job.ArtifactURL, "/binpkgs/"))
		}
		job.ArtifactPath = ""
		job.ArtifactURL = ""
		job.ArtifactPaths = nil
		job.Artifacts = nil
	}
	m.jobsMu.Unlock()
	if len(paths) == 0 {
		return
	}
	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			m.appendJobLog(jobID, fmt.Sprintf("[verify] warning: could not remove broken artifact: %v", err))
		}
	}
	m.appendJobLog(jobID, "[verify] broken artifact(s) removed from the binhost")
	if m.onArtifactStored != nil {
		m.onArtifactStored()
	}
	// Mirror copies must go too, and the index must stop referencing them.
	if up := newMirrorUploader(m.CloudSettings()); up != nil {
		if err := up.login(); err != nil {
			m.appendJobLog(jobID, "[verify] warning: mirror cleanup login failed: "+err.Error())
			return
		}
		for _, rel := range rels {
			if err := up.deletePath(rel); err != nil {
				m.appendJobLog(jobID, fmt.Sprintf("[verify] warning: mirror cleanup of %s failed: %v", rel, err))
			}
		}
		if idx := filepath.Join(m.config.BinpkgPath, "Packages"); m.config.BinpkgPath != "" {
			if _, err := os.Stat(idx); err == nil {
				_, _ = up.uploadLocalFile(idx, "")
			}
		}
		m.appendJobLog(jobID, "[verify] broken artifact(s) removed from the mirror")
	}
}

// uploadJobToMirror pushes every stored artifact of a job (plus the Packages
// index and the signing pubkey) to the configured mirror.
func (m *Manager) uploadJobToMirror(jobID string, up *mirrorUploader) error {
	m.jobsMu.RLock()
	var locals, rels []string
	if job, ok := m.jobs[jobID]; ok {
		if len(job.ArtifactPaths) == len(job.Artifacts) {
			locals = append(locals, job.ArtifactPaths...)
			for _, w := range job.Artifacts {
				rels = append(rels, strings.TrimPrefix(w, "/binpkgs/"))
			}
		}
		if len(locals) == 0 && job.ArtifactPath != "" {
			locals = append(locals, job.ArtifactPath)
			rels = append(rels, strings.TrimPrefix(job.ArtifactURL, "/binpkgs/"))
		}
	}
	m.jobsMu.RUnlock()
	if len(locals) == 0 {
		return fmt.Errorf("no artifacts recorded for upload")
	}
	if err := up.login(); err != nil {
		return err
	}
	for i, local := range locals {
		sub := ""
		if j := strings.LastIndex(rels[i], "/"); j >= 0 {
			sub = rels[i][:j]
		}
		url, err := up.uploadLocalFile(local, sub)
		if err != nil {
			return fmt.Errorf("upload %s: %w", rels[i], err)
		}
		m.appendJobLog(jobID, "[collect] uploaded to mirror: "+url)
	}
	if m.config.BinpkgPath != "" {
		idx := filepath.Join(m.config.BinpkgPath, "Packages")
		if _, err := os.Stat(idx); err == nil {
			if _, err := up.uploadLocalFile(idx, ""); err != nil {
				return fmt.Errorf("upload Packages index: %w", err)
			}
			m.appendJobLog(jobID, "[collect] uploaded to mirror: "+up.binhostURL()+"/Packages")
		}
	}
	if m.gpgKeyProvider != nil {
		if keyID, pub, _ := m.gpgKeyProvider(); keyID != "" && len(pub) > 0 {
			if _, err := up.uploadBytes("portage-engine.asc", pub, ""); err != nil {
				m.appendJobLog(jobID, "[collect] warning: pubkey upload failed: "+err.Error())
			} else {
				m.appendJobLog(jobID, "[collect] signing pubkey uploaded: "+up.binhostURL()+"/portage-engine.asc")
			}
		}
	}
	return nil
}

// buildProvisionRequest assembles a ProvisionRequest from server config, or
// returns an error explaining what is missing (so the caller can fail loudly
// instead of provisioning a VM that can never build anything).
func (m *Manager) buildProvisionRequest(req *BuildRequest) (*iac.ProvisionRequest, error) {
	provider := req.CloudProvider
	cs := m.CloudSettings()
	if provider == "" {
		provider = cs.Provider
	}
	if provider == "" {
		return nil, fmt.Errorf("no remote builders configured and no cloud provider set (set REMOTE_BUILDERS or CLOUD_DEFAULT_PROVIDER)")
	}
	if cs.SSHKeyPath == "" {
		return nil, fmt.Errorf("cloud build requires CLOUD_SSH_KEY_PATH so the builder can be deployed to the instance")
	}
	if cs.ServerCallbackURL == "" {
		return nil, fmt.Errorf("cloud build requires SERVER_CALLBACK_URL so the deployed builder can reach this server")
	}
	if cs.BuilderBinaryPath == "" && cs.BuilderBinaryURL == "" {
		// Not fatal: the image/template may ship the builder preinstalled. But
		// if it does not, the instance will never start a builder service.
		fmt.Println("Warning: neither CLOUD_BUILDER_BINARY_PATH nor CLOUD_BUILDER_BINARY_URL is set; " +
			"the instance can only build if its image ships /opt/portage-builder/portage-builder")
	}

	ttl := time.Duration(cs.InstanceTTLMinutes) * time.Minute

	// Point the deployed builder's Portage at a binhost for dependency reuse.
	// Prefer the internal mirror when uploads are configured: build nodes are
	// on the mirror's LAN (the central server may be on a different subnet the
	// nodes cannot route to), and the mirror already serves the signed
	// binpkgs the server publishes there.
	binhost := ""
	if cs.ServerCallbackURL != "" {
		binhost = strings.TrimRight(cs.ServerCallbackURL, "/") + "/binpkgs"
	}
	if cs.UploadURL != "" {
		dir := strings.Trim(cs.UploadDir, "/")
		if dir == "" {
			dir = "portage-engine"
		}
		binhost = strings.TrimRight(cs.UploadURL, "/") + "/local/" + dir
	}

	spec := req.MachineSpec
	switch provider {
	case "pve":
		spec = pveSpecWithDefaults(cs, req.MachineSpec)
	case "gcp":
		spec = gcpSpecWithDefaults(cs, req.MachineSpec)
	case "aws":
		spec = awsSpecWithDefaults(cs, req.MachineSpec)
	}

	preq := &iac.ProvisionRequest{
		Provider:    provider,
		Arch:        req.Arch,
		Spec:        spec,
		Credentials: m.cloudCredentials(cs),
		SSH: &iac.SSHConfig{
			KeyPath:         cs.SSHKeyPath,
			User:            cs.SSHUser,
			KnownHostsPath:  cs.SSHKnownHosts,
			InsecureHostKey: cs.SSHInsecureHostKey,
		},
		ServerCallback:    cs.ServerCallbackURL,
		BuilderPort:       9090,
		BuilderToken:      m.config.BuilderToken,
		BinpkgHost:        binhost,
		TTL:               ttl,
		BuilderBinaryPath: cs.BuilderBinaryPath,
		BuilderBinaryURL:  cs.BuilderBinaryURL,

		AptMirror:            cs.AptMirror,
		DockerDownloadMirror: cs.DockerDownloadMirror,
		DockerRegistryMirror: cs.DockerRegistryMirror,
		GentooMirror:         cs.GentooMirror,
		PortageSyncURI:       cs.PortageSyncURI,
		PortageSyncMethod:    cs.PortageSyncMethod,
		DockerImage:          cs.DockerImage,
		MakeConfExtra:        cs.MakeConfExtra,
		BuildFeatures:        cs.BuildFeatures,
		BuildMode:            cs.BuildMode,
	}
	// Deploy in-emerge signing when explicitly enabled, or always for native
	// Gentoo VMs (where portage's post-sign self-verify actually works). The
	// signing pubkey is still distributed for install verification below.
	if (cs.SignBinpkgs || cs.BuildMode == "native-gentoo") && m.gpgKeyProvider != nil {
		if keyID, _, sec := m.gpgKeyProvider(); keyID != "" && len(sec) > 0 {
			preq.GPGKeyID = keyID
			preq.GPGSecretKey = sec
		}
	}
	return preq, nil
}

// CloudSettings returns the current cloud provisioning settings snapshot.
// Safe for concurrent use; treat the result as read-only. Falls back to
// deriving the snapshot from the static config for Managers constructed
// without NewManager (tests).
func (m *Manager) CloudSettings() *config.CloudSettings {
	if cs := m.cloudSettings.Load(); cs != nil {
		return cs
	}
	cs := config.CloudSettingsFromServerConfig(m.config)
	m.cloudSettings.Store(cs)
	return cs
}

// UpdateCloudSettings atomically replaces the cloud provisioning settings.
// In-flight builds keep the snapshot they started with; subsequent builds use
// the new values. Used by the server's settings API (dashboard-managed config).
func (m *Manager) UpdateCloudSettings(s *config.CloudSettings) {
	m.cloudSettings.Store(s.Clone())
}

// remoteBuilders returns the current static builder list (runtime-adjustable
// via the settings API).
func (m *Manager) remoteBuilders() []string {
	return m.CloudSettings().RemoteBuilders
}

// pveSpecWithDefaults merges the runtime PVE settings into the per-request
// machine spec (request values win), so a plain build request can provision on
// PVE without the client re-sending endpoint/template/storage every time.
// Without this merge the PVE settings (other than the token credentials) would
// never reach the IaC layer.
func pveSpecWithDefaults(cs *config.CloudSettings, reqSpec map[string]string) map[string]string {
	spec := make(map[string]string, len(reqSpec)+6)
	set := func(key, value string) {
		if value != "" {
			spec[key] = value
		}
	}
	set("endpoint", cs.PVEEndpoint)
	set("node", cs.PVENode)
	set("nodes", strings.Join(cs.PVENodes, ","))
	set("storage", cs.PVEStorage)
	set("network", cs.PVENetwork)
	set("template", cs.PVETemplate)
	set("cicustom", cs.PVECICustom)
	set("nameserver", cs.PVENameserver)
	if cs.PVEInsecure {
		spec["insecure"] = "true"
	}
	// A native Gentoo template is a UEFI (ovmf/q35) image; the boot disk clones
	// onto scsi0. Set these unless the request overrides them.
	if cs.BuildMode == "native-gentoo" {
		set("bios", "ovmf")
		set("machine", "q35")
		set("disk_type", "scsi")
	}
	maps.Copy(spec, reqSpec)
	return spec
}

// gcpSpecWithDefaults merges the runtime GCP settings into the per-request
// machine spec (request values win).
func gcpSpecWithDefaults(cs *config.CloudSettings, reqSpec map[string]string) map[string]string {
	spec := make(map[string]string, len(reqSpec)+3)
	set := func(key, value string) {
		if value != "" {
			spec[key] = value
		}
	}
	set("project", cs.GCPProject)
	set("region", cs.GCPRegion)
	set("zone", cs.GCPZone)
	maps.Copy(spec, reqSpec)
	return spec
}

// awsSpecWithDefaults merges the runtime AWS settings into the per-request
// machine spec (request values win).
func awsSpecWithDefaults(cs *config.CloudSettings, reqSpec map[string]string) map[string]string {
	spec := make(map[string]string, len(reqSpec)+2)
	set := func(key, value string) {
		if value != "" {
			spec[key] = value
		}
	}
	set("region", cs.AWSRegion)
	set("zone", cs.AWSZone)
	maps.Copy(spec, reqSpec)
	return spec
}

// cloudCredentials maps configuration into IaC cloud credentials. PVE, GCP,
// and AWS credentials come from the runtime-adjustable settings; Aliyun (a
// non-functional stub provider) remains conf/env-only.
func (m *Manager) cloudCredentials(cs *config.CloudSettings) *iac.CloudCredentials {
	return &iac.CloudCredentials{
		AliyunAccessKey: m.config.CloudAliyunAK,
		AliyunSecretKey: m.config.CloudAliyunSK,
		GCPKeyFile:      cs.GCPKeyFile,
		AWSAccessKey:    cs.AWSAccessKey,
		AWSSecretKey:    cs.AWSSecretKey,
		PVETokenID:      cs.PVETokenID,
		PVETokenSecret:  cs.PVETokenSecret,
		PVEUsername:     cs.PVEUsername,
		PVEPassword:     cs.PVEPassword,
	}
}

// runBuildOnInstance submits the build to a provisioned instance's builder and
// blocks until it reaches a terminal state, updating the local job as it goes.
func (m *Manager) runBuildOnInstance(jobID string, instance *iac.Instance, req *BuildRequest) error {
	baseURL := normalizeBuilderURL(instance.BuilderEndpoint)

	remoteJobID, err := m.postBuildToBuilder(baseURL, req)
	if err != nil {
		return fmt.Errorf("failed to submit build to instance: %w", err)
	}

	// Poll until terminal, with an overall timeout tied to the instance TTL.
	deadline := time.Now().Add(2 * time.Hour)
	if instance.TTL > 0 {
		deadline = time.Now().Add(instance.TTL)
	}
	statusURL := fmt.Sprintf("%s/api/v1/jobs/%s", baseURL, remoteJobID)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	lastLogLen := 0
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("build timed out on instance %s", instance.ID)
		}
		<-ticker.C

		snap, err := m.fetchInstanceJob(statusURL)
		if err != nil {
			return fmt.Errorf("failed to poll instance builder: %w", err)
		}
		// Forward the remote build log incrementally into the local job log.
		if len(snap.Log) > lastLogLen {
			m.appendJobLog(jobID, strings.TrimRight(snap.Log[lastLogLen:], "\n"))
			lastLogLen = len(snap.Log)
		}
		m.iacMgr.UpdateInstanceActivity(instance.ID)
		m.updateStatus(jobID, snap.Status, instance.ID, snap.Error)

		if snap.Terminal {
			if snap.Status == "failed" {
				return fmt.Errorf("remote build failed: %s", snap.Error)
			}
			// Success: pull every produced package off the instance into the
			// central binhost BEFORE the VM can go away. The requested
			// package's own file becomes the job's primary artifact;
			// dependencies land alongside it (category preserved).
			if err := m.collectInstanceArtifacts(jobID, baseURL, remoteJobID, req.PackageName, snap); err != nil {
				return fmt.Errorf("build succeeded on instance but artifact retrieval failed: %w", err)
			}
			return nil
		}
	}
}

// postBuildToBuilder submits a build to a builder base URL and returns its job ID.
func (m *Manager) postBuildToBuilder(baseURL string, req *BuildRequest) (string, error) {
	localReq := LocalBuildRequest{
		PackageName:  req.PackageName,
		Version:      req.Version,
		Arch:         req.Arch,
		UseFlags:     make(map[string]string),
		Environment:  make(map[string]string),
		ConfigBundle: req.ConfigBundle,
	}
	for _, flag := range req.UseFlags {
		if name, found := strings.CutPrefix(flag, "-"); found {
			localReq.UseFlags[name] = "disabled"
		} else {
			localReq.UseFlags[flag] = "enabled"
		}
	}

	data, err := json.Marshal(localReq)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/build", bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setBuilderAuth(httpReq, m.config.BuilderToken)

	resp, err := builderHTTPClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("builder returned %d: %s", resp.StatusCode, string(body))
	}

	var buildResp BuildResponse
	if err := json.NewDecoder(resp.Body).Decode(&buildResp); err != nil {
		return "", err
	}
	if buildResp.JobID == "" {
		return "", fmt.Errorf("builder did not return a job id")
	}
	return buildResp.JobID, nil
}

// fetchInstanceJob queries a builder job's status once. buildLog is the remote
// job's full build log so far (streamed into the local job log as a delta).
// remoteJobSnapshot is one poll of a builder-side job.
type remoteJobSnapshot struct {
	Status      string
	Error       string
	Log         string
	ArtifactURL string
	Artifacts   []string
	Signed      bool
	Terminal    bool
}

func (m *Manager) fetchInstanceJob(statusURL string) (*remoteJobSnapshot, error) {
	resp, err := m.getFromBuilder(statusURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var job struct {
		Status      string         `json:"status"`
		Error       string         `json:"error"`
		Log         string         `json:"log"`
		ArtifactURL string         `json:"artifact_url"`
		Artifacts   []string       `json:"artifacts"`
		Metadata    map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, err
	}

	signed, _ := job.Metadata["signed"].(bool)
	return &remoteJobSnapshot{
		Status:      job.Status,
		Error:       job.Error,
		Log:         job.Log,
		ArtifactURL: job.ArtifactURL,
		Artifacts:   job.Artifacts,
		Signed:      signed,
		Terminal:    job.Status == "completed" || job.Status == "failed" || job.Status == "success",
	}, nil
}

// submitToRemoteBuilder forwards a build request to a configured static remote
// builder. Builders are tried round-robin starting from a rotating cursor
// (previously every job went to RemoteBuilders[0], so additional builders
// never received work); if submission to one fails, the next is tried before
// the job is marked failed.
func (m *Manager) submitToRemoteBuilder(jobID string, req *BuildRequest) {
	builders := m.remoteBuilders()
	if len(builders) == 0 {
		m.updateStatus(jobID, "failed", "", "no remote builders configured")
		return
	}

	start := int(m.rrNext.Add(1)-1) % len(builders)
	var lastErr error
	for i := 0; i < len(builders); i++ {
		builderURL := normalizeBuilderURL(builders[(start+i)%len(builders)])
		if err := m.submitToBuilderAt(jobID, "", builderURL, req); err != nil {
			lastErr = err
			fmt.Printf("Warning: build %s submission to builder %s failed: %v\n", jobID, builderURL, err)
			continue
		}
		return
	}
	m.updateStatus(jobID, "failed", "", fmt.Sprintf("all %d remote builder(s) rejected the build, last error: %v", len(builders), lastErr))
}

// submitToBuilderAt forwards a build request to a specific builder base URL and
// starts polling it. builderAddr is the address recorded for status polling; if
// empty, baseURL is used. Submission failures are returned (not written to the
// job) so the caller can try another builder before giving up.
func (m *Manager) submitToBuilderAt(jobID, builderAddr, baseURL string, req *BuildRequest) error {
	if builderAddr == "" {
		builderAddr = baseURL
	}
	builderURL := fmt.Sprintf("%s/api/v1/build", baseURL)

	m.updateStatus(jobID, "forwarding", "", "")

	// Convert BuildRequest to LocalBuildRequest format
	localReq := LocalBuildRequest{
		PackageName:  req.PackageName, // Already in category/package format from server
		Version:      req.Version,
		Arch:         req.Arch,
		UseFlags:     make(map[string]string),
		Environment:  make(map[string]string),
		ConfigBundle: req.ConfigBundle, // Forward the full config bundle when present.
	}

	// Convert UseFlags from []string to map[string]string
	for _, flag := range req.UseFlags {
		if name, found := strings.CutPrefix(flag, "-"); found {
			localReq.UseFlags[name] = "disabled"
		} else {
			localReq.UseFlags[flag] = "enabled"
		}
	}

	// Marshal request
	data, err := json.Marshal(localReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Forward to remote builder with the shared token and a bounded timeout.
	httpReq, err := http.NewRequest(http.MethodPost, builderURL, bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setBuilderAuth(httpReq, m.config.BuilderToken)

	resp, err := builderHTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to forward to builder: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("builder returned error: %s", string(body))
	}

	// Parse response to get remote job ID
	var buildResp BuildResponse
	if err := json.NewDecoder(resp.Body).Decode(&buildResp); err != nil {
		return fmt.Errorf("failed to parse builder response: %w", err)
	}

	// Track remote job ID
	m.jobsMu.Lock()
	m.remoteBuilds[jobID] = buildResp.JobID
	m.jobsMu.Unlock()

	// Start polling remote builder for status
	go m.pollRemoteBuilder(jobID, builderAddr, buildResp.JobID)
	return nil
}

// maxConsecutivePollFailures is how many poll attempts in a row may fail
// before a remote build is declared lost. A single transient network blip or
// builder restart must not permanently fail a build that is still running.
const maxConsecutivePollFailures = 6

// pollRemoteBuilder polls remote builder for job status.
func (m *Manager) pollRemoteBuilder(localJobID, builderAddr, remoteJobID string) {
	baseURL := normalizeBuilderURL(builderAddr)
	statusURL := fmt.Sprintf("%s/api/v1/jobs/%s", baseURL, remoteJobID)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	failures := 0
	for range ticker.C {
		resp, err := m.getFromBuilder(statusURL)
		if err != nil {
			failures++
			if failures >= maxConsecutivePollFailures {
				m.updateStatus(localJobID, "failed", "", fmt.Sprintf("failed to poll builder %d times in a row: %v", failures, err))
				return
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			failures++
			if failures >= maxConsecutivePollFailures {
				m.updateStatus(localJobID, "failed", "", fmt.Sprintf("builder returned status %d while polling (%d consecutive failures)", resp.StatusCode, failures))
				return
			}
			continue
		}
		failures = 0

		var remoteJob struct {
			ID          string    `json:"id"`
			Status      string    `json:"status"`
			Error       string    `json:"error,omitempty"`
			Log         string    `json:"log"`
			ArtifactURL string    `json:"artifact_url"`
			StartTime   time.Time `json:"start_time"`
			EndTime     time.Time `json:"end_time"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&remoteJob); err != nil {
			_ = resp.Body.Close()
			m.updateStatus(localJobID, "failed", "", fmt.Sprintf("failed to parse status: %v", err))
			return
		}
		_ = resp.Body.Close()

		// Update local job with remote status including log
		errorMsg := remoteJob.Error
		if remoteJob.Log != "" {
			errorMsg = remoteJob.Log
		}
		m.updateStatus(localJobID, remoteJob.Status, "", errorMsg)

		// Update artifact path if available
		if remoteJob.ArtifactURL != "" {
			m.jobsMu.Lock()
			if job, exists := m.jobs[localJobID]; exists {
				job.ArtifactPath = remoteJob.ArtifactURL
			}
			m.jobsMu.Unlock()
		}

		// Stop polling if terminal state reached
		if remoteJob.Status == "completed" || remoteJob.Status == "failed" || remoteJob.Status == "success" {
			// On success, pull the artifact into the central binhost so builds
			// from every builder converge into one consumable Packages index.
			// A static builder stays alive, so on failure the remote reference
			// is kept (the artifact proxy can still serve it) and we only warn.
			if remoteJob.Status != "failed" && remoteJob.ArtifactURL != "" {
				if localPath, webPath, err := m.fetchArtifactToBinhost(baseURL, remoteJobID, m.jobPackageName(localJobID), remoteJob.ArtifactURL); err != nil {
					fmt.Printf("Warning: failed to pull artifact for job %s into binhost: %v\n", localJobID, err)
				} else {
					m.jobsMu.Lock()
					if job, exists := m.jobs[localJobID]; exists {
						job.ArtifactPath = localPath
						job.ArtifactURL = webPath
					}
					m.jobsMu.Unlock()
				}
			}
			m.jobsMu.Lock()
			delete(m.remoteBuilds, localJobID)
			m.jobsMu.Unlock()
			return
		}
	}
}

// maxJobLogBytes caps a job's live log so a chatty provision/build cannot grow
// server memory without bound.
const maxJobLogBytes = 2 * 1024 * 1024

// ansiEscapes matches ANSI color/cursor escape sequences so raw tool output
// (terraform, apt, emerge) stays readable in the web UI.
var ansiEscapes = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// appendJobLog appends a line to a job's live log (served by the logs API and
// shown on the dashboard's logs page while provisioning/building runs).
//
// Truncation keeps the HEAD and the TAIL, dropping the middle: earlier stages'
// logs (provision/deploy) must stay visible even when a later stage floods the
// log (a full portage tree sync prints tens of thousands of lines).
func (m *Manager) appendJobLog(jobID, line string) {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return
	}
	line = ansiEscapes.ReplaceAllString(line, "")
	line = strings.ReplaceAll(line, "\r", "")
	job.Log += line + "\n"
	if len(job.Log) > maxJobLogBytes {
		head := job.Log[:maxJobLogBytes/4]
		if i := strings.LastIndexByte(head, '\n'); i > 0 {
			head = head[:i+1]
		}
		tail := job.Log[len(job.Log)-maxJobLogBytes/2:]
		if i := strings.IndexByte(tail, '\n'); i >= 0 {
			tail = tail[i+1:]
		}
		job.Log = head + "[... log truncated: middle omitted ...]\n" + tail
	}
	job.UpdatedAt = time.Now()
}

// ListInstances returns the current cloud instances (provisioning, running,
// or pending destroy-retry) for the dashboard's Build Nodes page.
func (m *Manager) ListInstances() []*iac.Instance {
	return m.iacMgr.ListInstances()
}

// terminalStatus reports whether a job status is final.
func terminalStatus(s string) bool {
	return s == "failed" || s == "completed" || s == "success"
}

// DeleteJob removes a terminal job record. In-flight jobs are refused so a
// running build cannot lose its bookkeeping.
func (m *Manager) DeleteJob(jobID string) error {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}
	if !terminalStatus(job.Status) {
		return fmt.Errorf("job %s is %s; only finished jobs can be deleted", jobID, job.Status)
	}
	delete(m.jobs, jobID)
	return nil
}

// CleanupFailedJobs removes all failed job records and returns the count.
func (m *Manager) CleanupFailedJobs() int {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()
	n := 0
	for id, job := range m.jobs {
		if job.Status == "failed" {
			delete(m.jobs, id)
			n++
		}
	}
	return n
}

// setFailedStage records which pipeline stage a job failed in.
func (m *Manager) setFailedStage(jobID, stage string) {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()
	if job, ok := m.jobs[jobID]; ok {
		job.FailedStage = stage
	}
}

// jobPackageName returns a job's package atom ("category/name"), or "".
func (m *Manager) jobPackageName(jobID string) string {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()
	if job, ok := m.jobs[jobID]; ok {
		return job.PackageName
	}
	return ""
}

// updateStatus updates the status of a build job.
func (m *Manager) updateStatus(jobID, status, instanceID, errorMsg string) {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()

	if job, exists := m.jobs[jobID]; exists {
		job.Status = status
		job.UpdatedAt = time.Now()
		if instanceID != "" {
			job.InstanceID = instanceID
		}
		if errorMsg != "" {
			job.Error = errorMsg
		}
	}
}

// ListAllBuilds returns all build jobs, including those from remote builders.
func (m *Manager) ListAllBuilds() []*BuildStatus {
	m.jobsMu.RLock()
	localBuilds := make([]*BuildStatus, 0, len(m.jobs))
	for _, job := range m.jobs {
		// Copy under the lock so concurrent updateStatus writes don't race the
		// caller's reads / JSON encoding.
		jobCopy := *job
		localBuilds = append(localBuilds, &jobCopy)
	}
	m.jobsMu.RUnlock()

	// Aggregate builds from remote builders
	remoteBuilds := m.fetchRemoteBuilderJobs()

	// Merge local and remote builds, avoiding duplicates
	allBuilds := localBuilds
	localJobIDs := make(map[string]bool)
	for _, job := range localBuilds {
		localJobIDs[job.JobID] = true
	}
	for _, job := range remoteBuilds {
		if !localJobIDs[job.JobID] {
			allBuilds = append(allBuilds, job)
		}
	}

	return allBuilds
}

// fetchRemoteBuilderJobs fetches jobs from all configured remote builders.
func (m *Manager) fetchRemoteBuilderJobs() []*BuildStatus {
	if len(m.remoteBuilders()) == 0 {
		return nil
	}

	var allJobs []*BuildStatus
	var mu sync.Mutex
	var wg sync.WaitGroup

	client := &http.Client{Timeout: 5 * time.Second}

	for _, builder := range m.remoteBuilders() {
		wg.Add(1)
		go func(builderAddr string) {
			defer wg.Done()

			baseURL := normalizeBuilderURL(builderAddr)
			url := fmt.Sprintf("%s/api/v1/jobs", baseURL)
			resp, err := m.builderGet(client, url)
			if err != nil {
				return
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				return
			}

			var jobs []struct {
				ID      string `json:"id"`
				Request struct {
					PackageName string `json:"package_name"`
					Version     string `json:"version"`
					Arch        string `json:"arch"`
				} `json:"request"`
				Status      string    `json:"status"`
				StartTime   time.Time `json:"start_time"`
				EndTime     time.Time `json:"end_time"`
				Log         string    `json:"log"`
				ArtifactURL string    `json:"artifact_url"`
				Error       string    `json:"error"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
				return
			}

			mu.Lock()
			for _, job := range jobs {
				arch := job.Request.Arch
				if arch == "" {
					arch = "amd64"
				}
				version := job.Request.Version
				if version == "" {
					version = extractVersionFromArtifact(job.ArtifactURL)
				}
				status := &BuildStatus{
					JobID:        job.ID,
					PackageName:  job.Request.PackageName,
					Version:      version,
					Arch:         arch,
					Status:       job.Status,
					CreatedAt:    job.StartTime,
					UpdatedAt:    job.EndTime,
					InstanceID:   builderAddr,
					ArtifactPath: job.ArtifactURL,
					Log:          job.Log,
					Error:        job.Error,
				}
				// Normalize status names
				if status.Status == "success" {
					status.Status = "completed"
				}
				allJobs = append(allJobs, status)
			}
			mu.Unlock()
		}(builder)
	}

	wg.Wait()
	return allJobs
}

// ClusterStatus represents the overall cluster status.
type ClusterStatus struct {
	ActiveBuilds    int       `json:"active_builds"`
	QueuedBuilds    int       `json:"queued_builds"`
	ActiveInstances int       `json:"active_instances"`
	TotalBuilds     int       `json:"total_builds"`
	CompletedBuilds int       `json:"completed_builds"`
	FailedBuilds    int       `json:"failed_builds"`
	SuccessRate     float64   `json:"success_rate"`
	LastUpdated     time.Time `json:"last_updated"`
}

// GetClusterStatus returns the current cluster status.
// It aggregates status from local jobs and remote builders.
func (m *Manager) GetClusterStatus() *ClusterStatus {
	status := &ClusterStatus{
		LastUpdated: time.Now(),
	}

	// Count local jobs
	m.jobsMu.RLock()
	for _, job := range m.jobs {
		status.TotalBuilds++
		switch job.Status {
		case "claimed", "building", "provisioning", "forwarding":
			status.ActiveBuilds++
		case "queued":
			status.QueuedBuilds++
		case "completed":
			status.CompletedBuilds++
		case "failed":
			status.FailedBuilds++
		}
	}
	m.jobsMu.RUnlock()

	// Aggregate stats from remote builders
	remoteStats := m.fetchRemoteBuilderStats()
	status.TotalBuilds += remoteStats.TotalBuilds
	status.ActiveBuilds += remoteStats.ActiveBuilds
	status.QueuedBuilds += remoteStats.QueuedBuilds
	status.CompletedBuilds += remoteStats.CompletedBuilds
	status.FailedBuilds += remoteStats.FailedBuilds
	status.ActiveInstances += remoteStats.ActiveInstances

	// Get active instances count from IaC manager
	status.ActiveInstances += len(m.iacMgr.ListInstances())

	// Calculate success rate
	if status.CompletedBuilds+status.FailedBuilds > 0 {
		status.SuccessRate = float64(status.CompletedBuilds) / float64(status.CompletedBuilds+status.FailedBuilds) * 100
	}

	return status
}

// fetchRemoteBuilderStats fetches aggregated stats from all remote builders.
func (m *Manager) fetchRemoteBuilderStats() *ClusterStatus {
	stats := &ClusterStatus{}

	if len(m.remoteBuilders()) == 0 {
		return stats
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	client := &http.Client{Timeout: 5 * time.Second}

	for _, builderAddr := range m.remoteBuilders() {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()

			baseURL := normalizeBuilderURL(addr)
			url := fmt.Sprintf("%s/api/v1/status", baseURL)
			resp, err := m.builderGet(client, url)
			if err != nil {
				return
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				return
			}

			var builderStatus struct {
				Workers   int `json:"workers"`
				Queued    int `json:"queued"`
				Building  int `json:"building"`
				Completed int `json:"completed"`
				Failed    int `json:"failed"`
				Total     int `json:"total"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&builderStatus); err != nil {
				return
			}

			mu.Lock()
			stats.TotalBuilds += builderStatus.Total
			stats.QueuedBuilds += builderStatus.Queued
			stats.ActiveBuilds += builderStatus.Building
			stats.CompletedBuilds += builderStatus.Completed
			stats.FailedBuilds += builderStatus.Failed
			// Count each remote builder with workers as an active instance
			if builderStatus.Workers > 0 {
				stats.ActiveInstances++
			}
			mu.Unlock()
		}(builderAddr)
	}

	wg.Wait()
	return stats
}

// GetBuildLogs returns logs for a specific build job.
func (m *Manager) GetBuildLogs(jobID string) (string, error) {
	// First check local jobs. Copy under the lock so formatLocalLogs never reads
	// the live struct while updateStatus is writing it.
	m.jobsMu.RLock()
	status, exists := m.jobs[jobID]
	if exists {
		statusCopy := *status
		m.jobsMu.RUnlock()
		return m.formatLocalLogs(&statusCopy), nil
	}
	m.jobsMu.RUnlock()

	// Try to fetch from remote builders
	logs, err := m.fetchRemoteBuilderLogs(jobID)
	if err == nil {
		return logs, nil
	}

	return "", fmt.Errorf("job not found: %s", jobID)
}

// formatLocalLogs formats logs for a local job.
func (m *Manager) formatLocalLogs(status *BuildStatus) string {
	logs := fmt.Sprintf("Build Job: %s\n", status.JobID)
	logs += fmt.Sprintf("Package: %s-%s\n", status.PackageName, status.Version)
	logs += fmt.Sprintf("Architecture: %s\n", status.Arch)
	logs += fmt.Sprintf("Status: %s\n", status.Status)
	logs += fmt.Sprintf("Created: %s\n", status.CreatedAt.Format(time.RFC3339))
	logs += fmt.Sprintf("Updated: %s\n", status.UpdatedAt.Format(time.RFC3339))
	if status.InstanceID != "" {
		logs += fmt.Sprintf("Builder Instance: %s\n", status.InstanceID)
	}
	logs += "\n--- Build Output ---\n"

	// Include actual log if available
	if status.Log != "" {
		logs += status.Log
	} else {
		logs += "Compiling package...\n"
		logs += "Running configure...\n"
		logs += "Building sources...\n"
	}

	switch status.Status {
	case "completed", "success":
		logs += "\nBuild completed successfully\n"
		if status.ArtifactPath != "" {
			logs += fmt.Sprintf("Artifact: %s\n", status.ArtifactPath)
		}
	case "failed":
		logs += fmt.Sprintf("\nBuild failed: %s\n", status.Error)
	case "building":
		logs += "\nBuild in progress...\n"
	}

	return logs
}

// fetchRemoteBuilderLogs fetches logs for a job from remote builders.
func (m *Manager) fetchRemoteBuilderLogs(jobID string) (string, error) {
	if len(m.remoteBuilders()) == 0 {
		return "", fmt.Errorf("no remote builders configured")
	}

	client := &http.Client{Timeout: 10 * time.Second}

	for _, builder := range m.remoteBuilders() {
		baseURL := normalizeBuilderURL(builder)
		url := fmt.Sprintf("%s/api/v1/jobs/%s", baseURL, jobID)
		resp, err := m.builderGet(client, url)
		if err != nil {
			continue
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			continue
		}

		var job struct {
			ID      string `json:"id"`
			Request struct {
				PackageName string `json:"package_name"`
				Version     string `json:"version"`
			} `json:"request"`
			Status      string    `json:"status"`
			StartTime   time.Time `json:"start_time"`
			EndTime     time.Time `json:"end_time"`
			Log         string    `json:"log"`
			ArtifactURL string    `json:"artifact_url"`
			Error       string    `json:"error"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
			_ = resp.Body.Close()
			continue
		}
		_ = resp.Body.Close()

		// Format the logs
		logs := fmt.Sprintf("Build Job: %s\n", job.ID)
		logs += fmt.Sprintf("Package: %s\n", job.Request.PackageName)
		logs += fmt.Sprintf("Version: %s\n", job.Request.Version)
		logs += fmt.Sprintf("Status: %s\n", job.Status)
		logs += fmt.Sprintf("Builder: %s\n", builder)
		logs += fmt.Sprintf("Started: %s\n", job.StartTime.Format(time.RFC3339))
		if !job.EndTime.IsZero() {
			logs += fmt.Sprintf("Finished: %s\n", job.EndTime.Format(time.RFC3339))
		}
		logs += "\n--- Build Output ---\n"
		if job.Log != "" {
			logs += job.Log
		}
		if job.Error != "" {
			logs += fmt.Sprintf("\nError: %s\n", job.Error)
		}
		if job.ArtifactURL != "" {
			logs += fmt.Sprintf("\nArtifact: %s\n", job.ArtifactURL)
		}

		return logs, nil
	}

	return "", fmt.Errorf("job not found on any remote builder")
}

// GetSchedulerStatus returns scheduler status with task assignments.
func (m *Manager) GetSchedulerStatus() map[string]interface{} {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	runningTasks := 0
	queuedTasks := 0
	tasksByBuilder := make(map[string][]string)

	for jobID, job := range m.jobs {
		switch job.Status {
		case "claimed", "building", "provisioning", "forwarding":
			runningTasks++
			if job.InstanceID != "" {
				tasksByBuilder[job.InstanceID] = append(tasksByBuilder[job.InstanceID], jobID)
			}
		case "queued":
			queuedTasks++
		}
	}

	builders := make([]map[string]interface{}, 0, len(tasksByBuilder))
	for builderID, tasks := range tasksByBuilder {
		builders = append(builders, map[string]interface{}{
			"id":           builderID,
			"capacity":     4,
			"current_load": len(tasks),
			"enabled":      true,
			"healthy":      true,
			"tasks":        tasks,
		})
	}

	return map[string]interface{}{
		"builders":      builders,
		"queued_tasks":  queuedTasks,
		"running_tasks": runningTasks,
	}
}

// UpdateBuilderHeartbeat updates builder heartbeat information.
func (m *Manager) UpdateBuilderHeartbeat(req *HeartbeatRequest) error {
	if req.BuilderID == "" {
		return fmt.Errorf("builder_id is required")
	}

	// For now, just validate the request
	// In the future, this could update scheduler state
	if req.Status == "" {
		return fmt.Errorf("status is required")
	}

	return nil
}

// normalizeBuilderURL ensures the builder address has the correct URL format.
// It handles cases where the address may or may not include the http:// prefix.
func normalizeBuilderURL(address string) string {
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		return address
	}
	return fmt.Sprintf("http://%s", address)
}

// builderHTTPClient is used for all server→builder calls. Unlike http.DefaultClient
// it has a timeout, so a hung builder cannot block a worker goroutine forever.
var builderHTTPClient = &http.Client{Timeout: 30 * time.Second}

// artifactHTTPClient downloads build artifacts from builders. Binary packages
// are routinely tens to hundreds of MB, so the timeout is much larger than the
// control-plane client's.
var artifactHTTPClient = &http.Client{Timeout: 15 * time.Minute}

// fetchArtifactToBinhost downloads a completed build's artifact from a builder
// into this server's binhost PKGDIR (BINPKG_PATH/<category>/<file>), so
// artifacts from many parallel builders converge into the single Packages
// index emerge clients consume. Returns the local path.
// collectInstanceArtifacts pulls every artifact the remote job produced into
// the binhost and records primary/all on the job. Falls back to the legacy
// single-artifact download when the builder predates artifact lists.
func (m *Manager) collectInstanceArtifacts(jobID, baseURL, remoteJobID, packageName string, snap *remoteJobSnapshot) error {
	if len(snap.Artifacts) == 0 {
		if snap.ArtifactURL == "" {
			return nil
		}
		m.appendJobLog(jobID, "[collect] fetching artifact from the instance into the binhost...")
		localPath, webPath, err := m.fetchArtifactToBinhost(baseURL, remoteJobID, packageName, snap.ArtifactURL)
		if err != nil {
			return err
		}
		m.appendJobLog(jobID, "[collect] artifact stored: "+localPath)
		m.jobsMu.Lock()
		if job, ok := m.jobs[jobID]; ok {
			job.ArtifactPath = localPath
			job.ArtifactURL = webPath
			job.ArtifactPaths = []string{localPath}
			job.Artifacts = []string{webPath}
			job.Signed = snap.Signed
		}
		m.jobsMu.Unlock()
		return nil
	}

	m.appendJobLog(jobID, fmt.Sprintf("[collect] fetching %d artifact(s) from the instance into the binhost...", len(snap.Artifacts)))
	var locals, webs []string
	primaryLocal, primaryWeb := "", ""
	for _, rel := range snap.Artifacts {
		localPath, webPath, err := m.fetchArtifactRelToBinhost(baseURL, remoteJobID, rel)
		if err != nil {
			return err
		}
		m.appendJobLog(jobID, "[collect] artifact stored: "+localPath)
		locals = append(locals, localPath)
		webs = append(webs, webPath)
		if artifactMatchesPackage(rel, packageName) {
			primaryLocal, primaryWeb = localPath, webPath
		}
	}
	if primaryWeb == "" && len(webs) > 0 {
		primaryLocal, primaryWeb = locals[0], webs[0]
	}
	if m.onArtifactStored != nil {
		m.onArtifactStored()
	}
	m.jobsMu.Lock()
	if job, ok := m.jobs[jobID]; ok {
		job.ArtifactPath = primaryLocal
		job.ArtifactURL = primaryWeb
		job.ArtifactPaths = locals
		job.Artifacts = webs
		job.Signed = snap.Signed
	}
	m.jobsMu.Unlock()
	return nil
}

// artifactMatchesPackage reports whether rel (e.g.
// "app-misc/jq-1.8.1-1.gpkg.tar") is the binpkg of the requested atom
// ("app-misc/jq") rather than a dependency that happened to be built.
func artifactMatchesPackage(rel, packageName string) bool {
	category, pn := "", packageName
	if i := strings.LastIndex(packageName, "/"); i >= 0 {
		category, pn = packageName[:i], packageName[i+1:]
	}
	base := rel
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if !strings.HasPrefix(base, pn+"-") || len(base) <= len(pn)+1 {
		return false
	}
	if c := base[len(pn)+1]; c < '0' || c > '9' {
		return false // "jq-extras-1.0" must not match "jq"
	}
	if category != "" && strings.Contains(rel, "/") && !strings.HasPrefix(rel, category+"/") {
		return false
	}
	return true
}

// fetchArtifactRelToBinhost downloads one named artifact of a remote job and
// stores it at BINPKG_PATH/<rel>, preserving the category directory.
func (m *Manager) fetchArtifactRelToBinhost(baseURL, remoteJobID, rel string) (string, string, error) {
	if m.config.BinpkgPath == "" {
		return "", "", fmt.Errorf("BINPKG_PATH is not configured")
	}
	clean := path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	if clean == "." || strings.HasPrefix(clean, "..") || strings.HasPrefix(clean, "/") {
		return "", "", fmt.Errorf("invalid artifact path %q", rel)
	}

	url := fmt.Sprintf("%s/api/v1/artifacts/download/%s?path=%s", baseURL, remoteJobID, neturl.QueryEscape(rel))
	httpReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	setBuilderAuth(httpReq, m.config.BuilderToken)

	resp, err := artifactHTTPClient.Do(httpReq)
	if err != nil {
		return "", "", fmt.Errorf("download artifact: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", "", fmt.Errorf("artifact download returned %d: %s", resp.StatusCode, string(body))
	}

	dest := filepath.Join(m.config.BinpkgPath, filepath.FromSlash(clean))
	destDir := filepath.Dir(dest)
	if err := os.MkdirAll(destDir, 0o755); err != nil { // #nosec G301 -- binhost dirs are served publicly.
		return "", "", fmt.Errorf("create binhost dir: %w", err)
	}
	tmp, err := os.CreateTemp(destDir, ".artifact-*")
	if err != nil {
		return "", "", err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", "", fmt.Errorf("write artifact: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", "", err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil { // #nosec G302 -- binpkgs are served publicly.
		_ = os.Remove(tmpName)
		return "", "", err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return "", "", err
	}
	return dest, "/binpkgs/" + clean, nil
}

func (m *Manager) fetchArtifactToBinhost(baseURL, remoteJobID, packageName, remoteArtifact string) (string, string, error) {
	if m.config.BinpkgPath == "" {
		return "", "", fmt.Errorf("BINPKG_PATH is not configured")
	}

	url := fmt.Sprintf("%s/api/v1/artifacts/download/%s", baseURL, remoteJobID)
	httpReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	setBuilderAuth(httpReq, m.config.BuilderToken)

	resp, err := artifactHTTPClient.Do(httpReq)
	if err != nil {
		return "", "", fmt.Errorf("download artifact: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", "", fmt.Errorf("artifact download returned %d: %s", resp.StatusCode, string(body))
	}

	filename := artifactFilename(resp.Header.Get("Content-Disposition"), remoteArtifact)
	if filename == "" {
		return "", "", fmt.Errorf("cannot determine artifact filename")
	}

	// PKGDIR layout is <category>/<PF>.gpkg.tar; the category comes from the
	// validated package atom.
	category := ""
	if i := strings.IndexByte(packageName, '/'); i > 0 {
		category = packageName[:i]
	}
	destDir := filepath.Join(m.config.BinpkgPath, category)
	if err := os.MkdirAll(destDir, 0o755); err != nil { // #nosec G301 -- binhost dirs must be world-readable for the HTTP file server.
		return "", "", fmt.Errorf("create binhost dir: %w", err)
	}

	// Download to a temp file and rename, so a concurrent index scan never
	// sees a half-written package.
	tmp, err := os.CreateTemp(destDir, ".artifact-*")
	if err != nil {
		return "", "", err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", "", fmt.Errorf("write artifact: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", "", err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil { // #nosec G302 -- binpkgs are served publicly by the binhost.
		_ = os.Remove(tmpName)
		return "", "", err
	}
	dest := filepath.Join(destDir, filename)
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return "", "", err
	}

	if m.onArtifactStored != nil {
		m.onArtifactStored()
	}
	rel := filename
	if category != "" {
		rel = category + "/" + filename
	}
	return dest, "/binpkgs/" + rel, nil
}

// artifactFilename extracts a safe filename from a Content-Disposition header,
// falling back to the basename of the builder-side artifact path.
func artifactFilename(disposition, remoteArtifact string) string {
	if disposition != "" {
		if _, params, err := mime.ParseMediaType(disposition); err == nil {
			if name := filepath.Base(params["filename"]); name != "." && name != string(filepath.Separator) && !strings.HasPrefix(name, ".") {
				return name
			}
		}
	}
	if name := filepath.Base(remoteArtifact); remoteArtifact != "" && name != "." && name != string(filepath.Separator) && !strings.HasPrefix(name, ".") {
		return name
	}
	return ""
}

// authHeader sets the shared builder token on an outbound request when configured.
func setBuilderAuth(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("X-API-Key", token)
	}
}

// getFromBuilder issues an authenticated GET to a builder endpoint.
func (m *Manager) getFromBuilder(url string) (*http.Response, error) {
	return m.builderGet(builderHTTPClient, url)
}

// builderGet issues an authenticated GET using the supplied client.
func (m *Manager) builderGet(client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	setBuilderAuth(req, m.config.BuilderToken)
	return client.Do(req)
}
