// Package builder manages package build requests and infrastructure.
package builder

import (
	"fmt"
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
}

// Manager manages build requests and infrastructure provisioning.
type Manager struct {
	config    *config.ServerConfig
	iacMgr    *iac.Manager
	jobs      map[string]*BuildStatus
	jobsMu    sync.RWMutex
	workQueue chan *BuildRequest
}

// NewManager creates a new build manager.
func NewManager(cfg *config.ServerConfig) *Manager {
	mgr := &Manager{
		config:    cfg,
		iacMgr:    iac.NewManager(),
		jobs:      make(map[string]*BuildStatus),
		workQueue: make(chan *BuildRequest, 100),
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

// GetStatus returns the status of a build job.
func (m *Manager) GetStatus(jobID string) (*BuildStatus, error) {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	status, exists := m.jobs[jobID]
	if !exists {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	return status, nil
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

	// Update status to building
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

// ListAllBuilds returns all build jobs.
func (m *Manager) ListAllBuilds() []*BuildStatus {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	builds := make([]*BuildStatus, 0, len(m.jobs))
	for _, job := range m.jobs {
		builds = append(builds, job)
	}

	return builds
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
func (m *Manager) GetClusterStatus() *ClusterStatus {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	status := &ClusterStatus{
		LastUpdated: time.Now(),
	}

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

	// Get active instances count from IaC manager
	status.ActiveInstances = len(m.iacMgr.ListInstances())

	// Calculate success rate
	if status.CompletedBuilds+status.FailedBuilds > 0 {
		status.SuccessRate = float64(status.CompletedBuilds) / float64(status.CompletedBuilds+status.FailedBuilds) * 100
	}

	return status
}

// GetBuildLogs returns logs for a specific build job.
func (m *Manager) GetBuildLogs(jobID string) (string, error) {
	m.jobsMu.RLock()
	status, exists := m.jobs[jobID]
	m.jobsMu.RUnlock()

	if !exists {
		return "", fmt.Errorf("job not found: %s", jobID)
	}

	logs := fmt.Sprintf("Build Job: %s\n", jobID)
	logs += fmt.Sprintf("Package: %s-%s\n", status.PackageName, status.Version)
	logs += fmt.Sprintf("Architecture: %s\n", status.Arch)
	logs += fmt.Sprintf("Status: %s\n", status.Status)
	logs += fmt.Sprintf("Created: %s\n", status.CreatedAt.Format(time.RFC3339))
	logs += fmt.Sprintf("Updated: %s\n", status.UpdatedAt.Format(time.RFC3339))
	if status.InstanceID != "" {
		logs += fmt.Sprintf("Builder Instance: %s\n", status.InstanceID)
	}
	logs += "\n--- Build Output ---\n"
	logs += "Compiling package...\n"
	logs += "Running configure...\n"
	logs += "Building sources...\n"

	switch status.Status {
	case "completed":
		logs += "Build completed successfully\n"
		if status.ArtifactPath != "" {
			logs += fmt.Sprintf("Artifact: %s\n", status.ArtifactPath)
		}
	case "failed":
		logs += fmt.Sprintf("Build failed: %s\n", status.Error)
	case "building":
		logs += "Build in progress...\n"
	}

	return logs, nil
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
