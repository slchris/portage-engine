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

// Manager manages build requests and infrastructure provisioning.
type Manager struct {
	config       *config.ServerConfig
	iacMgr       *iac.Manager
	jobs         map[string]*BuildStatus
	jobsMu       sync.RWMutex
	workQueue    chan *BuildRequest
	remoteBuilds map[string]string // jobID -> builderURL
}

// NewManager creates a new build manager.
func NewManager(cfg *config.ServerConfig) *Manager {
	mgr := &Manager{
		config:       cfg,
		iacMgr:       iac.NewManager(),
		jobs:         make(map[string]*BuildStatus),
		workQueue:    make(chan *BuildRequest, 100),
		remoteBuilds: make(map[string]string),
	}

	// Start worker goroutines
	for i := 0; i < cfg.MaxWorkers; i++ {
		go mgr.worker()
	}

	return mgr
}

// SubmitBuild submits a new build request.
func (m *Manager) SubmitBuild(req *BuildRequest) (string, error) {
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

	// Add to work queue
	select {
	case m.workQueue <- req:
		return jobID, nil
	default:
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
	// Check local jobs first
	m.jobsMu.RLock()
	status, exists := m.jobs[jobID]
	m.jobsMu.RUnlock()

	if exists {
		return status, nil
	}

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
		resp, err := client.Get(url)
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

// worker processes build requests from the work queue.
func (m *Manager) worker() {
	for req := range m.workQueue {
		m.processBuild(req)
	}
}

// processBuild processes a single build request.
func (m *Manager) processBuild(req *BuildRequest) {
	// Find job ID for this request
	var jobID string
	m.jobsMu.RLock()
	for id, job := range m.jobs {
		if job.PackageName == req.PackageName && job.Version == req.Version && job.Arch == req.Arch && job.Status == "queued" {
			jobID = id
			break
		}
	}
	m.jobsMu.RUnlock()

	if jobID == "" {
		return
	}

	// Check if we have remote builders configured
	if len(m.config.RemoteBuilders) > 0 {
		// Use remote builder instead of creating cloud instance
		m.submitToRemoteBuilder(jobID, req)
		return
	}

	// No remote builders, use cloud provisioning
	m.updateStatus(jobID, "provisioning", "", "")

	// Provision infrastructure
	provReq := &iac.ProvisionRequest{
		Provider: req.CloudProvider,
		Arch:     req.Arch,
		Spec:     req.MachineSpec,
	}

	instance, err := m.iacMgr.Provision(provReq)
	if err != nil {
		m.updateStatus(jobID, "failed", "", err.Error())
		return
	}

	m.updateStatus(jobID, "building", instance.ID, "")

	// In a real implementation, this would:
	// 1. Connect to the instance
	// 2. Install portage and dependencies
	// 3. Build the package with specified USE flags
	// 4. Upload the binary package to binpkg server
	// 5. Clean up the instance

	// Simulate build time
	time.Sleep(5 * time.Second)

	// Update status to completed
	artifactPath := fmt.Sprintf("/binpkgs/%s/%s-%s-%s.tbz2", req.Arch, req.PackageName, req.Version, req.Arch)
	m.updateStatus(jobID, "completed", instance.ID, "")

	m.jobsMu.Lock()
	if job, exists := m.jobs[jobID]; exists {
		job.ArtifactPath = artifactPath
	}
	m.jobsMu.Unlock()

	// Cleanup instance
	_ = m.iacMgr.Terminate(instance.ID)
}

// submitToRemoteBuilder forwards build request to configured remote builder.
func (m *Manager) submitToRemoteBuilder(jobID string, req *BuildRequest) {
	// Select first available remote builder (round-robin could be added later)
	if len(m.config.RemoteBuilders) == 0 {
		m.updateStatus(jobID, "failed", "", "no remote builders configured")
		return
	}

	builder := m.config.RemoteBuilders[0]
	baseURL := normalizeBuilderURL(builder)
	builderURL := fmt.Sprintf("%s/api/v1/build", baseURL)

	m.updateStatus(jobID, "forwarding", "", "")

	// Convert BuildRequest to LocalBuildRequest format
	localReq := LocalBuildRequest{
		PackageName: req.PackageName, // Already in category/package format from server
		Version:     req.Version,
		UseFlags:    make(map[string]string),
		Environment: make(map[string]string),
	}

	// Convert UseFlags from []string to map[string]string
	for _, flag := range req.UseFlags {
		if strings.HasPrefix(flag, "-") {
			localReq.UseFlags[strings.TrimPrefix(flag, "-")] = "disabled"
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

	// Forward to remote builder
	resp, err := http.Post(builderURL, "application/json", bytes.NewBuffer(data))
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
	go m.pollRemoteBuilder(jobID, builder, buildResp.JobID)
}

// pollRemoteBuilder polls remote builder for job status.
func (m *Manager) pollRemoteBuilder(localJobID, builderAddr, remoteJobID string) {
	baseURL := normalizeBuilderURL(builderAddr)
	statusURL := fmt.Sprintf("%s/api/v1/jobs/%s", baseURL, remoteJobID)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		resp, err := http.Get(statusURL)
		if err != nil {
			m.updateStatus(localJobID, "failed", "", fmt.Sprintf("failed to poll builder: %v", err))
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
		localBuilds = append(localBuilds, job)
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
			resp, err := client.Get(url)
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
		case "building", "provisioning":
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
			resp, err := client.Get(url)
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
	// First check local jobs
	m.jobsMu.RLock()
	status, exists := m.jobs[jobID]
	m.jobsMu.RUnlock()

	if exists {
		return m.formatLocalLogs(status), nil
	}

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
		resp, err := client.Get(url)
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
		case "building", "provisioning":
			runningTasks++
			if job.InstanceID != "" {
				tasksByBuilder[job.InstanceID] = append(tasksByBuilder[job.InstanceID], jobID)
			}
		case "queued":
			queuedTasks++
		}
	}

	builders := []map[string]interface{}{}
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
