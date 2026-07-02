// Package server provides persistence for server-side build job state.
package server

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/slchris/portage-engine/internal/builder"
)

// ServerStore provides persistent storage for server-side build status.
// It uses atomic file writes (write-to-temp + rename) for crash safety.
//
//nolint:revive // ServerStore/ServerPersister are the established names across the package.
type ServerStore struct {
	dataDir  string
	filename string
	mu       sync.RWMutex
}

// persistedState represents the full server state saved to disk.
type persistedState struct {
	Jobs      map[string]*builder.BuildStatus `json:"jobs"`
	UpdatedAt time.Time                       `json:"updated_at"`
	Version   string                          `json:"version"`
}

// NewServerStore creates a new server store at the given directory.
// The directory is created if it doesn't exist.
func NewServerStore(dataDir string) (*ServerStore, error) {
	if dataDir == "" {
		dataDir = "/var/lib/portage-engine/server"
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

	return &ServerStore{
		dataDir:  dataDir,
		filename: filepath.Join(dataDir, "server_jobs.json"),
	}, nil
}

// LoadJobs loads persisted jobs from disk.
// Returns an empty map if the file doesn't exist (first run).
func (s *ServerStore) LoadJobs() (map[string]*builder.BuildStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.filename)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*builder.BuildStatus), nil
		}
		return nil, fmt.Errorf("failed to read server jobs file: %w", err)
	}

	// Distinguish the current envelope format ({"jobs":..,"version":..}) from the
	// legacy bare-map format ({"job-id":{...}}) STRUCTURALLY: a legacy file
	// unmarshals into persistedState without error (its top-level keys simply
	// don't match), which would silently drop every legacy job. Detect the
	// envelope by the presence of a top-level "jobs" key.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("failed to parse server jobs file: %w", err)
	}

	if _, isEnvelope := top["jobs"]; isEnvelope {
		var state persistedState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, fmt.Errorf("failed to parse server jobs file: %w", err)
		}
		if state.Jobs == nil {
			return make(map[string]*builder.BuildStatus), nil
		}
		return state.Jobs, nil
	}

	// Legacy bare-map format.
	var legacyJobs map[string]*builder.BuildStatus
	if err := json.Unmarshal(data, &legacyJobs); err != nil {
		return nil, fmt.Errorf("failed to parse legacy server jobs file: %w", err)
	}
	if legacyJobs == nil {
		return make(map[string]*builder.BuildStatus), nil
	}
	return legacyJobs, nil
}

// SaveJobs saves the current jobs to disk atomically.
func (s *ServerStore) SaveJobs(jobs map[string]*builder.BuildStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := persistedState{
		Jobs:      jobs,
		UpdatedAt: time.Now(),
		Version:   Version,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal server jobs: %w", err)
	}

	// Write to temp file first, then rename for atomicity
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

// CleanOldJobs removes completed/failed jobs older than maxAge.
// In-progress jobs (queued, building, provisioning, forwarding) are always kept.
func (s *ServerStore) CleanOldJobs(jobs map[string]*builder.BuildStatus, maxAge time.Duration) (map[string]*builder.BuildStatus, int) {
	cutoff := time.Now().Add(-maxAge)
	cleaned := make(map[string]*builder.BuildStatus)
	removedCount := 0

	for id, job := range jobs {
		switch {
		case job.Status == "queued" || job.Status == "building" || job.Status == "provisioning" || job.Status == "forwarding":
			cleaned[id] = job
		case job.UpdatedAt.After(cutoff) || job.UpdatedAt.IsZero():
			cleaned[id] = job
		default:
			removedCount++
		}
	}

	return cleaned, removedCount
}

// ServerPersister handles periodic persistence of server jobs.
//
//nolint:revive // established name; see ServerStore.
type ServerPersister struct {
	store       *ServerStore
	getJobsFunc func() map[string]*builder.BuildStatus
	interval    time.Duration
	maxAge      time.Duration
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

// NewServerPersister creates a new persister that periodically saves jobs to disk.
func NewServerPersister(store *ServerStore, getJobsFunc func() map[string]*builder.BuildStatus, interval, maxAge time.Duration) *ServerPersister {
	return &ServerPersister{
		store:       store,
		getJobsFunc: getJobsFunc,
		interval:    interval,
		maxAge:      maxAge,
		stopCh:      make(chan struct{}),
	}
}

// Start begins the periodic persistence goroutine.
func (p *ServerPersister) Start() {
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
						log.Printf("Server persistence: cleaned %d old jobs", removed)
						jobs = cleaned
					}
				}

				if err := p.store.SaveJobs(jobs); err != nil {
					log.Printf("Server persistence: failed to save jobs: %v", err)
				}
			case <-p.stopCh:
				// Final save before stopping
				jobs := p.getJobsFunc()
				if err := p.store.SaveJobs(jobs); err != nil {
					log.Printf("Server persistence: failed final save on shutdown: %v", err)
				} else {
					log.Printf("Server persistence: final save complete (%d jobs)", len(jobs))
				}
				return
			}
		}
	}()
}

// Stop stops persistence and performs a final save. Blocks until complete.
func (p *ServerPersister) Stop() {
	close(p.stopCh)
	p.wg.Wait()
}

// SaveNow performs an immediate save.
func (p *ServerPersister) SaveNow() error {
	jobs := p.getJobsFunc()
	return p.store.SaveJobs(jobs)
}
