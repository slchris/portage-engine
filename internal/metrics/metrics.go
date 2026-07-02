// Package metrics provides monitoring and metrics collection.
package metrics

import (
	"expvar"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var (
	once     sync.Once
	registry *Metrics
)

// Config holds metrics configuration.
type Config struct {
	Enabled  bool
	Port     string
	Password string
}

// Metrics collects various system metrics.
type Metrics struct {
	// enabled is accessed concurrently by metric recorders and Handler,
	// so it is stored as an atomic to keep reads/writes race-free.
	enabled  atomic.Bool
	password string
	mu       sync.RWMutex

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
//
// New returns a process-wide singleton: the first call decides whether the
// metrics are published to the global expvar registry (expvar.Publish panics
// on duplicate names, so registration can only happen once). Subsequent calls
// return the same instance and may only toggle the enabled flag and password;
// they never re-register expvar variables. All access to the enabled flag is
// race-free via an atomic, so recorders and the HTTP handler can be used
// concurrently with additional New calls.
func New(cfg *Config) *Metrics {
	if cfg == nil {
		cfg = &Config{Enabled: false}
	}

	once.Do(func() {
		registry = &Metrics{
			password:          cfg.Password,
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
		registry.enabled.Store(cfg.Enabled)

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

	// Update enabled flag if different. Note: this only toggles the runtime
	// flag; expvar registration is decided once by the first New call.
	if registry.enabled.Load() != cfg.Enabled {
		registry.enabled.Store(cfg.Enabled)
	}

	// Update password if different
	registry.mu.Lock()
	if registry.password != cfg.Password {
		registry.password = cfg.Password
	}
	registry.mu.Unlock()

	return registry
}

// IsEnabled returns whether metrics are enabled.
func (m *Metrics) IsEnabled() bool {
	return m.enabled.Load()
}

// Build metrics

// IncBuildsTotal increments the total builds counter.
func (m *Metrics) IncBuildsTotal() {
	if !m.enabled.Load() {
		return
	}
	m.buildsTotal.Add(1)
}

// IncBuildsSucceeded increments the successful builds counter.
func (m *Metrics) IncBuildsSucceeded() {
	if !m.enabled.Load() {
		return
	}
	m.buildsSucceeded.Add(1)
}

// IncBuildsFailed increments the failed builds counter.
func (m *Metrics) IncBuildsFailed() {
	if !m.enabled.Load() {
		return
	}
	m.buildsFailed.Add(1)
}

// SetBuildsQueued sets the number of queued builds.
func (m *Metrics) SetBuildsQueued(count int64) {
	if !m.enabled.Load() {
		return
	}
	m.buildsQueued.Set(count)
}

// RecordBuildDuration records a build duration.
func (m *Metrics) RecordBuildDuration(packageName string, duration time.Duration) {
	if !m.enabled.Load() {
		return
	}
	m.buildDurations.Add(packageName, duration.Milliseconds())
}

// Builder metrics

// SetBuildersActive sets the number of active builders.
func (m *Metrics) SetBuildersActive(count int64) {
	if !m.enabled.Load() {
		return
	}
	m.buildersActive.Set(count)
}

// SetBuildersHealthy sets the number of healthy builders.
func (m *Metrics) SetBuildersHealthy(count int64) {
	if !m.enabled.Load() {
		return
	}
	m.buildersHealthy.Set(count)
}

// SetBuilderCapacity sets the total builder capacity.
func (m *Metrics) SetBuilderCapacity(count int64) {
	if !m.enabled.Load() {
		return
	}
	m.builderCapacity.Set(count)
}

// IncHeartbeatsTotal increments the total heartbeats counter.
func (m *Metrics) IncHeartbeatsTotal() {
	if !m.enabled.Load() {
		return
	}
	m.heartbeatsTotal.Add(1)
}

// IncHeartbeatsFailed increments the failed heartbeats counter.
func (m *Metrics) IncHeartbeatsFailed() {
	if !m.enabled.Load() {
		return
	}
	m.heartbeatsFailed.Add(1)
}

// Storage metrics

// IncPackagesStored increments the packages stored counter.
func (m *Metrics) IncPackagesStored() {
	if !m.enabled.Load() {
		return
	}
	m.packagesStored.Add(1)
}

// IncStorageReads increments the storage reads counter.
func (m *Metrics) IncStorageReads() {
	if !m.enabled.Load() {
		return
	}
	m.storageReads.Add(1)
}

// IncStorageWrites increments the storage writes counter.
func (m *Metrics) IncStorageWrites() {
	if !m.enabled.Load() {
		return
	}
	m.storageWrites.Add(1)
}

// IncStorageErrors increments the storage errors counter.
func (m *Metrics) IncStorageErrors() {
	if !m.enabled.Load() {
		return
	}
	m.storageErrors.Add(1)
}

// HTTP metrics

// IncHTTPRequests increments the HTTP requests counter.
func (m *Metrics) IncHTTPRequests() {
	if !m.enabled.Load() {
		return
	}
	m.httpRequests.Add(1)
}

// IncHTTPRequestErrors increments the HTTP request errors counter.
func (m *Metrics) IncHTTPRequestErrors() {
	if !m.enabled.Load() {
		return
	}
	m.httpRequestErrors.Add(1)
}

// RecordHTTPLatency records an HTTP request latency.
func (m *Metrics) RecordHTTPLatency(endpoint string, duration time.Duration) {
	if !m.enabled.Load() {
		return
	}
	m.httpLatencies.Add(endpoint, duration.Milliseconds())
}

// System metrics

// UpdateGoroutines updates the goroutines counter.
func (m *Metrics) UpdateGoroutines(count int64) {
	if !m.enabled.Load() {
		return
	}
	m.goroutines.Set(count)
}

// Handler returns an HTTP handler for the metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	if !m.enabled.Load() {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Metrics disabled", http.StatusNotFound)
		})
	}

	// Capture the password under the lock, since New may update it concurrently.
	m.mu.RLock()
	expectedPassword := m.password
	m.mu.RUnlock()

	// If password is not set, return expvar handler directly
	if expectedPassword == "" {
		return expvar.Handler()
	}

	// Wrap expvar handler with basic auth
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "metrics" || password != expectedPassword {
			w.Header().Set("WWW-Authenticate", `Basic realm="Metrics"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		expvar.Handler().ServeHTTP(w, r)
	})
}

// PrometheusHandler returns an HTTP handler that outputs metrics in Prometheus
// text exposition format. This allows Prometheus to scrape metrics without
// requiring the prometheus/client_golang dependency.
func (m *Metrics) PrometheusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.enabled.Load() {
			http.Error(w, "Metrics disabled", http.StatusNotFound)
			return
		}

		// Capture the password under the lock, since New may update it
		// concurrently (same fix as Handler()).
		m.mu.RLock()
		expectedPassword := m.password
		m.mu.RUnlock()

		// Check basic auth if password is set
		if expectedPassword != "" {
			username, password, ok := r.BasicAuth()
			if !ok || username != "metrics" || password != expectedPassword {
				w.Header().Set("WWW-Authenticate", `Basic realm="Metrics"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		// Build metrics
		_, _ = fmt.Fprintf(w, "# HELP portage_builds_total Total number of builds submitted.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_builds_total counter\n")
		_, _ = fmt.Fprintf(w, "portage_builds_total %d\n", m.buildsTotal.Value())

		_, _ = fmt.Fprintf(w, "# HELP portage_builds_succeeded_total Total number of successful builds.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_builds_succeeded_total counter\n")
		_, _ = fmt.Fprintf(w, "portage_builds_succeeded_total %d\n", m.buildsSucceeded.Value())

		_, _ = fmt.Fprintf(w, "# HELP portage_builds_failed_total Total number of failed builds.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_builds_failed_total counter\n")
		_, _ = fmt.Fprintf(w, "portage_builds_failed_total %d\n", m.buildsFailed.Value())

		_, _ = fmt.Fprintf(w, "# HELP portage_builds_queued Current number of queued builds.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_builds_queued gauge\n")
		_, _ = fmt.Fprintf(w, "portage_builds_queued %d\n", m.buildsQueued.Value())

		// Builder metrics
		_, _ = fmt.Fprintf(w, "# HELP portage_builders_active Number of active builders.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_builders_active gauge\n")
		_, _ = fmt.Fprintf(w, "portage_builders_active %d\n", m.buildersActive.Value())

		_, _ = fmt.Fprintf(w, "# HELP portage_builders_healthy Number of healthy builders.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_builders_healthy gauge\n")
		_, _ = fmt.Fprintf(w, "portage_builders_healthy %d\n", m.buildersHealthy.Value())

		_, _ = fmt.Fprintf(w, "# HELP portage_builder_capacity Total builder capacity.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_builder_capacity gauge\n")
		_, _ = fmt.Fprintf(w, "portage_builder_capacity %d\n", m.builderCapacity.Value())

		_, _ = fmt.Fprintf(w, "# HELP portage_heartbeats_total Total builder heartbeats received.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_heartbeats_total counter\n")
		_, _ = fmt.Fprintf(w, "portage_heartbeats_total %d\n", m.heartbeatsTotal.Value())

		_, _ = fmt.Fprintf(w, "# HELP portage_heartbeats_failed_total Total failed heartbeats.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_heartbeats_failed_total counter\n")
		_, _ = fmt.Fprintf(w, "portage_heartbeats_failed_total %d\n", m.heartbeatsFailed.Value())

		// Storage metrics
		_, _ = fmt.Fprintf(w, "# HELP portage_packages_stored Total packages in storage.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_packages_stored gauge\n")
		_, _ = fmt.Fprintf(w, "portage_packages_stored %d\n", m.packagesStored.Value())

		_, _ = fmt.Fprintf(w, "# HELP portage_storage_reads_total Total storage read operations.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_storage_reads_total counter\n")
		_, _ = fmt.Fprintf(w, "portage_storage_reads_total %d\n", m.storageReads.Value())

		_, _ = fmt.Fprintf(w, "# HELP portage_storage_writes_total Total storage write operations.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_storage_writes_total counter\n")
		_, _ = fmt.Fprintf(w, "portage_storage_writes_total %d\n", m.storageWrites.Value())

		_, _ = fmt.Fprintf(w, "# HELP portage_storage_errors_total Total storage errors.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_storage_errors_total counter\n")
		_, _ = fmt.Fprintf(w, "portage_storage_errors_total %d\n", m.storageErrors.Value())

		// HTTP metrics
		_, _ = fmt.Fprintf(w, "# HELP portage_http_requests_total Total HTTP requests.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_http_requests_total counter\n")
		_, _ = fmt.Fprintf(w, "portage_http_requests_total %d\n", m.httpRequests.Value())

		_, _ = fmt.Fprintf(w, "# HELP portage_http_request_errors_total Total HTTP request errors.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_http_request_errors_total counter\n")
		_, _ = fmt.Fprintf(w, "portage_http_request_errors_total %d\n", m.httpRequestErrors.Value())

		// System metrics
		_, _ = fmt.Fprintf(w, "# HELP portage_goroutines Current number of goroutines.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_goroutines gauge\n")
		_, _ = fmt.Fprintf(w, "portage_goroutines %d\n", m.goroutines.Value())

		_, _ = fmt.Fprintf(w, "# HELP portage_uptime_seconds Server uptime in seconds.\n")
		_, _ = fmt.Fprintf(w, "# TYPE portage_uptime_seconds gauge\n")
		_, _ = fmt.Fprintf(w, "portage_uptime_seconds %.2f\n", time.Since(m.startTime).Seconds())
	})
}

// GetSnapshot returns a snapshot of current metrics.
func (m *Metrics) GetSnapshot() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.enabled.Load() {
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
