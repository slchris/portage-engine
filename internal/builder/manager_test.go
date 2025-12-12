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

// TestGetClusterStatusCalculations tests cluster status calculations.
func TestGetClusterStatusCalculations(t *testing.T) {
	t.Parallel()

	cfg := &config.ServerConfig{
		MaxWorkers: 1,
	}

	mgr := NewManager(cfg)

	// Initially should be all zeros
	status := mgr.GetClusterStatus()
	if status.TotalBuilds != 0 {
		t.Errorf("Expected TotalBuilds=0, got %d", status.TotalBuilds)
	}
	if status.SuccessRate != 0 {
		t.Errorf("Expected SuccessRate=0, got %f", status.SuccessRate)
	}

	// Submit a build
	req := &BuildRequest{
		PackageName: "dev-lang/python",
		Version:     "3.11",
		Arch:        "amd64",
	}
	_, _ = mgr.SubmitBuild(req)

	status = mgr.GetClusterStatus()
	if status.TotalBuilds != 1 {
		t.Errorf("Expected TotalBuilds=1, got %d", status.TotalBuilds)
	}

	// Verify LastUpdated is set
	if status.LastUpdated.IsZero() {
		t.Error("LastUpdated should not be zero")
	}
}

// TestClusterStatusSuccessRate tests success rate calculation.
func TestClusterStatusSuccessRate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		completed   int
		failed      int
		expectedPct float64
	}{
		{"all success", 10, 0, 100.0},
		{"all failed", 0, 10, 0.0},
		{"50-50", 5, 5, 50.0},
		{"no builds", 0, 0, 0.0},
		{"75% success", 3, 1, 75.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completed := tt.completed
			failed := tt.failed
			var rate float64
			if completed+failed > 0 {
				rate = float64(completed) / float64(completed+failed) * 100
			}
			if rate != tt.expectedPct {
				t.Errorf("Expected rate=%f, got %f", tt.expectedPct, rate)
			}
		})
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

// TestUpdateBuilderHeartbeat tests updating builder heartbeat.
func TestUpdateBuilderHeartbeat(t *testing.T) {
	cfg := &config.ServerConfig{
		MaxWorkers: 2,
	}

	mgr := NewManager(cfg)

	tests := []struct {
		name      string
		req       *HeartbeatRequest
		wantError bool
	}{
		{
			name: "valid heartbeat",
			req: &HeartbeatRequest{
				BuilderID:  "builder-1",
				Status:     "healthy",
				Endpoint:   "http://localhost:9090",
				Capacity:   4,
				ActiveJobs: 2,
				Timestamp:  time.Now(),
			},
			wantError: false,
		},
		{
			name: "missing builder_id",
			req: &HeartbeatRequest{
				Status:    "healthy",
				Timestamp: time.Now(),
			},
			wantError: true,
		},
		{
			name: "missing status",
			req: &HeartbeatRequest{
				BuilderID: "builder-1",
				Timestamp: time.Now(),
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mgr.UpdateBuilderHeartbeat(tt.req)
			if (err != nil) != tt.wantError {
				t.Errorf("UpdateBuilderHeartbeat() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// TestUpdateBuilderHeartbeatConcurrent tests concurrent heartbeat updates.
func TestUpdateBuilderHeartbeatConcurrent(_ *testing.T) {
	cfg := &config.ServerConfig{
		MaxWorkers: 2,
	}

	mgr := NewManager(cfg)

	// Send heartbeats from multiple goroutines
	numGoroutines := 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			req := &HeartbeatRequest{
				BuilderID:  "builder-1",
				Status:     "healthy",
				Capacity:   4,
				ActiveJobs: id,
				Timestamp:  time.Now(),
			}
			_ = mgr.UpdateBuilderHeartbeat(req)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

// TestBuildStatusArch tests that Arch field is properly handled in BuildStatus.
func TestBuildStatusArch(t *testing.T) {
	t.Parallel()

	cfg := &config.ServerConfig{
		MaxWorkers: 2,
	}

	mgr := NewManager(cfg)

	tests := []struct {
		name         string
		arch         string
		expectedArch string
	}{
		{
			name:         "explicit amd64",
			arch:         "amd64",
			expectedArch: "amd64",
		},
		{
			name:         "explicit arm64",
			arch:         "arm64",
			expectedArch: "arm64",
		},
		{
			name:         "empty arch",
			arch:         "",
			expectedArch: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &BuildRequest{
				PackageName: "dev-lang/python",
				Version:     "3.11",
				Arch:        tt.arch,
			}

			jobID, err := mgr.SubmitBuild(req)
			if err != nil {
				t.Fatalf("SubmitBuild failed: %v", err)
			}

			status, err := mgr.GetStatus(jobID)
			if err != nil {
				t.Fatalf("GetStatus failed: %v", err)
			}

			if status.Arch != tt.expectedArch {
				t.Errorf("Expected Arch=%q, got %q", tt.expectedArch, status.Arch)
			}
		})
	}
}

// TestLocalBuildRequestArch tests LocalBuildRequest Arch field.
func TestLocalBuildRequestArch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  *LocalBuildRequest
		arch string
	}{
		{
			name: "with arch",
			req: &LocalBuildRequest{
				PackageName: "app-misc/neofetch",
				Version:     "7.1.0",
				Arch:        "amd64",
			},
			arch: "amd64",
		},
		{
			name: "without arch",
			req: &LocalBuildRequest{
				PackageName: "app-misc/neofetch",
				Version:     "7.1.0",
			},
			arch: "",
		},
		{
			name: "arm64 arch",
			req: &LocalBuildRequest{
				PackageName: "app-misc/neofetch",
				Arch:        "arm64",
			},
			arch: "arm64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.req.Arch != tt.arch {
				t.Errorf("Expected Arch=%q, got %q", tt.arch, tt.req.Arch)
			}
		})
	}
}

// TestExtractVersionFromArtifact tests extracting version from artifact path.
func TestExtractVersionFromArtifact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		artifactPath string
		expected     string
	}{
		{
			name:         "standard gpkg format",
			artifactPath: "/var/tmp/portage-artifacts/screenfetch-3.9.9-1.gpkg.tar",
			expected:     "3.9.9",
		},
		{
			name:         "version with revision",
			artifactPath: "/var/tmp/portage-artifacts/neofetch-7.1.0-r1-1.gpkg.tar",
			expected:     "7.1.0-r1",
		},
		{
			name:         "two part version",
			artifactPath: "/var/tmp/portage-artifacts/htop-3.3-1.gpkg.tar",
			expected:     "3.3",
		},
		{
			name:         "complex package name",
			artifactPath: "/var/tmp/portage-artifacts/dev-python-setuptools-69.0.3-1.gpkg.tar",
			expected:     "69.0.3",
		},
		{
			name:         "empty path",
			artifactPath: "",
			expected:     "",
		},
		{
			name:         "no version pattern",
			artifactPath: "/var/tmp/portage-artifacts/somefile.tar",
			expected:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractVersionFromArtifact(tt.artifactPath)
			if result != tt.expected {
				t.Errorf("extractVersionFromArtifact(%q) = %q, want %q", tt.artifactPath, result, tt.expected)
			}
		})
	}
}
