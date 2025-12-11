package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/slchris/portage-engine/internal/dashboard"
	"github.com/slchris/portage-engine/pkg/config"
)

func TestDashboardHealth(t *testing.T) {
	cfg := &config.DashboardConfig{
		Port:      8081,
		ServerURL: "http://localhost:8080",
	}

	dash := dashboard.New(cfg)
	router := dash.Router()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestDashboardConfigLoad(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "dashboard.conf")

	configData := `port=8081
server_url=http://localhost:8080
refresh_sec=30
`

	if err := os.WriteFile(configPath, []byte(configData), 0600); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := config.LoadDashboardConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Port != 8081 {
		t.Errorf("Expected port 8081, got %d", cfg.Port)
	}

	if cfg.ServerURL != "http://localhost:8080" {
		t.Errorf("Expected server URL 'http://localhost:8080', got '%s'", cfg.ServerURL)
	}
}

func TestDashboardStartStop(t *testing.T) {
	cfg := &config.DashboardConfig{
		Port:      0,
		ServerURL: "http://localhost:8080",
	}

	dash := dashboard.New(cfg)

	httpServer := &http.Server{
		Addr:         ":0",
		Handler:      dash.Router(),
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
		IdleTimeout:  1 * time.Second,
	}

	go func() {
		_ = httpServer.ListenAndServe()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		t.Errorf("Failed to shutdown dashboard: %v", err)
	}
}

func TestDashboardRouter(t *testing.T) {
	cfg := &config.DashboardConfig{
		Port:      8081,
		ServerURL: "http://localhost:8080",
	}

	dash := dashboard.New(cfg)
	router := dash.Router()

	if router == nil {
		t.Fatal("Expected non-nil router")
	}
}

func TestDashboardRoutes(t *testing.T) {
	cfg := &config.DashboardConfig{
		Port:      8081,
		ServerURL: "http://localhost:8080",
	}

	dash := dashboard.New(cfg)
	router := dash.Router()

	tests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{"Root Page", http.MethodGet, "/", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.status {
				t.Errorf("Expected status %d, got %d", tt.status, w.Code)
			}
		})
	}
}

func TestDashboardGracefulShutdown(t *testing.T) {
	cfg := &config.DashboardConfig{
		Port:      0,
		ServerURL: "http://localhost:8080",
	}

	dash := dashboard.New(cfg)

	httpServer := &http.Server{
		Addr:         ":0",
		Handler:      dash.Router(),
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
		IdleTimeout:  1 * time.Second,
	}

	errChan := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
		close(errChan)
	}()

	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		t.Errorf("Failed to shutdown dashboard gracefully: %v", err)
	}

	if err := <-errChan; err != nil {
		t.Errorf("Dashboard error: %v", err)
	}
}
