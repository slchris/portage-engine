package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/slchris/portage-engine/internal/server"
	"github.com/slchris/portage-engine/pkg/config"
)

func TestServerHealth(t *testing.T) {
	cfg := &config.ServerConfig{
		Port:        8080,
		StorageType: "local",
	}

	srv := server.New(cfg)
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestServerConfigLoad(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "server.conf")

	configData := `port=8080
storage=local
storage_path=/tmp/packages
builder_urls=http://localhost:9090,http://localhost:9091
`

	if err := os.WriteFile(configPath, []byte(configData), 0600); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := config.LoadServerConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", cfg.Port)
	}

	if cfg.StorageType != "local" {
		t.Errorf("Expected storage 'local', got '%s'", cfg.StorageType)
	}
}

func TestServerStartStop(t *testing.T) {
	cfg := &config.ServerConfig{
		Port:        0,
		StorageType: "local",
	}

	srv := server.New(cfg)

	httpServer := &http.Server{
		Addr:         ":0",
		Handler:      srv.Router(),
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
		t.Errorf("Failed to shutdown server: %v", err)
	}
}

func TestServerRouter(t *testing.T) {
	cfg := &config.ServerConfig{
		Port:        8080,
		StorageType: "local",
	}

	srv := server.New(cfg)
	router := srv.Router()

	if router == nil {
		t.Fatal("Expected non-nil router")
	}
}

func TestServerAPIRoutes(t *testing.T) {
	cfg := &config.ServerConfig{
		Port:        8080,
		StorageType: "local",
	}

	srv := server.New(cfg)
	router := srv.Router()

	tests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{"Health Check", http.MethodGet, "/health", http.StatusOK},
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

func TestServerGracefulShutdown(t *testing.T) {
	cfg := &config.ServerConfig{
		Port:        0,
		StorageType: "local",
	}

	srv := server.New(cfg)

	httpServer := &http.Server{
		Addr:         ":0",
		Handler:      srv.Router(),
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
		t.Errorf("Failed to shutdown server gracefully: %v", err)
	}

	if err := <-errChan; err != nil {
		t.Errorf("Server error: %v", err)
	}
}
