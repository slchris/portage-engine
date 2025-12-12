package metrics

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		enabled bool
	}{
		{
			name:    "enabled metrics",
			cfg:     &Config{Enabled: true, Port: "2112"},
			enabled: true,
		},
		{
			name:    "disabled metrics",
			cfg:     &Config{Enabled: false, Port: "2112"},
			enabled: false,
		},
		{
			name:    "nil config",
			cfg:     nil,
			enabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(tt.cfg)
			if m == nil {
				t.Fatal("Expected non-nil Metrics")
			}
			if m.IsEnabled() != tt.enabled {
				t.Errorf("Expected enabled=%v, got %v", tt.enabled, m.IsEnabled())
			}
		})
	}
}

func TestBuildMetrics(t *testing.T) {
	m := New(&Config{Enabled: true})

	m.IncBuildsTotal()
	m.IncBuildsSucceeded()
	m.IncBuildsFailed()
	m.SetBuildsQueued(5)
	m.RecordBuildDuration("test-package", 100*time.Millisecond)

	snapshot := m.GetSnapshot()
	if snapshot["builds_total"].(int64) != 1 {
		t.Errorf("Expected builds_total=1, got %v", snapshot["builds_total"])
	}
	if snapshot["builds_succeeded"].(int64) != 1 {
		t.Errorf("Expected builds_succeeded=1, got %v", snapshot["builds_succeeded"])
	}
	if snapshot["builds_failed"].(int64) != 1 {
		t.Errorf("Expected builds_failed=1, got %v", snapshot["builds_failed"])
	}
	if snapshot["builds_queued"].(int64) != 5 {
		t.Errorf("Expected builds_queued=5, got %v", snapshot["builds_queued"])
	}
}

func TestBuilderMetrics(t *testing.T) {
	m := New(&Config{Enabled: true})

	m.SetBuildersActive(3)
	m.SetBuildersHealthy(2)
	m.SetBuilderCapacity(10)
	m.IncHeartbeatsTotal()
	m.IncHeartbeatsFailed()

	snapshot := m.GetSnapshot()
	if snapshot["builders_active"].(int64) != 3 {
		t.Errorf("Expected builders_active=3, got %v", snapshot["builders_active"])
	}
	if snapshot["builders_healthy"].(int64) != 2 {
		t.Errorf("Expected builders_healthy=2, got %v", snapshot["builders_healthy"])
	}
	if snapshot["builder_capacity"].(int64) != 10 {
		t.Errorf("Expected builder_capacity=10, got %v", snapshot["builder_capacity"])
	}
	if snapshot["heartbeats_total"].(int64) != 1 {
		t.Errorf("Expected heartbeats_total=1, got %v", snapshot["heartbeats_total"])
	}
	if snapshot["heartbeats_failed"].(int64) != 1 {
		t.Errorf("Expected heartbeats_failed=1, got %v", snapshot["heartbeats_failed"])
	}
}

func TestStorageMetrics(t *testing.T) {
	m := New(&Config{Enabled: true})

	m.IncPackagesStored()
	m.IncStorageReads()
	m.IncStorageWrites()
	m.IncStorageErrors()

	snapshot := m.GetSnapshot()
	if snapshot["packages_stored"].(int64) != 1 {
		t.Errorf("Expected packages_stored=1, got %v", snapshot["packages_stored"])
	}
	if snapshot["storage_reads"].(int64) != 1 {
		t.Errorf("Expected storage_reads=1, got %v", snapshot["storage_reads"])
	}
	if snapshot["storage_writes"].(int64) != 1 {
		t.Errorf("Expected storage_writes=1, got %v", snapshot["storage_writes"])
	}
	if snapshot["storage_errors"].(int64) != 1 {
		t.Errorf("Expected storage_errors=1, got %v", snapshot["storage_errors"])
	}
}

func TestHTTPMetrics(t *testing.T) {
	m := New(&Config{Enabled: true})

	m.IncHTTPRequests()
	m.IncHTTPRequestErrors()
	m.RecordHTTPLatency("/api/test", 50*time.Millisecond)

	snapshot := m.GetSnapshot()
	if snapshot["http_requests_total"].(int64) != 1 {
		t.Errorf("Expected http_requests_total=1, got %v", snapshot["http_requests_total"])
	}
	if snapshot["http_request_errors"].(int64) != 1 {
		t.Errorf("Expected http_request_errors=1, got %v", snapshot["http_request_errors"])
	}
}

func TestSystemMetrics(t *testing.T) {
	m := New(&Config{Enabled: true})

	goroutineCount := int64(runtime.NumGoroutine())
	m.UpdateGoroutines(goroutineCount)

	snapshot := m.GetSnapshot()
	if snapshot["goroutines"].(int64) != goroutineCount {
		t.Errorf("Expected goroutines=%v, got %v", goroutineCount, snapshot["goroutines"])
	}

	uptime := snapshot["uptime_seconds"].(float64)
	if uptime <= 0 {
		t.Errorf("Expected positive uptime, got %v", uptime)
	}
}

func TestMetricsDisabled(t *testing.T) {
	m := New(&Config{Enabled: false})

	m.IncBuildsTotal()
	m.IncBuildsSucceeded()
	m.SetBuildersActive(5)

	snapshot := m.GetSnapshot()
	if snapshot["enabled"].(bool) {
		t.Error("Expected metrics to be disabled")
	}

	if len(snapshot) != 1 {
		t.Errorf("Expected only 'enabled' field, got %v", snapshot)
	}
}

func TestHandler(t *testing.T) {
	tests := []struct {
		name           string
		enabled        bool
		expectedStatus int
	}{
		{
			name:           "enabled handler",
			enabled:        true,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "disabled handler",
			enabled:        false,
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(&Config{Enabled: tt.enabled})
			handler := m.Handler()

			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %v, got %v", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestConcurrentMetrics(t *testing.T) {
	m := New(&Config{Enabled: true})

	// Get initial values
	initialSnapshot := m.GetSnapshot()
	initialBuildsTotal := initialSnapshot["builds_total"].(int64)
	initialBuildsSucceeded := initialSnapshot["builds_succeeded"].(int64)
	initialHTTPRequests := initialSnapshot["http_requests_total"].(int64)

	var wg sync.WaitGroup
	iterations := 100

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				m.IncBuildsTotal()
				m.IncBuildsSucceeded()
				m.IncBuildsFailed()
				m.IncHeartbeatsTotal()
				m.IncPackagesStored()
				m.IncStorageReads()
				m.IncHTTPRequests()
			}
		}()
	}

	wg.Wait()

	snapshot := m.GetSnapshot()
	expected := int64(10 * iterations)

	if snapshot["builds_total"].(int64) != initialBuildsTotal+expected {
		t.Errorf("Expected builds_total=%v, got %v", initialBuildsTotal+expected, snapshot["builds_total"])
	}
	if snapshot["builds_succeeded"].(int64) != initialBuildsSucceeded+expected {
		t.Errorf("Expected builds_succeeded=%v, got %v", initialBuildsSucceeded+expected, snapshot["builds_succeeded"])
	}
	if snapshot["http_requests_total"].(int64) != initialHTTPRequests+expected {
		t.Errorf("Expected http_requests_total=%v, got %v", initialHTTPRequests+expected, snapshot["http_requests_total"])
	}
}

func TestGetSnapshotStructure(t *testing.T) {
	m := New(&Config{Enabled: true})

	snapshot := m.GetSnapshot()

	requiredFields := []string{
		"enabled",
		"builds_total",
		"builds_succeeded",
		"builds_failed",
		"builds_queued",
		"builders_active",
		"builders_healthy",
		"builder_capacity",
		"heartbeats_total",
		"heartbeats_failed",
		"packages_stored",
		"storage_reads",
		"storage_writes",
		"storage_errors",
		"http_requests_total",
		"http_request_errors",
		"goroutines",
		"uptime_seconds",
	}

	for _, field := range requiredFields {
		if _, ok := snapshot[field]; !ok {
			t.Errorf("Missing required field: %s", field)
		}
	}
}
