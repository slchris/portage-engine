package builder

import (
	"testing"
	"time"

	"github.com/slchris/portage-engine/internal/gpg"
)

// TestNewLocalBuilder tests creating a new LocalBuilder.
func TestNewLocalBuilder(t *testing.T) {
	signer := gpg.NewSigner("/tmp/test-gpg", "test@example.com", false)

	builder := NewLocalBuilder(2, signer)
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

	builder := NewLocalBuilder(1, signer)

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

	builder := NewLocalBuilder(1, signer)

	req := &LocalBuildRequest{
		PackageName: "app-editors/vim",
		Version:     "9.0",
	}

	jobID, err := builder.SubmitBuild(req)
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	job, err := builder.GetJobStatus(jobID)
	if err != nil {
		t.Fatalf("GetJobStatus failed: %v", err)
	}

	if job == nil {
		t.Fatal("Expected non-nil job")
	}

	if job.Status == "" {
		t.Error("Job status should not be empty")
	}
}

// TestLocalBuilderGetJobStatusNotFound tests retrieving non-existent job.
func TestLocalBuilderGetJobStatusNotFound(t *testing.T) {
	signer := gpg.NewSigner("/tmp/test-gpg", "test@example.com", false)

	builder := NewLocalBuilder(1, signer)

	_, err := builder.GetJobStatus("non-existent-job-id")
	if err == nil {
		t.Error("Expected error for non-existent job")
	}
}

// TestLocalBuilderListJobs tests listing all jobs.
func TestLocalBuilderListJobs(t *testing.T) {
	signer := gpg.NewSigner("/tmp/test-gpg", "test@example.com", false)

	builder := NewLocalBuilder(1, signer)

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

	builder := NewLocalBuilder(2, signer)

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
