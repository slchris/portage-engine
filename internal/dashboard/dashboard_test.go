package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// TestHandleLogin tests the login handler with valid credentials.
func TestHandleLogin(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:       "http://localhost:8080",
		AuthEnabled:     true,
		AllowAnonymous:  false,
		JWTSecret:       "test-secret-that-is-at-least-32-chars-long",
		AdminUser:       "testuser",
		AdminPassword:   "testpass",
		TokenTTLMinutes: 60,
	}

	dashboard := New(cfg)

	body, err := json.Marshal(map[string]string{"username": "testuser", "password": "testpass"})
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	w := httptest.NewRecorder()

	dashboard.handleLogin(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// The issued token must verify against the configured secret.
	if err := verifyToken(cfg.JWTSecret, out["token"], time.Now()); err != nil {
		t.Errorf("issued token does not verify: %v", err)
	}
}

// TestHandleLoginRejectsBadCredentials ensures wrong credentials are refused.
func TestHandleLoginRejectsBadCredentials(t *testing.T) {
	cfg := &config.DashboardConfig{
		AuthEnabled:   true,
		JWTSecret:     "test-secret-that-is-at-least-32-chars-long",
		AdminUser:     "testuser",
		AdminPassword: "testpass",
	}
	dashboard := New(cfg)

	body, _ := json.Marshal(map[string]string{"username": "testuser", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	w := httptest.NewRecorder()

	dashboard.handleLogin(w, req)

	if got := w.Result().StatusCode; got != http.StatusUnauthorized {
		t.Errorf("Expected status 401 for bad credentials, got %d", got)
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
	// Backend up: real data is proxied through as 200.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"active_builds": 1, "total_builds": 3})
	}))
	defer backend.Close()

	d := New(&config.DashboardConfig{ServerURL: backend.URL, AllowAnonymous: true})
	w := httptest.NewRecorder()
	d.handleStatus(w, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("backend up: expected 200, got %d", w.Result().StatusCode)
	}

	// Backend down: honest 502, NOT fabricated 200 data.
	d2 := New(&config.DashboardConfig{ServerURL: "http://127.0.0.1:1", AllowAnonymous: true})
	w2 := httptest.NewRecorder()
	d2.handleStatus(w2, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if w2.Result().StatusCode != http.StatusBadGateway {
		t.Errorf("backend down: expected 502, got %d", w2.Result().StatusCode)
	}
}

// TestHandleBuilds tests the builds API endpoint.
func TestHandleBuilds(t *testing.T) {
	// Backend down must be an honest 502, not fabricated sample builds.
	d := New(&config.DashboardConfig{ServerURL: "http://127.0.0.1:1", AllowAnonymous: true})
	w := httptest.NewRecorder()
	d.handleBuilds(w, httptest.NewRequest(http.MethodGet, "/api/builds", nil))
	if w.Result().StatusCode != http.StatusBadGateway {
		t.Errorf("backend down: expected 502, got %d", w.Result().StatusCode)
	}
	if strings.Contains(w.Body.String(), "sample-job") {
		t.Error("response contains fabricated sample data")
	}
}

// TestHandleKeyEndpointsProxyRealKey verifies the key endpoints serve the
// server's real GPG key (not the old hardcoded fake key).
func TestHandleKeyEndpointsProxyRealKey(t *testing.T) {
	const realKey = "-----BEGIN PGP PUBLIC KEY BLOCK-----\nREALKEYDATA\n-----END PGP PUBLIC KEY BLOCK-----\n"
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/gpg/public-key" {
			_, _ = w.Write([]byte(realKey))
			return
		}
		http.NotFound(w, r)
	}))
	defer backend.Close()

	d := New(&config.DashboardConfig{ServerURL: backend.URL, AllowAnonymous: true})

	w := httptest.NewRecorder()
	d.handlePublicKeyAPI(w, httptest.NewRequest(http.MethodGet, "/api/keys/public", nil))
	body := w.Body.String()
	if !strings.Contains(body, "REALKEYDATA") {
		t.Errorf("public key endpoint did not proxy the real key: %s", body)
	}
	if strings.Contains(body, "portage-engine-2024") {
		t.Error("public key endpoint still returns the hardcoded fake key")
	}

	// Backend down → 502, not a fake key.
	d2 := New(&config.DashboardConfig{ServerURL: "http://127.0.0.1:1", AllowAnonymous: true})
	w2 := httptest.NewRecorder()
	d2.handleKeyInfoAPI(w2, httptest.NewRequest(http.MethodGet, "/api/keys/info", nil))
	if w2.Result().StatusCode != http.StatusBadGateway {
		t.Errorf("key info with backend down: expected 502, got %d", w2.Result().StatusCode)
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

// TestAuthMiddleware verifies the middleware rejects missing/invalid tokens on
// protected routes and accepts a validly signed token.
func TestAuthMiddleware(t *testing.T) {
	cfg := &config.DashboardConfig{
		ServerURL:      "http://localhost:8080",
		AuthEnabled:    true,
		AllowAnonymous: false,
		JWTSecret:      "test-secret-that-is-at-least-32-chars-long",
	}

	dashboard := New(cfg)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := dashboard.authMiddleware(ok)

	// A protected path (not /, /login, or /static/) with no token → 401.
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if got := w.Result().StatusCode; got != http.StatusUnauthorized {
		t.Errorf("no token: expected 401, got %d", got)
	}

	// An invalid token → 401.
	req = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if got := w.Result().StatusCode; got != http.StatusUnauthorized {
		t.Errorf("bad token: expected 401, got %d", got)
	}

	// A validly signed token → 200.
	token, err := signToken(cfg.JWTSecret, "admin", time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if got := w.Result().StatusCode; got != http.StatusOK {
		t.Errorf("valid token: expected 200, got %d", got)
	}

	// The login and index paths must stay reachable without a token.
	for _, p := range []string{"/login", "/"} {
		req = httptest.NewRequest(http.MethodGet, p, nil)
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if got := w.Result().StatusCode; got != http.StatusOK {
			t.Errorf("public path %s: expected 200, got %d", p, got)
		}
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

// TestHandleBuildsPage verifies the /builds page renders (was a 500 due to a
// missing "builds" template).
func TestHandleBuildsPage(t *testing.T) {
	cfg := &config.DashboardConfig{ServerURL: "http://localhost:8080", AuthEnabled: false, AllowAnonymous: true}
	dashboard := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/builds", nil)
	w := httptest.NewRecorder()
	dashboard.handleBuildsPage(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Result().StatusCode)
	}
	if !strings.Contains(w.Body.String(), "Build Jobs") {
		t.Errorf("expected builds page content, got: %s", w.Body.String()[:min(200, w.Body.Len())])
	}
}
