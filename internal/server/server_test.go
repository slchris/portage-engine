package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/slchris/portage-engine/internal/binpkg"
	"github.com/slchris/portage-engine/internal/builder"
	"github.com/slchris/portage-engine/pkg/config"
)

// TestNew tests creating a new server.
func TestNew(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
	}

	server := New(cfg)
	if server == nil {
		t.Fatal("New returned nil")
	}

	if server.config != cfg {
		t.Error("Config not set correctly")
	}

	if server.binpkgStore == nil {
		t.Error("binpkgStore not initialized")
	}

	if server.builder == nil {
		t.Error("builder not initialized")
	}
}

// TestRouter tests the HTTP router setup.
func TestRouter(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
	}

	server := New(cfg)
	router := server.Router()

	if router == nil {
		t.Fatal("Router returned nil")
	}
}

// TestHandleHealth tests the health check endpoint.
func TestHandleHealth(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
	}

	server := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestHandlePackageQuery tests the package query endpoint.
func TestHandlePackageQuery(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
	}

	server := New(cfg)

	queryReq := binpkg.QueryRequest{
		Name:    "dev-lang/python",
		Version: "3.11.0",
		Arch:    "amd64",
	}

	body, err := json.Marshal(queryReq)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/packages/query", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handlePackageQuery(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var queryResp binpkg.QueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
}

// TestHandlePackageQueryMethodNotAllowed tests method validation.
func TestHandlePackageQueryMethodNotAllowed(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
	}

	server := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/packages/query", nil)
	w := httptest.NewRecorder()

	server.handlePackageQuery(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

// TestHandleBuildRequest tests the build request endpoint.
func TestHandleBuildRequest(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
	}

	server := New(cfg)

	buildReq := builder.BuildRequest{
		PackageName: "app-editors/vim",
		Version:     "9.0",
		Arch:        "amd64",
	}

	body, err := json.Marshal(buildReq)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/packages/request-build", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleBuildRequest(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("Expected status 202, got %d", resp.StatusCode)
	}

	var buildResp builder.BuildResponse
	if err := json.NewDecoder(resp.Body).Decode(&buildResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if buildResp.JobID == "" {
		t.Error("Expected non-empty job ID")
	}
}

// TestHandleBuildStatus tests the build status endpoint.
func TestHandleBuildStatus(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
	}

	server := New(cfg)

	// Submit a build first
	buildReq := builder.BuildRequest{
		PackageName: "sys-apps/portage",
		Version:     "3.0.0",
		Arch:        "amd64",
	}

	body, _ := json.Marshal(buildReq)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/packages/request-build", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.handleBuildRequest(w, req)

	var buildResp builder.BuildResponse
	_ = json.NewDecoder(w.Result().Body).Decode(&buildResp)

	// Query the build status
	req = httptest.NewRequest(http.MethodGet, "/api/v1/packages/status?job_id="+buildResp.JobID, nil)
	w = httptest.NewRecorder()

	server.handleBuildStatus(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestHandleHeartbeat tests the heartbeat endpoint.
func TestHandleHeartbeat(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
		MaxWorkers: 2,
	}

	server := New(cfg)

	tests := []struct {
		name           string
		method         string
		body           interface{}
		expectedStatus int
	}{
		{
			name:   "valid heartbeat",
			method: http.MethodPost,
			body: builder.HeartbeatRequest{
				BuilderID:  "builder-1",
				Status:     "healthy",
				Endpoint:   "http://localhost:9090",
				Capacity:   4,
				ActiveJobs: 2,
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "method not allowed",
			method:         http.MethodGet,
			body:           nil,
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:   "missing builder_id",
			method: http.MethodPost,
			body: builder.HeartbeatRequest{
				Status: "healthy",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "missing status",
			method: http.MethodPost,
			body: builder.HeartbeatRequest{
				BuilderID: "builder-1",
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if tt.body != nil {
				body, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(tt.method, "/api/v1/heartbeat", bytes.NewReader(body))
			w := httptest.NewRecorder()

			server.handleHeartbeat(w, req)

			resp := w.Result()
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			if tt.expectedStatus == http.StatusOK {
				var heartbeatResp builder.HeartbeatResponse
				_ = json.NewDecoder(resp.Body).Decode(&heartbeatResp)

				if !heartbeatResp.Success {
					t.Error("Expected success=true")
				}
			}
		})
	}
}

// TestHandleHeartbeatInvalidJSON tests heartbeat with invalid JSON.
func TestHandleHeartbeatInvalidJSON(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
		MaxWorkers: 2,
	}

	server := New(cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/heartbeat", bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	server.handleHeartbeat(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

// TestHandleBuilderRegister tests the builder registration endpoint.
func TestHandleBuilderRegister(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
	}

	server := New(cfg)

	tests := []struct {
		name           string
		method         string
		body           interface{}
		expectedStatus int
	}{
		{
			name:   "valid registration",
			method: http.MethodPost,
			body: builder.BuilderInfo{
				ID:           "builder-1",
				Endpoint:     "http://localhost:9090",
				Architecture: "amd64",
				Status:       "online",
				Capacity:     4,
				CurrentLoad:  0,
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "registration with metrics",
			method: http.MethodPost,
			body: builder.BuilderInfo{
				ID:           "builder-2",
				Endpoint:     "http://localhost:9091",
				Architecture: "arm64",
				Status:       "online",
				Capacity:     2,
				CPUUsage:     45.5,
				MemoryUsage:  60.2,
				DiskUsage:    55.0,
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "method not allowed",
			method:         http.MethodGet,
			body:           nil,
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "invalid json",
			method:         http.MethodPost,
			body:           "invalid",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if tt.body != nil {
				if str, ok := tt.body.(string); ok {
					body = []byte(str)
				} else {
					body, _ = json.Marshal(tt.body)
				}
			}

			req := httptest.NewRequest(tt.method, "/api/v1/builders/register", bytes.NewReader(body))
			w := httptest.NewRecorder()

			server.handleBuilderRegister(w, req)

			resp := w.Result()
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			if tt.expectedStatus == http.StatusOK {
				var result map[string]interface{}
				_ = json.NewDecoder(resp.Body).Decode(&result)

				if success, ok := result["success"].(bool); !ok || !success {
					t.Error("Expected success=true")
				}
			}
		})
	}
}

// TestHandleBuildersList tests the builders list endpoint.
func TestHandleBuildersList(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
	}

	server := New(cfg)

	// Register some builders first
	builders := []builder.BuilderInfo{
		{
			ID:           "builder-1",
			Endpoint:     "http://localhost:9090",
			Architecture: "amd64",
			Status:       "online",
		},
		{
			ID:           "builder-2",
			Endpoint:     "http://localhost:9091",
			Architecture: "arm64",
			Status:       "online",
		},
	}

	for _, b := range builders {
		body, _ := json.Marshal(b)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builders/register", bytes.NewReader(body))
		w := httptest.NewRecorder()
		server.handleBuilderRegister(w, req)
	}

	// Test list endpoint
	tests := []struct {
		name           string
		method         string
		expectedStatus int
		expectBuilders bool
	}{
		{
			name:           "get builders list",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
			expectBuilders: true,
		},
		{
			name:           "method not allowed",
			method:         http.MethodPost,
			expectedStatus: http.StatusMethodNotAllowed,
			expectBuilders: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/builders/list", nil)
			w := httptest.NewRecorder()

			server.handleBuildersList(w, req)

			resp := w.Result()
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			if tt.expectBuilders {
				var result []*builder.BuilderInfo
				_ = json.NewDecoder(resp.Body).Decode(&result)

				if len(result) < 2 {
					t.Errorf("Expected at least 2 builders, got %d", len(result))
				}
			}
		})
	}
}

// TestHandleBuildersStatus tests the builders status endpoint.
func TestHandleBuildersStatus(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
		// No remote builders configured - should return empty list
	}

	server := New(cfg)

	// Test status endpoint
	tests := []struct {
		name           string
		method         string
		expectedStatus int
	}{
		{
			name:           "get builders status",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "method not allowed",
			method:         http.MethodPost,
			expectedStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/builders/status", nil)
			w := httptest.NewRecorder()

			server.handleBuildersStatus(w, req)

			resp := w.Result()
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			if tt.expectedStatus == http.StatusOK {
				var result map[string]interface{}
				_ = json.NewDecoder(resp.Body).Decode(&result)

				// Check stats
				stats, ok := result["stats"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected stats in response")
				}

				// With no remote builders configured, total should be 0
				if stats["total_builders"].(float64) != 0 {
					t.Errorf("Expected 0 total builders, got %v", stats["total_builders"])
				}
			}
		})
	}
}

// TestCalculateBuilderStats tests the builder stats calculation.
func TestCalculateBuilderStats(t *testing.T) {
	builders := []BuilderStatusInfo{
		{
			ID:            "builder-1",
			Status:        "online",
			Capacity:      4,
			CurrentLoad:   2,
			TotalBuilds:   100,
			SuccessBuilds: 95,
			FailedBuilds:  5,
		},
		{
			ID:            "builder-2",
			Status:        "busy",
			Capacity:      2,
			CurrentLoad:   2,
			TotalBuilds:   50,
			SuccessBuilds: 48,
			FailedBuilds:  2,
		},
		{
			ID:            "builder-3",
			Status:        "offline",
			Capacity:      4,
			CurrentLoad:   0,
			TotalBuilds:   30,
			SuccessBuilds: 25,
			FailedBuilds:  5,
		},
	}

	stats := calculateBuilderStats(builders)

	if stats["total_builders"].(int) != 3 {
		t.Errorf("Expected 3 total builders, got %v", stats["total_builders"])
	}

	if stats["online_builders"].(int) != 2 {
		t.Errorf("Expected 2 online builders, got %v", stats["online_builders"])
	}

	if stats["offline_builders"].(int) != 1 {
		t.Errorf("Expected 1 offline builder, got %v", stats["offline_builders"])
	}

	if stats["total_capacity"].(int) != 10 {
		t.Errorf("Expected capacity 10, got %v", stats["total_capacity"])
	}

	if stats["total_load"].(int) != 4 {
		t.Errorf("Expected load 4, got %v", stats["total_load"])
	}

	if stats["total_builds"].(int) != 180 {
		t.Errorf("Expected 180 total builds, got %v", stats["total_builds"])
	}

	expectedSuccessRate := float64(168) / float64(180) * 100
	if stats["success_rate"].(float64) != expectedSuccessRate {
		t.Errorf("Expected success rate %f, got %v", expectedSuccessRate, stats["success_rate"])
	}
}

// TestHelperFunctions tests the type conversion helper functions.
func TestHelperFunctions(t *testing.T) {
	m := map[string]interface{}{
		"string_val":  "test",
		"int_val":     42,
		"float_val":   3.14,
		"bool_val":    true,
		"int64_val":   int64(100),
		"float64_val": float64(99.9),
	}

	// Test getStringValue
	if v := getStringValue(m, "string_val", "default"); v != "test" {
		t.Errorf("Expected 'test', got '%s'", v)
	}
	if v := getStringValue(m, "missing", "default"); v != "default" {
		t.Errorf("Expected 'default', got '%s'", v)
	}

	// Test getIntValue
	if v := getIntValue(m, "int_val", 0); v != 42 {
		t.Errorf("Expected 42, got %d", v)
	}
	if v := getIntValue(m, "float64_val", 0); v != 99 {
		t.Errorf("Expected 99, got %d", v)
	}
	if v := getIntValue(m, "int64_val", 0); v != 100 {
		t.Errorf("Expected 100, got %d", v)
	}
	if v := getIntValue(m, "missing", 10); v != 10 {
		t.Errorf("Expected 10, got %d", v)
	}

	// Test getFloatValue
	if v := getFloatValue(m, "float_val", 0); v != 3.14 {
		t.Errorf("Expected 3.14, got %f", v)
	}
	if v := getFloatValue(m, "int_val", 0); v != 42.0 {
		t.Errorf("Expected 42.0, got %f", v)
	}
	if v := getFloatValue(m, "missing", 1.5); v != 1.5 {
		t.Errorf("Expected 1.5, got %f", v)
	}

	// Test getBoolValue
	if v := getBoolValue(m, "bool_val", false); v != true {
		t.Errorf("Expected true, got %v", v)
	}
	if v := getBoolValue(m, "missing", true); v != true {
		t.Errorf("Expected true, got %v", v)
	}
}

// TestBuilderRegistryIntegration tests the integration between server and builder registry.
func TestBuilderRegistryIntegration(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
	}

	server := New(cfg)

	// Register a builder
	builderInfo := builder.BuilderInfo{
		ID:           "test-builder",
		Endpoint:     "http://localhost:9090",
		Architecture: "amd64",
		Status:       "online",
		Capacity:     4,
		CurrentLoad:  1,
	}

	body, _ := json.Marshal(builderInfo)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builders/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.handleBuilderRegister(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Registration failed: %d", w.Code)
	}

	// Send heartbeat to update the builder
	heartbeat := builder.HeartbeatRequest{
		BuilderID:  "test-builder",
		Status:     "busy",
		Endpoint:   "http://localhost:9090",
		Capacity:   4,
		ActiveJobs: 3,
	}

	body, _ = json.Marshal(heartbeat)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/heartbeat", bytes.NewReader(body))
	w = httptest.NewRecorder()
	server.handleHeartbeat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Heartbeat failed: %d", w.Code)
	}

	// Verify the builder was updated in the registry
	info, exists := server.builderRegistry.Get("test-builder")
	if !exists {
		t.Error("Builder not found in registry")
	}
	if info.Status != "busy" {
		t.Errorf("Expected status 'busy', got %v", info.Status)
	}
	if info.CurrentLoad != 3 {
		t.Errorf("Expected current_load 3, got %v", info.CurrentLoad)
	}
}

// TestHandleGPGPublicKey tests the GPG public key endpoint.
func TestHandleGPGPublicKey(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test-key.asc")
	keyContent := "-----BEGIN PGP PUBLIC KEY BLOCK-----\ntest key\n-----END PGP PUBLIC KEY BLOCK-----\n"

	if err := os.WriteFile(keyPath, []byte(keyContent), 0600); err != nil {
		t.Fatalf("failed to create test key file: %v", err)
	}

	tests := []struct {
		name           string
		method         string
		gpgEnabled     bool
		gpgKeyPath     string
		expectedStatus int
		checkContent   bool
	}{
		{
			name:           "valid request",
			method:         http.MethodGet,
			gpgEnabled:     true,
			gpgKeyPath:     keyPath,
			expectedStatus: http.StatusOK,
			checkContent:   true,
		},
		{
			name:           "method not allowed",
			method:         http.MethodPost,
			gpgEnabled:     true,
			gpgKeyPath:     keyPath,
			expectedStatus: http.StatusMethodNotAllowed,
			checkContent:   false,
		},
		{
			name:           "gpg not enabled",
			method:         http.MethodGet,
			gpgEnabled:     false,
			gpgKeyPath:     keyPath,
			expectedStatus: http.StatusNotFound,
			checkContent:   false,
		},
		{
			name:           "key path not configured",
			method:         http.MethodGet,
			gpgEnabled:     true,
			gpgKeyPath:     "",
			expectedStatus: http.StatusInternalServerError,
			checkContent:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.ServerConfig{
				BinpkgPath: "/tmp/binpkgs",
				GPGEnabled: tt.gpgEnabled,
				GPGKeyPath: tt.gpgKeyPath,
			}

			server := New(cfg)

			req := httptest.NewRequest(tt.method, "/api/v1/gpg/public-key", nil)
			w := httptest.NewRecorder()

			server.handleGPGPublicKey(w, req)

			resp := w.Result()
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			if tt.checkContent {
				body := w.Body.String()
				if body != keyContent {
					t.Errorf("Expected content %q, got %q", keyContent, body)
				}
			}
		})
	}
}

// TestHandleArtifactInfo tests the artifact info endpoint.
func TestHandleArtifactInfo(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath:     "/tmp/binpkgs",
		RemoteBuilders: []string{"http://localhost:9090"},
	}

	server := New(cfg)

	// Test method not allowed
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/info/test-job-id", nil)
	w := httptest.NewRecorder()

	server.handleArtifactInfo(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}

	// Test missing job ID
	req = httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/info/", nil)
	w = httptest.NewRecorder()

	server.handleArtifactInfo(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400 for missing job ID, got %d", resp.StatusCode)
	}
}

// TestHandleArtifactDownload tests the artifact download endpoint.
func TestHandleArtifactDownload(t *testing.T) {
	cfg := &config.ServerConfig{
		BinpkgPath:     "/tmp/binpkgs",
		RemoteBuilders: []string{"http://localhost:9090"},
	}

	server := New(cfg)

	// Test method not allowed
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/download/test-job-id", nil)
	w := httptest.NewRecorder()

	server.handleArtifactDownload(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}

	// Test missing job ID
	req = httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/download/", nil)
	w = httptest.NewRecorder()

	server.handleArtifactDownload(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400 for missing job ID, got %d", resp.StatusCode)
	}
}

// TestGetBuilderURLForJob tests the getBuilderURLForJob helper.
func TestGetBuilderURLForJob(t *testing.T) {
	// Test with no builders and no remote builders configured
	cfg := &config.ServerConfig{
		BinpkgPath: "/tmp/binpkgs",
	}

	server := New(cfg)

	_, err := server.getBuilderURLForJob("test-job")
	if err == nil {
		t.Error("Expected error when no builders configured")
	}

	// Test with remote builders configured
	cfg.RemoteBuilders = []string{"http://builder1:9090", "http://builder2:9090"}
	server = New(cfg)

	url, err := server.getBuilderURLForJob("test-job")
	if err != nil {
		t.Errorf("Expected no error with remote builders, got: %v", err)
	}
	if url != "http://builder1:9090" {
		t.Errorf("Expected first remote builder, got: %s", url)
	}
}
