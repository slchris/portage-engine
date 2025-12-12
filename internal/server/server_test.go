package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	}

	server := New(cfg)

	// Register builders with various states
	builders := []builder.BuilderInfo{
		{
			ID:            "builder-1",
			Endpoint:      "http://localhost:9090",
			Architecture:  "amd64",
			Status:        "online",
			Capacity:      4,
			CurrentLoad:   2,
			TotalBuilds:   100,
			SuccessBuilds: 95,
			FailedBuilds:  5,
			Enabled:       true,
		},
		{
			ID:            "builder-2",
			Endpoint:      "http://localhost:9091",
			Architecture:  "arm64",
			Status:        "busy",
			Capacity:      2,
			CurrentLoad:   2,
			TotalBuilds:   50,
			SuccessBuilds: 48,
			FailedBuilds:  2,
			Enabled:       true,
		},
		{
			ID:            "builder-3",
			Endpoint:      "http://localhost:9092",
			Architecture:  "amd64",
			Status:        "offline",
			Capacity:      4,
			CurrentLoad:   0,
			TotalBuilds:   30,
			SuccessBuilds: 25,
			FailedBuilds:  5,
			Enabled:       false,
		},
	}

	for _, b := range builders {
		body, _ := json.Marshal(b)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builders/register", bytes.NewReader(body))
		w := httptest.NewRecorder()
		server.handleBuilderRegister(w, req)
	}

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

				if stats["total_builders"].(float64) != 3 {
					t.Errorf("Expected 3 total builders, got %v", stats["total_builders"])
				}

				// Check builders array
				buildersArray, ok := result["builders"].([]interface{})
				if !ok {
					t.Fatal("Expected builders array in response")
				}

				if len(buildersArray) != 3 {
					t.Errorf("Expected 3 builders, got %d", len(buildersArray))
				}
			}
		})
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

	// Verify the builder was updated via status endpoint
	req = httptest.NewRequest(http.MethodGet, "/api/v1/builders/status", nil)
	w = httptest.NewRecorder()
	server.handleBuildersStatus(w, req)

	var result map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&result)

	buildersArray := result["builders"].([]interface{})
	found := false
	for _, b := range buildersArray {
		builderMap := b.(map[string]interface{})
		if builderMap["id"] == "test-builder" {
			found = true
			if builderMap["status"] != "busy" {
				t.Errorf("Expected status 'busy', got %v", builderMap["status"])
			}
			if builderMap["current_load"] != float64(3) {
				t.Errorf("Expected current_load 3, got %v", builderMap["current_load"])
			}
		}
	}

	if !found {
		t.Error("Builder not found in status response")
	}
}
