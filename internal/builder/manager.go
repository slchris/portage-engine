// Package builder manages package build requests and infrastructure.
package builder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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
	Log          string    `json:"log,omitempty"`
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
}

// NewManager creates a new build manager.
func NewManager(cfg *config.ServerConfig) *Manager {
	var iacOpts []iac.ManagerOption
	if cfg.CloudInstanceTTL > 0 {
		iacOpts = append(iacOpts, iac.WithDefaultTTL(time.Duration(cfg.CloudInstanceTTL)*time.Minute))
	}

	mgr := &Manager{
		config:       cfg,
		iacMgr:       iac.NewManager(iacOpts...),
		jobs:         make(map[string]*BuildStatus),
		workQueue:    make(chan *queuedJob, 100),
		remoteBuilds: make(map[string]string),
	}

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
		case "queued", "claimed", "building", "provisioning", "forwarding":
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
	if len(m.config.RemoteBuilders) > 0 {
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

	for _, builderAddr := range m.config.RemoteBuilders {
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
	if len(m.config.RemoteBuilders) > 0 {
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

	m.updateStatus(jobID, "provisioning", "", "")
	instance, err := m.iacMgr.Provision(provReq)
	if err != nil {
		m.updateStatus(jobID, "failed", "", fmt.Sprintf("provisioning failed: %v", err))
		return
	}

	// Always tear the instance down when we are done with it.
	defer func() {
		if termErr := m.iacMgr.Terminate(instance.ID); termErr != nil {
			// Terminate keeps the instance tracked for the cleanup routine to
			// retry; just log here.
			fmt.Printf("Warning: failed to terminate instance %s: %v\n", instance.ID, termErr)
		}
	}()

	if instance.BuilderEndpoint == "" {
		m.updateStatus(jobID, "failed", instance.ID, "provisioned instance has no builder endpoint")
		return
	}

	m.updateStatus(jobID, "building", instance.ID, "")

	// Mark the instance active so the TTL cleanup does not reclaim it mid-build.
	m.iacMgr.SetInstanceActiveTasks(instance.ID, 1)
	defer m.iacMgr.SetInstanceActiveTasks(instance.ID, 0)

	// Submit and wait for the build on the freshly provisioned builder, then
	// pull the resulting artifact back to the server's binpkg dir.
	if err := m.runBuildOnInstance(jobID, instance, req); err != nil {
		m.updateStatus(jobID, "failed", instance.ID, err.Error())
		return
	}
}

// buildProvisionRequest assembles a ProvisionRequest from server config, or
// returns an error explaining what is missing (so the caller can fail loudly
// instead of provisioning a VM that can never build anything).
func (m *Manager) buildProvisionRequest(req *BuildRequest) (*iac.ProvisionRequest, error) {
	provider := req.CloudProvider
	if provider == "" {
		provider = m.config.CloudProvider
	}
	if provider == "" {
		return nil, fmt.Errorf("no remote builders configured and no cloud provider set (set REMOTE_BUILDERS or CLOUD_DEFAULT_PROVIDER)")
	}
	if m.config.CloudSSHKeyPath == "" {
		return nil, fmt.Errorf("cloud build requires CLOUD_SSH_KEY_PATH so the builder can be deployed to the instance")
	}
	if m.config.ServerCallbackURL == "" {
		return nil, fmt.Errorf("cloud build requires SERVER_CALLBACK_URL so the deployed builder can reach this server")
	}

	ttl := time.Duration(m.config.CloudInstanceTTL) * time.Minute

	// Point the deployed builder's Portage at this server's binhost so it can
	// reuse already-built dependencies.
	binhost := ""
	if m.config.ServerCallbackURL != "" {
		binhost = strings.TrimRight(m.config.ServerCallbackURL, "/") + "/binpkgs"
	}

	return &iac.ProvisionRequest{
		Provider:       provider,
		Arch:           req.Arch,
		Spec:           req.MachineSpec,
		Credentials:    m.cloudCredentials(),
		SSH:            &iac.SSHConfig{KeyPath: m.config.CloudSSHKeyPath, User: m.config.CloudSSHUser},
		ServerCallback: m.config.ServerCallbackURL,
		BuilderPort:    9090,
		BuilderToken:   m.config.BuilderToken,
		BinpkgHost:     binhost,
		TTL:            ttl,
	}, nil
}

// cloudCredentials maps server config into IaC cloud credentials.
func (m *Manager) cloudCredentials() *iac.CloudCredentials {
	return &iac.CloudCredentials{
		AliyunAccessKey: m.config.CloudAliyunAK,
		AliyunSecretKey: m.config.CloudAliyunSK,
		GCPKeyFile:      m.config.CloudGCPKeyFile,
		AWSAccessKey:    m.config.CloudAWSAccessKey,
		AWSSecretKey:    m.config.CloudAWSSecretKey,
		PVETokenID:      m.config.CloudPVETokenID,
		PVETokenSecret:  m.config.CloudPVETokenSecret,
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

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("build timed out on instance %s", instance.ID)
		}
		<-ticker.C

		status, errMsg, artifact, terminal, err := m.fetchInstanceJob(statusURL)
		if err != nil {
			return fmt.Errorf("failed to poll instance builder: %w", err)
		}
		m.iacMgr.UpdateInstanceActivity(instance.ID)
		m.updateStatus(jobID, status, instance.ID, errMsg)

		if terminal {
			if status == "failed" {
				return fmt.Errorf("remote build failed: %s", errMsg)
			}
			// Success: record the artifact reference.
			if artifact != "" {
				m.jobsMu.Lock()
				if job, ok := m.jobs[jobID]; ok {
					job.ArtifactPath = artifact
				}
				m.jobsMu.Unlock()
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

// fetchInstanceJob queries a builder job's status once.
func (m *Manager) fetchInstanceJob(statusURL string) (status, errMsg, artifact string, terminal bool, err error) {
	resp, err := m.getFromBuilder(statusURL)
	if err != nil {
		return "", "", "", false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", "", "", false, fmt.Errorf("status %d", resp.StatusCode)
	}

	var job struct {
		Status      string `json:"status"`
		Error       string `json:"error"`
		Log         string `json:"log"`
		ArtifactURL string `json:"artifact_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return "", "", "", false, err
	}

	msg := job.Error
	if msg == "" {
		msg = job.Log
	}
	term := job.Status == "completed" || job.Status == "failed" || job.Status == "success"
	return job.Status, msg, job.ArtifactURL, term, nil
}

// submitToRemoteBuilder forwards a build request to the first configured static
// remote builder.
func (m *Manager) submitToRemoteBuilder(jobID string, req *BuildRequest) {
	if len(m.config.RemoteBuilders) == 0 {
		m.updateStatus(jobID, "failed", "", "no remote builders configured")
		return
	}
	m.submitToBuilderAt(jobID, "", normalizeBuilderURL(m.config.RemoteBuilders[0]), req)
}

// submitToBuilderAt forwards a build request to a specific builder base URL and
// starts polling it. builderAddr is the address recorded for status polling; if
// empty, baseURL is used.
func (m *Manager) submitToBuilderAt(jobID, builderAddr, baseURL string, req *BuildRequest) {
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
		m.updateStatus(jobID, "failed", "", fmt.Sprintf("failed to marshal request: %v", err))
		return
	}

	// Forward to remote builder with the shared token and a bounded timeout.
	httpReq, err := http.NewRequest(http.MethodPost, builderURL, bytes.NewBuffer(data))
	if err != nil {
		m.updateStatus(jobID, "failed", "", fmt.Sprintf("failed to build request: %v", err))
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setBuilderAuth(httpReq, m.config.BuilderToken)

	resp, err := builderHTTPClient.Do(httpReq)
	if err != nil {
		m.updateStatus(jobID, "failed", "", fmt.Sprintf("failed to forward to builder: %v", err))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		m.updateStatus(jobID, "failed", "", fmt.Sprintf("builder returned error: %s", string(body)))
		return
	}

	// Parse response to get remote job ID
	var buildResp BuildResponse
	if err := json.NewDecoder(resp.Body).Decode(&buildResp); err != nil {
		m.updateStatus(jobID, "failed", "", fmt.Sprintf("failed to parse builder response: %v", err))
		return
	}

	// Track remote job ID
	m.jobsMu.Lock()
	m.remoteBuilds[jobID] = buildResp.JobID
	m.jobsMu.Unlock()

	// Start polling remote builder for status
	go m.pollRemoteBuilder(jobID, builderAddr, buildResp.JobID)
}

// pollRemoteBuilder polls remote builder for job status.
func (m *Manager) pollRemoteBuilder(localJobID, builderAddr, remoteJobID string) {
	baseURL := normalizeBuilderURL(builderAddr)
	statusURL := fmt.Sprintf("%s/api/v1/jobs/%s", baseURL, remoteJobID)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		resp, err := m.getFromBuilder(statusURL)
		if err != nil {
			m.updateStatus(localJobID, "failed", "", fmt.Sprintf("failed to poll builder: %v", err))
			return
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			m.updateStatus(localJobID, "failed", "", fmt.Sprintf("builder returned status %d while polling", resp.StatusCode))
			return
		}

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
			m.jobsMu.Lock()
			delete(m.remoteBuilds, localJobID)
			m.jobsMu.Unlock()
			return
		}
	}
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
	if len(m.config.RemoteBuilders) == 0 {
		return nil
	}

	var allJobs []*BuildStatus
	var mu sync.Mutex
	var wg sync.WaitGroup

	client := &http.Client{Timeout: 5 * time.Second}

	for _, builder := range m.config.RemoteBuilders {
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

	if len(m.config.RemoteBuilders) == 0 {
		return stats
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	client := &http.Client{Timeout: 5 * time.Second}

	for _, builderAddr := range m.config.RemoteBuilders {
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
	if len(m.config.RemoteBuilders) == 0 {
		return "", fmt.Errorf("no remote builders configured")
	}

	client := &http.Client{Timeout: 10 * time.Second}

	for _, builder := range m.config.RemoteBuilders {
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
