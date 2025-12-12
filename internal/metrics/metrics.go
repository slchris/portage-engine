// Package metrics provides monitoring and metrics collection.
package metrics

import (
	"expvar"
	"net/http"
	"sync"
	"time"
)

var (
	once     sync.Once
	registry *Metrics
)

// Config holds metrics configuration.
type Config struct {
	Enabled bool
	Port    string
}

// Metrics collects various system metrics.
type Metrics struct {
	enabled bool
	mu      sync.RWMutex

	// Build metrics
	buildsTotal     *expvar.Int
	buildsSucceeded *expvar.Int
	buildsFailed    *expvar.Int
	buildsQueued    *expvar.Int
	buildDurations  *expvar.Map

	// Builder metrics
	buildersActive   *expvar.Int
	buildersHealthy  *expvar.Int
	builderCapacity  *expvar.Int
	heartbeatsTotal  *expvar.Int
	heartbeatsFailed *expvar.Int

	// Storage metrics
	packagesStored *expvar.Int
	storageReads   *expvar.Int
	storageWrites  *expvar.Int
	storageErrors  *expvar.Int

	// HTTP metrics
	httpRequests      *expvar.Int
	httpRequestErrors *expvar.Int
	httpLatencies     *expvar.Map

	// System metrics
	goroutines *expvar.Int
	startTime  time.Time
}

// New creates a new Metrics instance.
func New(cfg *Config) *Metrics {
	if cfg == nil {
		cfg = &Config{Enabled: false}
	}

	once.Do(func() {
		registry = &Metrics{
			enabled:           cfg.Enabled,
			buildsTotal:       new(expvar.Int),
			buildsSucceeded:   new(expvar.Int),
			buildsFailed:      new(expvar.Int),
			buildsQueued:      new(expvar.Int),
			buildDurations:    new(expvar.Map),
			buildersActive:    new(expvar.Int),
			buildersHealthy:   new(expvar.Int),
			builderCapacity:   new(expvar.Int),
			heartbeatsTotal:   new(expvar.Int),
			heartbeatsFailed:  new(expvar.Int),
			packagesStored:    new(expvar.Int),
			storageReads:      new(expvar.Int),
			storageWrites:     new(expvar.Int),
			storageErrors:     new(expvar.Int),
			httpRequests:      new(expvar.Int),
			httpRequestErrors: new(expvar.Int),
			httpLatencies:     new(expvar.Map),
			goroutines:        new(expvar.Int),
			startTime:         time.Now(),
		}

		if cfg.Enabled {
			// Only publish to global expvar registry if metrics are enabled
			expvar.Publish("builds_total", registry.buildsTotal)
			expvar.Publish("builds_succeeded", registry.buildsSucceeded)
			expvar.Publish("builds_failed", registry.buildsFailed)
			expvar.Publish("builds_queued", registry.buildsQueued)
			expvar.Publish("build_durations", registry.buildDurations)
			expvar.Publish("builders_active", registry.buildersActive)
			expvar.Publish("builders_healthy", registry.buildersHealthy)
			expvar.Publish("builder_capacity", registry.builderCapacity)
			expvar.Publish("heartbeats_total", registry.heartbeatsTotal)
			expvar.Publish("heartbeats_failed", registry.heartbeatsFailed)
			expvar.Publish("packages_stored", registry.packagesStored)
			expvar.Publish("storage_reads", registry.storageReads)
			expvar.Publish("storage_writes", registry.storageWrites)
			expvar.Publish("storage_errors", registry.storageErrors)
			expvar.Publish("http_requests_total", registry.httpRequests)
			expvar.Publish("http_request_errors", registry.httpRequestErrors)
			expvar.Publish("http_latencies", registry.httpLatencies)
			expvar.Publish("goroutines", registry.goroutines)

			// Publish uptime
			expvar.Publish("uptime_seconds", expvar.Func(func() interface{} {
				return time.Since(registry.startTime).Seconds()
			}))
		}
	})

	// Update enabled flag if different
	if registry.enabled != cfg.Enabled {
		registry.mu.Lock()
		registry.enabled = cfg.Enabled
		registry.mu.Unlock()
	}

	return registry
}

// IsEnabled returns whether metrics are enabled.
func (m *Metrics) IsEnabled() bool {
	return m.enabled
}

// Build metrics

// IncBuildsTotal increments the total builds counter.
func (m *Metrics) IncBuildsTotal() {
	if !m.enabled {
		return
	}
	m.buildsTotal.Add(1)
}

// IncBuildsSucceeded increments the successful builds counter.
func (m *Metrics) IncBuildsSucceeded() {
	if !m.enabled {
		return
	}
	m.buildsSucceeded.Add(1)
}

// IncBuildsFailed increments the failed builds counter.
func (m *Metrics) IncBuildsFailed() {
	if !m.enabled {
		return
	}
	m.buildsFailed.Add(1)
}

// SetBuildsQueued sets the number of queued builds.
func (m *Metrics) SetBuildsQueued(count int64) {
	if !m.enabled {
		return
	}
	m.buildsQueued.Set(count)
}

// RecordBuildDuration records a build duration.
func (m *Metrics) RecordBuildDuration(packageName string, duration time.Duration) {
	if !m.enabled {
		return
	}
	m.buildDurations.Add(packageName, duration.Milliseconds())
}

// Builder metrics

// SetBuildersActive sets the number of active builders.
func (m *Metrics) SetBuildersActive(count int64) {
	if !m.enabled {
		return
	}
	m.buildersActive.Set(count)
}

// SetBuildersHealthy sets the number of healthy builders.
func (m *Metrics) SetBuildersHealthy(count int64) {
	if !m.enabled {
		return
	}
	m.buildersHealthy.Set(count)
}

// SetBuilderCapacity sets the total builder capacity.
func (m *Metrics) SetBuilderCapacity(count int64) {
	if !m.enabled {
		return
	}
	m.builderCapacity.Set(count)
}

// IncHeartbeatsTotal increments the total heartbeats counter.
func (m *Metrics) IncHeartbeatsTotal() {
	if !m.enabled {
		return
	}
	m.heartbeatsTotal.Add(1)
}

// IncHeartbeatsFailed increments the failed heartbeats counter.
func (m *Metrics) IncHeartbeatsFailed() {
	if !m.enabled {
		return
	}
	m.heartbeatsFailed.Add(1)
}

// Storage metrics

// IncPackagesStored increments the packages stored counter.
func (m *Metrics) IncPackagesStored() {
	if !m.enabled {
		return
	}
	m.packagesStored.Add(1)
}

// IncStorageReads increments the storage reads counter.
func (m *Metrics) IncStorageReads() {
	if !m.enabled {
		return
	}
	m.storageReads.Add(1)
}

// IncStorageWrites increments the storage writes counter.
func (m *Metrics) IncStorageWrites() {
	if !m.enabled {
		return
	}
	m.storageWrites.Add(1)
}

// IncStorageErrors increments the storage errors counter.
func (m *Metrics) IncStorageErrors() {
	if !m.enabled {
		return
	}
	m.storageErrors.Add(1)
}

// HTTP metrics

// IncHTTPRequests increments the HTTP requests counter.
func (m *Metrics) IncHTTPRequests() {
	if !m.enabled {
		return
	}
	m.httpRequests.Add(1)
}

// IncHTTPRequestErrors increments the HTTP request errors counter.
func (m *Metrics) IncHTTPRequestErrors() {
	if !m.enabled {
		return
	}
	m.httpRequestErrors.Add(1)
}

// RecordHTTPLatency records an HTTP request latency.
func (m *Metrics) RecordHTTPLatency(endpoint string, duration time.Duration) {
	if !m.enabled {
		return
	}
	m.httpLatencies.Add(endpoint, duration.Milliseconds())
}

// System metrics

// UpdateGoroutines updates the goroutines counter.
func (m *Metrics) UpdateGoroutines(count int64) {
	if !m.enabled {
		return
	}
	m.goroutines.Set(count)
}

// Handler returns an HTTP handler for the metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	if !m.enabled {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Metrics disabled", http.StatusNotFound)
		})
	}
	return expvar.Handler()
}

// GetSnapshot returns a snapshot of current metrics.
func (m *Metrics) GetSnapshot() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.enabled {
		return map[string]interface{}{
			"enabled": false,
		}
	}

	return map[string]interface{}{
		"enabled":             true,
		"builds_total":        m.buildsTotal.Value(),
		"builds_succeeded":    m.buildsSucceeded.Value(),
		"builds_failed":       m.buildsFailed.Value(),
		"builds_queued":       m.buildsQueued.Value(),
		"builders_active":     m.buildersActive.Value(),
		"builders_healthy":    m.buildersHealthy.Value(),
		"builder_capacity":    m.builderCapacity.Value(),
		"heartbeats_total":    m.heartbeatsTotal.Value(),
		"heartbeats_failed":   m.heartbeatsFailed.Value(),
		"packages_stored":     m.packagesStored.Value(),
		"storage_reads":       m.storageReads.Value(),
		"storage_writes":      m.storageWrites.Value(),
		"storage_errors":      m.storageErrors.Value(),
		"http_requests_total": m.httpRequests.Value(),
		"http_request_errors": m.httpRequestErrors.Value(),
		"goroutines":          m.goroutines.Value(),
		"uptime_seconds":      time.Since(m.startTime).Seconds(),
	}
}
