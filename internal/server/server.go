// Package server implements the core Portage Engine server functionality.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/slchris/portage-engine/internal/binpkg"
	"github.com/slchris/portage-engine/internal/builder"
	"github.com/slchris/portage-engine/internal/gpg"
	"github.com/slchris/portage-engine/internal/metrics"
	"github.com/slchris/portage-engine/pkg/config"
)

// Version information (set via -ldflags at build time).
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Server represents the Portage Engine server.
type Server struct {
	config          *config.ServerConfig
	binpkgStore     *binpkg.Store
	builder         *builder.Manager
	builderRegistry *builder.Registry
	metrics         *metrics.Metrics
	gpgSigner       *gpg.Signer
	startTime       time.Time
	store           *ServerStore
	persister       *ServerPersister
	binhostStop     chan struct{}
	settingsMu      sync.Mutex // serializes settings updates + persistence
}

// New creates a new Server instance.
func New(cfg *config.ServerConfig) *Server {
	metricsCfg := &metrics.Config{
		Enabled:  cfg.MetricsEnabled,
		Port:     cfg.MetricsPort,
		Password: cfg.MetricsPassword,
	}

	// Create GPG signer with options
	var opts []gpg.SignerOption
	if cfg.GPGHome != "" {
		opts = append(opts, gpg.WithGnupgHome(cfg.GPGHome))
	}
	if cfg.GPGAutoCreate {
		opts = append(opts, gpg.WithAutoCreate(cfg.GPGKeyName, cfg.GPGKeyEmail))
	}
	signer := gpg.NewSigner(cfg.GPGKeyID, cfg.GPGKeyPath, cfg.GPGEnabled, opts...)

	s := &Server{
		config:          cfg,
		binpkgStore:     binpkg.NewStore(cfg.BinpkgPath),
		builder:         builder.NewManager(cfg),
		builderRegistry: builder.NewRegistry(60*time.Second, 30*time.Second),
		metrics:         metrics.New(metricsCfg),
		gpgSigner:       signer,
		startTime:       time.Now(),
	}

	// When a build's artifact lands in the binhost PKGDIR, refresh the
	// Packages index right away so clients see the new package without
	// waiting for the periodic refresher.
	s.builder.SetArtifactStoredHook(func() {
		if _, err := s.binpkgStore.RegenerateIndex(s.binhostArch()); err != nil {
			log.Printf("Warning: binhost index refresh after artifact ingest failed: %v", err)
		}
	})

	return s
}

// Initialize initializes the server, including GPG key setup and state persistence.
func (s *Server) Initialize() error {
	if s.gpgSigner.IsEnabled() {
		log.Printf("Initializing GPG signer...")
		if err := s.gpgSigner.Initialize(); err != nil {
			return fmt.Errorf("failed to initialize GPG: %w", err)
		}

		// Export public key if configured
		if s.config.GPGPublicKeyPath != "" {
			if err := s.gpgSigner.ExportPublicKey(s.config.GPGPublicKeyPath); err != nil {
				log.Printf("Warning: failed to export public key: %v", err)
			}
		}

		log.Printf("GPG signer initialized with key: %s", s.gpgSigner.KeyID())
	}

	// Initialize persistence
	if err := s.initPersistence(); err != nil {
		// Persistence failure is non-fatal — log and continue
		log.Printf("Warning: failed to initialize persistence: %v (server will run without state persistence)", err)
	}

	// Apply dashboard-managed cloud settings saved from a previous run (these
	// override the static conf/env values).
	s.loadCloudSettingsOverride()

	// Re-enable dashboard-managed GPG signing from a previous run.
	s.loadGPGRuntime()

	// Binhost signing key material for builder deployment, verify-side pubkey
	// import, and mirror publication.
	s.builder.SetGPGKeyProvider(s.gpgKeyMaterial)

	// Build the binhost Packages index from whatever is already on disk so
	// clients can immediately consume this server as a binhost, then keep it
	// fresh in the background as new packages appear in PKGDIR.
	if n, err := s.binpkgStore.RegenerateIndex(s.binhostArch()); err != nil {
		log.Printf("Warning: failed to generate binhost index: %v", err)
	} else {
		log.Printf("Binhost index generated with %d package(s) at %s/Packages", n, s.binpkgStore.BasePath())
	}
	s.startBinhostRefresher(5 * time.Minute)

	return nil
}

// startBinhostRefresher periodically regenerates the binhost index so packages
// added to PKGDIR out of band become visible to emerge without a restart.
func (s *Server) startBinhostRefresher(interval time.Duration) {
	s.binhostStop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.binhostStop:
				return
			case <-ticker.C:
				if _, err := s.binpkgStore.RegenerateIndex(s.binhostArch()); err != nil {
					log.Printf("Warning: failed to refresh binhost index: %v", err)
				}
			}
		}
	}()
}

// binhostArch returns the ARCH advertised in the binhost Packages preamble.
func (s *Server) binhostArch() string {
	// A single-arch binhost is the common case; default to amd64 when unset.
	// (Portage tolerates a missing/empty ARCH and falls back to per-package
	// KEYWORDS, but advertising one is friendlier.)
	return "amd64"
}

// initPersistence sets up the server store and loads any previously saved state.
func (s *Server) initPersistence() error {
	store, err := NewServerStore(s.config.DataDir)
	if err != nil {
		return fmt.Errorf("failed to create server store: %w", err)
	}
	s.store = store

	// Load previously persisted jobs
	jobs, err := store.LoadJobs()
	if err != nil {
		log.Printf("Warning: failed to load persisted jobs: %v", err)
	} else if len(jobs) > 0 {
		s.builder.LoadJobs(jobs)
		log.Printf("Loaded %d persisted jobs from disk", len(jobs))
	}

	// Start periodic persistence (save every 30s, clean jobs older than 7 days)
	s.persister = NewServerPersister(
		store,
		s.builder.GetJobsSnapshot,
		30*time.Second,
		7*24*time.Hour,
	)
	s.persister.Start()
	log.Printf("Server persistence started (data dir: %s)", s.config.DataDir)

	return nil
}

// Shutdown gracefully shuts down the server components.
// It saves state, stops the builder, and closes the registry.
func (s *Server) Shutdown() {
	log.Println("Shutting down server components...")

	// Stop the binhost index refresher.
	if s.binhostStop != nil {
		close(s.binhostStop)
		s.binhostStop = nil
	}

	// Stop persistence first (performs final save)
	if s.persister != nil {
		log.Println("Saving server state to disk...")
		s.persister.Stop()
	}

	// Shutdown builder manager (closes work queue, stops IaC cleanup)
	if s.builder != nil {
		s.builder.Shutdown()
	}

	// Close builder registry
	if s.builderRegistry != nil {
		s.builderRegistry.Close()
	}

	log.Println("Server components shut down")
}

// GPGSigner returns the GPG signer instance.
func (s *Server) GPGSigner() *gpg.Signer {
	return s.gpgSigner
}

// Router returns the HTTP router for the server.
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	// Package query endpoints
	mux.HandleFunc("/api/v1/packages/query", s.handlePackageQuery)
	mux.HandleFunc("/api/v1/packages/request-build", s.handleBuildRequest)
	mux.HandleFunc("/api/v1/packages/status", s.handleBuildStatus)

	// Build management endpoints
	mux.HandleFunc("/api/v1/settings/cloud", s.handleCloudSettings)
	mux.HandleFunc("/api/v1/settings/cloud/test", s.handleCloudSettingsTest)
	mux.HandleFunc("/api/v1/instances", s.handleInstancesList)
	mux.HandleFunc("/api/v1/instances/shell", s.handleInstanceShell)
	mux.HandleFunc("/api/v1/builds/delete", s.handleBuildDelete)
	mux.HandleFunc("/api/v1/builds/cleanup-failed", s.handleBuildsCleanupFailed)
	mux.HandleFunc("/api/v1/builds/list", s.handleBuildsList)
	mux.HandleFunc("/api/v1/builds/submit", s.handleSubmitBuildWithConfig)
	mux.HandleFunc("/api/v1/builds/status", s.handleBuildStatus)
	mux.HandleFunc("/api/v1/builds/logs", s.handleBuildLogs)
	mux.HandleFunc("/api/v1/cluster/status", s.handleClusterStatus)
	mux.HandleFunc("/api/v1/scheduler/status", s.handleSchedulerStatus)

	// Builder endpoints
	mux.HandleFunc("/api/v1/builders/register", s.handleBuilderRegister)
	mux.HandleFunc("/api/v1/builders/list", s.handleBuildersList)
	mux.HandleFunc("/api/v1/builders/status", s.handleBuildersStatus)

	// Artifact download proxy endpoints
	mux.HandleFunc("/api/v1/artifacts/download/", s.handleArtifactDownload)
	mux.HandleFunc("/api/v1/artifacts/info/", s.handleArtifactInfo)

	// Binhost: serve the PKGDIR (including the Packages index) so a stock
	// `emerge --getbinpkg` can consume this server. This is intentionally public
	// (emerge cannot present the API key) and read-only.
	binhostFS := http.FileServer(http.Dir(s.binpkgStore.BasePath()))
	mux.Handle("/binpkgs/", http.StripPrefix("/binpkgs/", s.binhostReadOnly(binhostFS)))

	// GPG endpoint
	mux.HandleFunc("/api/v1/gpg/public-key", s.handleGPGPublicKey)
	mux.HandleFunc("/api/v1/gpg/status", s.handleGPGStatus)
	mux.HandleFunc("/api/v1/gpg/generate", s.handleGPGGenerate)
	mux.HandleFunc("/api/v1/gpg/pubkey", s.handleGPGPubkey)

	// Heartbeat endpoint
	mux.HandleFunc("/api/v1/heartbeat", s.handleHeartbeat)

	// Metrics endpoints
	if s.metrics.IsEnabled() {
		mux.Handle("/metrics", s.metrics.Handler())                      // Legacy expvar JSON
		mux.Handle("/metrics/prometheus", s.metrics.PrometheusHandler()) // Prometheus text format
	}

	// Health / readiness / liveness probes (always public)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/livez", s.handleLivez)

	// Stack middleware (outermost first):
	// requestID → enhancedLogging → CORS → maxBodySize → apiKey auth
	var handler http.Handler = mux
	handler = s.apiKeyAuthMiddleware(handler)
	handler = s.maxBodySizeMiddleware(handler)
	handler = s.corsMiddleware(handler)
	handler = s.enhancedLoggingMiddleware(handler)
	handler = s.requestIDMiddleware(handler)

	return handler
}

// corsMiddleware adds CORS headers using the configured allowed origins.
// If no origins are configured, it falls back to "*" for backward compatibility.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := s.isOriginAllowed(origin)

		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else if len(s.config.CORSAllowedOrigins) == 0 {
			// No whitelist configured: allow all for backward compatibility
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		// If origin is not allowed and a whitelist IS configured, do not set
		// the Allow-Origin header at all — the browser will block the request.

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isOriginAllowed checks whether the given origin is in the whitelist.
func (s *Server) isOriginAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	for _, o := range s.config.CORSAllowedOrigins {
		if o == origin || o == "*" {
			return true
		}
	}
	return false
}

// apiKeyAuthMiddleware protects API endpoints with a shared API key.
// Public endpoints (/health, /readyz, /livez, /metrics) are excluded.
// If APIKey is empty in config, the middleware is a no-op (backward compatible).
func (s *Server) apiKeyAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if no API key is configured
		if s.config.APIKey == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Public endpoints that never require auth. The binhost (/binpkgs/) is
		// public because emerge cannot present the API key; it is read-only.
		path := r.URL.Path
		if path == "/health" || path == "/readyz" || path == "/livez" || path == "/metrics" || path == "/metrics/prometheus" ||
			strings.HasPrefix(path, "/binpkgs/") {
			next.ServeHTTP(w, r)
			return
		}

		// CORS preflight must pass through
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Check API key from X-API-Key header or Authorization: Bearer <key>
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				apiKey = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		// Constant-time comparison to avoid leaking the key via timing.
		if subtle.ConstantTimeCompare([]byte(apiKey), []byte(s.config.APIKey)) != 1 {
			s.metrics.IncHTTPRequestErrors()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "unauthorized: invalid or missing API key",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// maxBodySizeMiddleware limits the size of incoming request bodies to prevent
// abuse. POST/PUT/PATCH methods are limited; GET/DELETE/OPTIONS pass through.
func (s *Server) maxBodySizeMiddleware(next http.Handler) http.Handler {
	maxBytes := s.config.MaxRequestBodyBytes
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024 // Default 10MB
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// --- Health, Readiness, and Liveness Probes ---

// handleHealth handles health check requests.
// Returns overall system health including version and component readiness.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	// Check component health
	storageOK := s.checkStorageHealth()
	buildersOnline, buildersTotal := s.checkBuildersHealth()

	overallStatus := "healthy"
	if !storageOK {
		overallStatus = "degraded"
	}
	if buildersTotal > 0 && buildersOnline == 0 {
		overallStatus = "degraded"
	}

	response := map[string]interface{}{
		"status":  overallStatus,
		"version": Version,
		"commit":  Commit,
		"build":   BuildTime,
		"checks": map[string]interface{}{
			"storage": map[string]interface{}{
				"ok":   storageOK,
				"type": s.config.StorageType,
			},
			"builders": map[string]interface{}{
				"online": buildersOnline,
				"total":  buildersTotal,
			},
		},
		"uptime": time.Since(s.startTime).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	if overallStatus != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(response)
}

// handleReadyz checks if the server is ready to accept traffic.
// Returns 200 if ready, 503 if not.
func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	storageOK := s.checkStorageHealth()
	if !storageOK {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "not ready", "reason": "storage unavailable"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// handleLivez checks if the server process is alive.
// Always returns 200 as long as the process is running.
func (s *Server) handleLivez(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "alive"})
}

// checkStorageHealth verifies the storage backend is accessible.
func (s *Server) checkStorageHealth() bool {
	switch s.config.StorageType {
	case "local":
		dir := s.config.StorageLocalDir
		if dir == "" {
			dir = s.config.BinpkgPath
		}
		info, err := os.Stat(dir)
		return err == nil && info.IsDir()
	default:
		// For non-local storage, assume OK (actual check would require SDK calls)
		return true
	}
}

// checkBuildersHealth returns (online, total) counts of configured remote builders.
func (s *Server) checkBuildersHealth() (online, total int) {
	builders := s.builderRegistry.List()
	total = len(builders)
	for _, b := range builders {
		if b.Status == "online" || b.Status == "busy" {
			online++
		}
	}
	// Also count configured but unregistered remote builders
	if total == 0 {
		total = len(s.builder.CloudSettings().RemoteBuilders)
	}
	return online, total
}
