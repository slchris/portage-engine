// Package builder provides local and remote build capabilities.
package builder

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// JobStore provides persistent storage for build jobs.
type JobStore struct {
	dataDir  string
	filename string
	mu       sync.RWMutex
}

// persistedJobs represents the structure saved to disk.
type persistedJobs struct {
	Jobs      map[string]*BuildJob `json:"jobs"`
	UpdatedAt time.Time            `json:"updated_at"`
}

// NewJobStore creates a new job store with the specified data directory.
func NewJobStore(dataDir string) (*JobStore, error) {
	if dataDir == "" {
		dataDir = "/var/lib/portage-engine"
	}

	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create data directory %s: %w", dataDir, err)
	}

	// Verify directory is writable
	testFile := filepath.Join(dataDir, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		return nil, fmt.Errorf("data directory %s is not writable: %w", dataDir, err)
	}
	_ = os.Remove(testFile)

	return &JobStore{
		dataDir:  dataDir,
		filename: filepath.Join(dataDir, "jobs.json"),
	}, nil
}

// Load loads persisted jobs from disk.
func (s *JobStore) Load() (map[string]*BuildJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.filename)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*BuildJob), nil
		}
		return nil, fmt.Errorf("failed to read jobs file: %w", err)
	}

	var persisted persistedJobs
	if err := json.Unmarshal(data, &persisted); err != nil {
		// Try to parse as legacy format (just the map)
		var legacyJobs map[string]*BuildJob
		if err := json.Unmarshal(data, &legacyJobs); err != nil {
			return nil, fmt.Errorf("failed to parse jobs file: %w", err)
		}
		return legacyJobs, nil
	}

	if persisted.Jobs == nil {
		return make(map[string]*BuildJob), nil
	}

	return persisted.Jobs, nil
}

// Save saves jobs to disk.
func (s *JobStore) Save(jobs map[string]*BuildJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	persisted := persistedJobs{
		Jobs:      jobs,
		UpdatedAt: time.Now(),
	}

	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal jobs: %w", err)
	}

	// Write to a temp file first, then rename for atomicity
	tempFile := s.filename + ".tmp"
	if err := os.WriteFile(tempFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tempFile, s.filename); err != nil {
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// SaveJob saves a single job update.
func (s *JobStore) SaveJob(_ string, _ *BuildJob, allJobs map[string]*BuildJob) error {
	return s.Save(allJobs)
}

// CleanOldJobs removes jobs older than the specified duration.
func (s *JobStore) CleanOldJobs(jobs map[string]*BuildJob, maxAge time.Duration) (map[string]*BuildJob, int) {
	cutoff := time.Now().Add(-maxAge)
	cleaned := make(map[string]*BuildJob)
	removedCount := 0

	for id, job := range jobs {
		// Keep jobs that are:
		// 1. Still in progress (queued or building)
		// 2. Completed within the retention period
		switch {
		case job.Status == "queued" || job.Status == "building":
			cleaned[id] = job
		case job.EndTime.After(cutoff) || job.EndTime.IsZero():
			cleaned[id] = job
		default:
			removedCount++
		}
	}

	return cleaned, removedCount
}

// JobPersister handles periodic persistence of jobs.
type JobPersister struct {
	store       *JobStore
	getJobsFunc func() map[string]*BuildJob
	interval    time.Duration
	maxAge      time.Duration
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

// NewJobPersister creates a new job persister.
func NewJobPersister(store *JobStore, getJobsFunc func() map[string]*BuildJob, interval, maxAge time.Duration) *JobPersister {
	return &JobPersister{
		store:       store,
		getJobsFunc: getJobsFunc,
		interval:    interval,
		maxAge:      maxAge,
		stopCh:      make(chan struct{}),
	}
}

// Start starts the periodic persistence goroutine.
func (p *JobPersister) Start() {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				jobs := p.getJobsFunc()

				// Clean old jobs if maxAge is set
				if p.maxAge > 0 {
					cleaned, removed := p.store.CleanOldJobs(jobs, p.maxAge)
					if removed > 0 {
						log.Printf("Cleaned %d old jobs from persistence", removed)
						jobs = cleaned
					}
				}

				if err := p.store.Save(jobs); err != nil {
					log.Printf("Failed to persist jobs: %v", err)
				}
			case <-p.stopCh:
				// Final save before stopping
				jobs := p.getJobsFunc()
				if err := p.store.Save(jobs); err != nil {
					log.Printf("Failed to persist jobs on shutdown: %v", err)
				}
				return
			}
		}
	}()
}

// Stop stops the periodic persistence and performs a final save.
func (p *JobPersister) Stop() {
	close(p.stopCh)
	p.wg.Wait()
}

// SaveNow performs an immediate save.
func (p *JobPersister) SaveNow() error {
	jobs := p.getJobsFunc()
	return p.store.Save(jobs)
}
