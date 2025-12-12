package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slchris/portage-engine/pkg/config"
)

// TestNew tests creating a new dashboard.
func TestNew(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    false,
		AllowAnonymous: true,
	}

	dashboard := New(cfg)
	if dashboard == nil {
		t.Fatal("New returned nil")
	}

	if dashboard.config != cfg {
		t.Error("Config not set correctly")
	}

	if dashboard.templates == nil {
		t.Error("Templates not initialized")
	}

	if dashboard.httpClient == nil {
		t.Error("HTTP client not initialized")
	}
}

// TestRouter tests the HTTP router setup.
func TestRouter(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    false,
		AllowAnonymous: true,
	}

	dashboard := New(cfg)
	router := dashboard.Router()

	if router == nil {
		t.Fatal("Router returned nil")
	}
}

// TestHandleIndex tests the index page handler.
func TestHandleIndex(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    false,
		AllowAnonymous: true,
	}

	dashboard := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	dashboard.handleIndex(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestHandleIndexNotFound tests 404 handling.
func TestHandleIndexNotFound(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    false,
		AllowAnonymous: true,
	}

	dashboard := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/invalid-path", nil)
	w := httptest.NewRecorder()

	dashboard.handleIndex(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}
}

// TestHandleLogin tests the login handler.
func TestHandleLogin(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    true,
		AllowAnonymous: false,
	}

	dashboard := New(cfg)

	loginReq := map[string]string{
		"username": "testuser",
		"password": "testpass",
	}

	body, err := json.Marshal(loginReq)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	w := httptest.NewRecorder()

	dashboard.handleLogin(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestHandleLoginMethodNotAllowed tests method validation.
func TestHandleLoginMethodNotAllowed(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    true,
		AllowAnonymous: false,
	}

	dashboard := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	w := httptest.NewRecorder()

	dashboard.handleLogin(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

// TestHandleStatus tests the status API endpoint.
func TestHandleStatus(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    false,
		AllowAnonymous: true,
	}

	dashboard := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	dashboard.handleStatus(w, req)

	resp := w.Result()
	// Status may be 200 or 500 depending on server connectivity
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected status 200 or 500, got %d", resp.StatusCode)
	}
}

// TestHandleBuilds tests the builds API endpoint.
func TestHandleBuilds(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    false,
		AllowAnonymous: true,
	}

	dashboard := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/builds", nil)
	w := httptest.NewRecorder()

	dashboard.handleBuilds(w, req)

	resp := w.Result()
	// May return 200 or 500 depending on server connectivity
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected status 200 or 500, got %d", resp.StatusCode)
	}
}

// TestHandleInstances tests the instances API endpoint.
func TestHandleInstances(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    false,
		AllowAnonymous: true,
	}

	dashboard := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	w := httptest.NewRecorder()

	dashboard.handleInstances(w, req)

	resp := w.Result()
	// May return 200 or 500 depending on server connectivity
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected status 200 or 500, got %d", resp.StatusCode)
	}
}

// TestAuthMiddleware tests authentication middleware.
func TestAuthMiddleware(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    true,
		AllowAnonymous: false,
	}

	dashboard := New(cfg)

	// Test without auth header
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler := dashboard.authMiddleware(http.HandlerFunc(dashboard.handleIndex))
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", resp.StatusCode)
	}
}

// TestHandleArtifactInfo tests the artifact info endpoint.
func TestHandleArtifactInfo(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    false,
		AllowAnonymous: true,
	}

	dashboard := New(cfg)

	// Test method not allowed
	req := httptest.NewRequest(http.MethodPost, "/api/artifacts/info/test-job-id", nil)
	w := httptest.NewRecorder()

	dashboard.handleArtifactInfo(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}

	// Test missing job ID
	req = httptest.NewRequest(http.MethodGet, "/api/artifacts/info/", nil)
	w = httptest.NewRecorder()

	dashboard.handleArtifactInfo(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400 for missing job ID, got %d", resp.StatusCode)
	}
}

// TestHandleArtifactDownload tests the artifact download endpoint.
func TestHandleArtifactDownload(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    false,
		AllowAnonymous: true,
	}

	dashboard := New(cfg)

	// Test method not allowed
	req := httptest.NewRequest(http.MethodPost, "/api/artifacts/download/test-job-id", nil)
	w := httptest.NewRecorder()

	dashboard.handleArtifactDownload(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}

	// Test missing job ID
	req = httptest.NewRequest(http.MethodGet, "/api/artifacts/download/", nil)
	w = httptest.NewRecorder()

	dashboard.handleArtifactDownload(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400 for missing job ID, got %d", resp.StatusCode)
	}
}
