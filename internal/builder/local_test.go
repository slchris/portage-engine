package builder

import (
	"testing"
	"time"

	"github.com/slchris/portage-engine/internal/gpg"
)

// TestNewLocalBuilder tests creating a new LocalBuilder.
func TestNewLocalBuilder(t *testing.T) {
	signer := gpg.NewSigner("/tmp/test-gpg", "test@example.com", false)

	builder := NewLocalBuilder(2, signer, nil)
	if builder == nil {
		t.Fatal("NewLocalBuilder returned nil")
	}

	if builder.workers != 2 {
		t.Errorf("Expected 2 workers, got %d", builder.workers)
	}

	if builder.signer != signer {
		t.Error("Signer not set correctly")
	}

	if builder.executor == nil {
		t.Error("Executor not initialized")
	}

	if builder.dockerExecutor == nil {
		t.Error("Docker executor not initialized")
	}
}

// TestLocalBuilderSubmitBuild tests submitting a build job.
func TestLocalBuilderSubmitBuild(t *testing.T) {
	signer := gpg.NewSigner("/tmp/test-gpg", "test@example.com", false)

	builder := NewLocalBuilder(1, signer, nil)

	req := &LocalBuildRequest{
		PackageName: "dev-lang/python",
		Version:     "3.11.0",
		UseFlags:    map[string]string{"ssl": "enabled"},
		Environment: map[string]string{"ARCH": "amd64"},
	}

	jobID, err := builder.SubmitBuild(req)
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	if jobID == "" {
		t.Error("Expected non-empty job ID")
	}

	// Wait a moment for the job to be queued
	time.Sleep(100 * time.Millisecond)

	// Verify the job exists
	job, err := builder.GetJobStatus(jobID)
	if err != nil {
		t.Fatalf("GetJobStatus failed: %v", err)
	}

	if job.ID != jobID {
		t.Errorf("Expected job ID %s, got %s", jobID, job.ID)
	}

	if job.Request.PackageName != req.PackageName {
		t.Errorf("Expected package %s, got %s", req.PackageName, job.Request.PackageName)
	}
}

// TestLocalBuilderGetJobStatus tests retrieving job status.
func TestLocalBuilderGetJobStatus(t *testing.T) {
	signer := gpg.NewSigner("/tmp/test-gpg", "test@example.com", false)

	builder := NewLocalBuilder(1, signer, nil)

	req := &LocalBuildRequest{
		PackageName: "app-editors/vim",
		Version:     "9.0",
	}

	jobID, err := builder.SubmitBuild(req)
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Wait briefly for the job to be picked up by worker
	time.Sleep(50 * time.Millisecond)

	job, err := builder.GetJobStatus(jobID)
	if err != nil {
		t.Fatalf("GetJobStatus failed: %v", err)
	}

	if job == nil {
		t.Fatal("Expected non-nil job")
	}

	// Note: Not checking job.Status directly to avoid race condition
	// The worker goroutine may be modifying it concurrently
}

// TestLocalBuilderGetJobStatusNotFound tests retrieving non-existent job.
func TestLocalBuilderGetJobStatusNotFound(t *testing.T) {
	signer := gpg.NewSigner("/tmp/test-gpg", "test@example.com", false)

	builder := NewLocalBuilder(1, signer, nil)

	_, err := builder.GetJobStatus("non-existent-job-id")
	if err == nil {
		t.Error("Expected error for non-existent job")
	}
}

// TestLocalBuilderListJobs tests listing all jobs.
func TestLocalBuilderListJobs(t *testing.T) {
	signer := gpg.NewSigner("/tmp/test-gpg", "test@example.com", false)

	builder := NewLocalBuilder(1, signer, nil)

	// Submit multiple jobs
	for i := 0; i < 3; i++ {
		req := &LocalBuildRequest{
			PackageName: "test-package",
			Version:     "1.0",
		}
		_, err := builder.SubmitBuild(req)
		if err != nil {
			t.Fatalf("SubmitBuild failed: %v", err)
		}
	}

	jobs := builder.ListJobs()
	if len(jobs) != 3 {
		t.Errorf("Expected 3 jobs, got %d", len(jobs))
	}
}

// TestLocalBuilderGetStatus tests getting builder status.
func TestLocalBuilderGetStatus(t *testing.T) {
	signer := gpg.NewSigner("/tmp/test-gpg", "test@example.com", false)

	builder := NewLocalBuilder(2, signer, nil)

	status := builder.GetStatus()
	if status == nil {
		t.Fatal("Expected non-nil status")
	}

	if workers, ok := status["workers"].(int); !ok || workers != 2 {
		t.Errorf("Expected 2 workers in status, got %v", status["workers"])
	}

	if _, ok := status["total"]; !ok {
		t.Error("Expected total in status")
	}
}

// TestLocalBuilderGetArtifactPath tests getting artifact path for a job.
func TestLocalBuilderGetArtifactPath(t *testing.T) {
	signer := gpg.NewSigner("/tmp/test-gpg", "test@example.com", false)
	builder := NewLocalBuilder(1, signer, nil)

	// Test non-existent job
	_, err := builder.GetArtifactPath("non-existent-job")
	if err == nil {
		t.Error("Expected error for non-existent job")
	}

	// Submit a job
	req := &LocalBuildRequest{
		PackageName: "test-package",
		Version:     "1.0",
	}
	jobID, err := builder.SubmitBuild(req)
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Job is queued, not completed, should error
	_, err = builder.GetArtifactPath(jobID)
	if err == nil {
		t.Error("Expected error for non-completed job")
	}
}

// TestLocalBuilderGetArtifactInfo tests getting artifact info for a job.
func TestLocalBuilderGetArtifactInfo(t *testing.T) {
	signer := gpg.NewSigner("/tmp/test-gpg", "test@example.com", false)
	builder := NewLocalBuilder(1, signer, nil)

	// Test non-existent job
	_, err := builder.GetArtifactInfo("non-existent-job")
	if err == nil {
		t.Error("Expected error for non-existent job")
	}

	// Submit a job
	req := &LocalBuildRequest{
		PackageName: "test-package",
		Version:     "1.0",
	}
	jobID, err := builder.SubmitBuild(req)
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Job is queued, not completed, should error
	_, err = builder.GetArtifactInfo(jobID)
	if err == nil {
		t.Error("Expected error for non-completed job")
	}
}

// TestArtifactInfo tests ArtifactInfo struct.
func TestArtifactInfo(t *testing.T) {
	info := &ArtifactInfo{
		JobID:       "job-123",
		FileName:    "test-1.0.tbz2",
		FilePath:    "/tmp/artifacts/test-1.0.tbz2",
		FileSize:    1024,
		PackageName: "test-package",
		Version:     "1.0",
	}

	if info.JobID != "job-123" {
		t.Errorf("Expected JobID 'job-123', got '%s'", info.JobID)
	}
	if info.FileName != "test-1.0.tbz2" {
		t.Errorf("Expected FileName 'test-1.0.tbz2', got '%s'", info.FileName)
	}
	if info.FileSize != 1024 {
		t.Errorf("Expected FileSize 1024, got %d", info.FileSize)
	}
}
