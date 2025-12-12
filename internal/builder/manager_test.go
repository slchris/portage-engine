package builder

import (
	"strings"
	"testing"
	"time"

	"github.com/slchris/portage-engine/pkg/config"
)

// TestNewManager tests the creation of a build manager.
func TestNewManager(t *testing.T) {
	cfg := &config.ServerConfig{
		MaxWorkers:    4,
		CloudProvider: "gcp",
	}

	mgr := NewManager(cfg)
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	if mgr.config != cfg {
		t.Error("Config not set correctly")
	}

	if mgr.jobs == nil {
		t.Error("Jobs map is nil")
	}

	if mgr.workQueue == nil {
		t.Error("Work queue is nil")
	}
}

// TestSubmitBuildManager tests submitting a build request.
func TestSubmitBuildManager(t *testing.T) {
	cfg := &config.ServerConfig{
		MaxWorkers: 2,
	}

	mgr := NewManager(cfg)

	req := &BuildRequest{
		PackageName: "dev-lang/python",
		Version:     "3.11",
		Arch:        "amd64",
		UseFlags:    []string{"ssl", "threads"},
	}

	jobID, err := mgr.SubmitBuild(req)
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	if jobID == "" {
		t.Error("Job ID is empty")
	}

	status, err := mgr.GetStatus(jobID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if status.Status != "queued" {
		t.Errorf("Expected status=queued, got %s", status.Status)
	}

	if status.PackageName != "dev-lang/python" {
		t.Errorf("Expected package=dev-lang/python, got %s", status.PackageName)
	}
}

// TestGetStatusNotFound tests getting status of non-existent job.
func TestGetStatusNotFound(t *testing.T) {
	cfg := &config.ServerConfig{
		MaxWorkers: 2,
	}

	mgr := NewManager(cfg)

	_, err := mgr.GetStatus("non-existent-job")
	if err == nil {
		t.Error("Expected error for non-existent job, got nil")
	}
}

// TestListAllBuilds tests listing all build jobs.
func TestListAllBuilds(t *testing.T) {
	cfg := &config.ServerConfig{
		MaxWorkers: 2,
	}

	mgr := NewManager(cfg)

	// Submit a few builds
	req1 := &BuildRequest{
		PackageName: "dev-lang/python",
		Version:     "3.11",
		Arch:        "amd64",
	}
	_, _ = mgr.SubmitBuild(req1)

	req2 := &BuildRequest{
		PackageName: "sys-devel/gcc",
		Version:     "13.2.0",
		Arch:        "amd64",
	}
	_, _ = mgr.SubmitBuild(req2)

	builds := mgr.ListAllBuilds()
	if len(builds) != 2 {
		t.Errorf("Expected 2 builds, got %d", len(builds))
	}
}

// TestGetClusterStatus tests getting cluster status.
func TestGetClusterStatus(t *testing.T) {
	cfg := &config.ServerConfig{
		MaxWorkers: 2,
	}

	mgr := NewManager(cfg)

	// Submit some builds with different statuses
	req := &BuildRequest{
		PackageName: "dev-lang/python",
		Version:     "3.11",
		Arch:        "amd64",
	}
	jobID, _ := mgr.SubmitBuild(req)

	// Give worker a moment to pick up the job
	time.Sleep(100 * time.Millisecond)

	status := mgr.GetClusterStatus()
	if status == nil {
		t.Fatal("GetClusterStatus returned nil")
	}

	if status.TotalBuilds == 0 {
		t.Error("Expected TotalBuilds > 0")
	}

	// Verify job exists in the system
	_, err := mgr.GetStatus(jobID)
	if err != nil {
		t.Errorf("Job not found: %v", err)
	}
}

// TestBuildRequest tests BuildRequest structure.
func TestBuildRequest(t *testing.T) {
	req := &BuildRequest{
		PackageName:   "dev-lang/python",
		Version:       "3.11",
		Arch:          "amd64",
		UseFlags:      []string{"ssl", "threads"},
		CloudProvider: "gcp",
		MachineSpec: map[string]string{
			"machine_type": "n1-standard-4",
			"zone":         "us-central1-a",
		},
	}

	if req.PackageName != "dev-lang/python" {
		t.Errorf("Expected PackageName=dev-lang/python, got %s", req.PackageName)
	}

	if len(req.UseFlags) != 2 {
		t.Errorf("Expected 2 USE flags, got %d", len(req.UseFlags))
	}

	if req.MachineSpec["machine_type"] != "n1-standard-4" {
		t.Errorf("Expected machine_type=n1-standard-4, got %s", req.MachineSpec["machine_type"])
	}
}

// TestGetBuildLogs tests retrieving build logs.
func TestGetBuildLogs(t *testing.T) {
	cfg := &config.ServerConfig{
		MaxWorkers: 2,
	}

	mgr := NewManager(cfg)

	req := &BuildRequest{
		PackageName: "dev-lang/python",
		Version:     "3.11",
		Arch:        "amd64",
	}

	jobID, err := mgr.SubmitBuild(req)
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	logs, err := mgr.GetBuildLogs(jobID)
	if err != nil {
		t.Fatalf("GetBuildLogs failed: %v", err)
	}

	if logs == "" {
		t.Error("Logs should not be empty")
	}

	// Check that logs contain expected information
	expectedStrings := []string{
		"Build Job:",
		"Package: dev-lang/python-3.11",
		"Architecture: amd64",
		"Status:",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(logs, expected) {
			t.Errorf("Expected logs to contain '%s'", expected)
		}
	}
}

// TestGetBuildLogsNotFound tests retrieving logs for non-existent job.
func TestGetBuildLogsNotFound(t *testing.T) {
	cfg := &config.ServerConfig{
		MaxWorkers: 2,
	}

	mgr := NewManager(cfg)

	_, err := mgr.GetBuildLogs("non-existent-job")
	if err == nil {
		t.Error("Expected error for non-existent job, got nil")
	}
}

// TestGetSchedulerStatus tests retrieving scheduler status.
func TestGetSchedulerStatus(t *testing.T) {
	cfg := &config.ServerConfig{
		MaxWorkers: 0, // Disable automatic processing
	}

	mgr := NewManager(cfg)

	// Submit some builds
	req1 := &BuildRequest{
		PackageName: "dev-lang/python",
		Version:     "3.11",
		Arch:        "amd64",
	}
	jobID1, _ := mgr.SubmitBuild(req1)

	req2 := &BuildRequest{
		PackageName: "dev-lang/ruby",
		Version:     "3.2",
		Arch:        "amd64",
	}
	_, _ = mgr.SubmitBuild(req2)

	// Simulate one job being assigned to a builder
	mgr.jobsMu.Lock()
	if job, exists := mgr.jobs[jobID1]; exists {
		job.Status = "building"
		job.InstanceID = "builder-1"
	}
	mgr.jobsMu.Unlock()

	status := mgr.GetSchedulerStatus()
	if status == nil {
		t.Fatal("GetSchedulerStatus returned nil")
	}

	builders, ok := status["builders"].([]map[string]interface{})
	if !ok {
		t.Fatal("Expected builders to be an array")
	}

	queuedTasks, ok := status["queued_tasks"].(int)
	if !ok {
		t.Fatal("Expected queued_tasks to be an int")
	}

	runningTasks, ok := status["running_tasks"].(int)
	if !ok {
		t.Fatal("Expected running_tasks to be an int")
	}

	if runningTasks != 1 {
		t.Errorf("Expected 1 running task, got %d", runningTasks)
	}

	if queuedTasks != 1 {
		t.Errorf("Expected 1 queued task, got %d", queuedTasks)
	}

	if len(builders) != 1 {
		t.Errorf("Expected 1 builder, got %d", len(builders))
	}

	if len(builders) > 0 {
		builder := builders[0]
		if builder["id"] != "builder-1" {
			t.Errorf("Expected builder id=builder-1, got %s", builder["id"])
		}
		tasks, ok := builder["tasks"].([]string)
		if !ok || len(tasks) != 1 {
			t.Errorf("Expected builder to have 1 task")
		}
		if len(tasks) > 0 && tasks[0] != jobID1 {
			t.Errorf("Expected task %s, got %s", jobID1, tasks[0])
		}
	}
}
