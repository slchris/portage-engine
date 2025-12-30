// Package builder provides local and remote build capabilities.
package builder

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestJobStore_NewJobStore(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	store, err := NewJobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewJobStore() error = %v", err)
	}

	if store.dataDir != tmpDir {
		t.Errorf("dataDir = %v, want %v", store.dataDir, tmpDir)
	}

	expectedFilename := filepath.Join(tmpDir, "jobs.json")
	if store.filename != expectedFilename {
		t.Errorf("filename = %v, want %v", store.filename, expectedFilename)
	}
}

func TestJobStore_NewJobStoreInvalidDir(t *testing.T) {
	t.Parallel()

	// Create a file where we want a directory
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "notadir")
	if err := os.WriteFile(filePath, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}

	// Try to create a store with a path that can't be a directory
	_, err := NewJobStore(filepath.Join(filePath, "subdir"))
	if err == nil {
		t.Error("NewJobStore() expected error for invalid directory")
	}
}

func TestJobStore_LoadEmpty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	store, err := NewJobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewJobStore() error = %v", err)
	}

	jobs, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(jobs) != 0 {
		t.Errorf("Load() returned %d jobs, want 0", len(jobs))
	}
}

func TestJobStore_SaveAndLoad(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	store, err := NewJobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewJobStore() error = %v", err)
	}

	now := time.Now()
	jobs := map[string]*BuildJob{
		"job1": {
			ID:        "job1",
			Status:    "success",
			StartTime: now,
			EndTime:   now.Add(5 * time.Minute),
			Request: &LocalBuildRequest{
				PackageName: "app-misc/test",
			},
		},
		"job2": {
			ID:        "job2",
			Status:    "failed",
			StartTime: now,
			EndTime:   now.Add(2 * time.Minute),
			Error:     "build error",
			Request: &LocalBuildRequest{
				PackageName: "sys-apps/other",
			},
		},
	}

	if err := store.Save(jobs); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loadedJobs, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(loadedJobs) != 2 {
		t.Fatalf("Load() returned %d jobs, want 2", len(loadedJobs))
	}

	for id, orig := range jobs {
		loaded, exists := loadedJobs[id]
		if !exists {
			t.Errorf("Job %s not found after load", id)
			continue
		}

		if loaded.Status != orig.Status {
			t.Errorf("Job %s Status = %v, want %v", id, loaded.Status, orig.Status)
		}

		if loaded.Request.PackageName != orig.Request.PackageName {
			t.Errorf("Job %s PackageName = %v, want %v", id, loaded.Request.PackageName, orig.Request.PackageName)
		}
	}
}

func TestJobStore_CleanOldJobs(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	store, err := NewJobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewJobStore() error = %v", err)
	}

	now := time.Now()
	oldTime := now.Add(-48 * time.Hour)

	jobs := map[string]*BuildJob{
		"recent": {
			ID:        "recent",
			Status:    "success",
			StartTime: now,
			EndTime:   now,
		},
		"old": {
			ID:        "old",
			Status:    "success",
			StartTime: oldTime,
			EndTime:   oldTime,
		},
		"queued": {
			ID:        "queued",
			Status:    "queued",
			StartTime: oldTime,
		},
		"building": {
			ID:        "building",
			Status:    "building",
			StartTime: oldTime,
		},
	}

	// Clean jobs older than 24 hours
	cleaned, removed := store.CleanOldJobs(jobs, 24*time.Hour)

	if removed != 1 {
		t.Errorf("CleanOldJobs() removed = %d, want 1", removed)
	}

	// Should keep: recent, queued, building (in-progress jobs are always kept)
	if len(cleaned) != 3 {
		t.Errorf("CleanOldJobs() kept %d jobs, want 3", len(cleaned))
	}

	if _, exists := cleaned["recent"]; !exists {
		t.Error("CleanOldJobs() should keep recent job")
	}

	if _, exists := cleaned["queued"]; !exists {
		t.Error("CleanOldJobs() should keep queued job")
	}

	if _, exists := cleaned["building"]; !exists {
		t.Error("CleanOldJobs() should keep building job")
	}

	if _, exists := cleaned["old"]; exists {
		t.Error("CleanOldJobs() should remove old completed job")
	}
}

func TestJobPersister_SaveNow(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	store, err := NewJobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewJobStore() error = %v", err)
	}

	now := time.Now()
	jobs := map[string]*BuildJob{
		"job1": {
			ID:        "job1",
			Status:    "success",
			StartTime: now,
			EndTime:   now,
		},
	}

	getJobsFunc := func() map[string]*BuildJob {
		return jobs
	}

	persister := NewJobPersister(store, getJobsFunc, time.Minute, 0)

	if err := persister.SaveNow(); err != nil {
		t.Fatalf("SaveNow() error = %v", err)
	}

	// Verify the job was saved
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(loaded) != 1 {
		t.Errorf("Load() returned %d jobs, want 1", len(loaded))
	}
}

func TestJobPersister_StartStop(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	store, err := NewJobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewJobStore() error = %v", err)
	}

	now := time.Now()
	var jobsMutex sync.RWMutex
	jobs := map[string]*BuildJob{
		"job1": {
			ID:        "job1",
			Status:    "building",
			StartTime: now,
		},
	}

	getJobsFunc := func() map[string]*BuildJob {
		jobsMutex.RLock()
		defer jobsMutex.RUnlock()
		// Return a deep copy to avoid concurrent modification issues
		result := make(map[string]*BuildJob, len(jobs))
		for k, v := range jobs {
			// Deep copy the job
			jobCopy := *v
			result[k] = &jobCopy
		}
		return result
	}

	persister := NewJobPersister(store, getJobsFunc, 100*time.Millisecond, 0)
	persister.Start()

	// Wait for at least one tick
	time.Sleep(200 * time.Millisecond)

	// Update job status with proper synchronization
	jobsMutex.Lock()
	jobs["job1"].Status = "success"
	jobs["job1"].EndTime = time.Now()
	jobsMutex.Unlock()

	// Wait for the persister to save the updated state
	time.Sleep(200 * time.Millisecond)

	persister.Stop()

	// Verify the final state was saved
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if job, exists := loaded["job1"]; !exists {
		t.Error("Job not found after stop")
	} else if job.Status != "success" {
		t.Errorf("Job status = %v, want success", job.Status)
	}
}

func TestJobStore_SaveJob(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	store, err := NewJobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewJobStore() error = %v", err)
	}

	now := time.Now()
	jobs := map[string]*BuildJob{
		"job1": {
			ID:        "job1",
			Status:    "success",
			StartTime: now,
			EndTime:   now,
		},
	}

	if err := store.SaveJob("job1", jobs["job1"], jobs); err != nil {
		t.Fatalf("SaveJob() error = %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(loaded) != 1 {
		t.Errorf("Load() returned %d jobs, want 1", len(loaded))
	}
}
