// Package server implements the core Portage Engine server functionality.
package server

import (
	"encoding/json"
	"log"
	"net/http"
	"runtime"
	"time"

	"github.com/slchris/portage-engine/internal/binpkg"
	"github.com/slchris/portage-engine/internal/builder"
	"github.com/slchris/portage-engine/internal/metrics"
	"github.com/slchris/portage-engine/pkg/config"
)

// Server represents the Portage Engine server.
type Server struct {
	config          *config.ServerConfig
	binpkgStore     *binpkg.Store
	builder         *builder.Manager
	builderRegistry *builder.Registry
	metrics         *metrics.Metrics
}

// New creates a new Server instance.
func New(cfg *config.ServerConfig) *Server {
	metricsCfg := &metrics.Config{
		Enabled:  cfg.MetricsEnabled,
		Port:     cfg.MetricsPort,
		Password: cfg.MetricsPassword,
	}

	return &Server{
		config:          cfg,
		binpkgStore:     binpkg.NewStore(cfg.BinpkgPath),
		builder:         builder.NewManager(cfg),
		builderRegistry: builder.NewRegistry(60*time.Second, 30*time.Second),
		metrics:         metrics.New(metricsCfg),
	}
}

// Router returns the HTTP router for the server.
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	// Package query endpoints
	mux.HandleFunc("/api/v1/packages/query", s.handlePackageQuery)
	mux.HandleFunc("/api/v1/packages/request-build", s.handleBuildRequest)
	mux.HandleFunc("/api/v1/packages/status", s.handleBuildStatus)

	// Build management endpoints
	mux.HandleFunc("/api/v1/builds/list", s.handleBuildsList)
	mux.HandleFunc("/api/v1/builds/submit", s.handleSubmitBuildWithConfig)
	mux.HandleFunc("/api/v1/builds/logs", s.handleBuildLogs)
	mux.HandleFunc("/api/v1/cluster/status", s.handleClusterStatus)
	mux.HandleFunc("/api/v1/scheduler/status", s.handleSchedulerStatus)

	// Builder endpoints
	mux.HandleFunc("/api/v1/builders/register", s.handleBuilderRegister)
	mux.HandleFunc("/api/v1/builders/list", s.handleBuildersList)
	mux.HandleFunc("/api/v1/builders/status", s.handleBuildersStatus)

	// Heartbeat endpoint
	mux.HandleFunc("/api/v1/heartbeat", s.handleHeartbeat)

	// Metrics endpoint
	if s.metrics.IsEnabled() {
		mux.Handle("/metrics", s.metrics.Handler())
	}

	// Health check
	mux.HandleFunc("/health", s.handleHealth)

	return s.loggingMiddleware(mux)
}

// handlePackageQuery handles package availability queries.
func (s *Server) handlePackageQuery(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.metrics.IncHTTPRequests()

	if r.Method != http.MethodPost {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req binpkg.QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Query binpkg store
	s.metrics.IncStorageReads()
	pkg, found := s.binpkgStore.Query(&req)

	response := binpkg.QueryResponse{
		Found:   found,
		Package: pkg,
	}

	s.metrics.RecordHTTPLatency("/api/v1/packages/query", time.Since(start))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// handleBuildRequest handles build requests for missing packages.
func (s *Server) handleBuildRequest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.metrics.IncHTTPRequests()

	if r.Method != http.MethodPost {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req builder.BuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Submit build request
	s.metrics.IncBuildsTotal()
	jobID, err := s.builder.SubmitBuild(&req)
	if err != nil {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.metrics.RecordHTTPLatency("/api/v1/packages/request-build", time.Since(start))

	response := builder.BuildResponse{
		JobID:  jobID,
		Status: "queued",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(response)
}

// handleBuildStatus handles build status queries.
func (s *Server) handleBuildStatus(w http.ResponseWriter, r *http.Request) {
	s.metrics.IncHTTPRequests()

	if r.Method != http.MethodGet {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "Missing job_id parameter", http.StatusBadRequest)
		return
	}

	status, err := s.builder.GetStatus(jobID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

// handleSubmitBuildWithConfig handles build requests with configuration bundles.
func (s *Server) handleSubmitBuildWithConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req builder.LocalBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.ConfigBundle == nil {
		http.Error(w, "Missing configuration bundle", http.StatusBadRequest)
		return
	}

	if req.ConfigBundle.Packages == nil || len(req.ConfigBundle.Packages.Packages) == 0 {
		http.Error(w, "No packages specified in bundle", http.StatusBadRequest)
		return
	}

	// For now, this endpoint would need a different builder
	// that supports config bundles. For simplicity, return not implemented.
	http.Error(w, "Config bundle builds not yet implemented on server", http.StatusNotImplemented)
}

// handleHealth handles health check requests.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// handleBuildsList returns all build jobs.
func (s *Server) handleBuildsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	builds := s.builder.ListAllBuilds()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(builds)
}

// handleClusterStatus returns the cluster status.
func (s *Server) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.builder.GetClusterStatus()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

// handleBuildLogs returns logs for a specific build job.
func (s *Server) handleBuildLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		http.Error(w, "Missing job_id parameter", http.StatusBadRequest)
		return
	}

	logs, err := s.builder.GetBuildLogs(jobID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	response := map[string]interface{}{
		"job_id": jobID,
		"logs":   logs,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// handleSchedulerStatus returns scheduler status with task assignments.
func (s *Server) handleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.builder.GetSchedulerStatus()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

// handleBuilderRegister handles builder registration requests.
func (s *Server) handleBuilderRegister(w http.ResponseWriter, r *http.Request) {
	s.metrics.IncHTTPRequests()

	if r.Method != http.MethodPost {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var info builder.BuilderInfo
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Register the builder
	s.builderRegistry.Register(&info)

	response := map[string]interface{}{
		"success": true,
		"message": "Builder registered successfully",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// handleBuildersList returns the list of all registered builders.
func (s *Server) handleBuildersList(w http.ResponseWriter, r *http.Request) {
	s.metrics.IncHTTPRequests()

	if r.Method != http.MethodGet {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	builders := s.builderRegistry.List()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(builders)
}

// handleBuildersStatus returns aggregate status and statistics for all builders.
func (s *Server) handleBuildersStatus(w http.ResponseWriter, r *http.Request) {
	s.metrics.IncHTTPRequests()

	if r.Method != http.MethodGet {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := s.builderRegistry.GetStats()
	builders := s.builderRegistry.List()

	response := map[string]interface{}{
		"stats":    stats,
		"builders": builders,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// handleHeartbeat handles builder heartbeat requests.
func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	s.metrics.IncHTTPRequests()
	s.metrics.IncHeartbeatsTotal()

	if r.Method != http.MethodPost {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req builder.HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.metrics.IncHTTPRequestErrors()
		s.metrics.IncHeartbeatsFailed()
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Update builder registry with heartbeat info
	builderInfo := &builder.BuilderInfo{
		ID:          req.BuilderID,
		Endpoint:    req.Endpoint,
		Status:      req.Status,
		Capacity:    req.Capacity,
		CurrentLoad: req.ActiveJobs,
	}
	s.builderRegistry.Register(builderInfo)

	// Update builder heartbeat in the scheduler
	if err := s.builder.UpdateBuilderHeartbeat(&req); err != nil {
		s.metrics.IncHeartbeatsFailed()
		response := builder.HeartbeatResponse{
			Success: false,
			Message: err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(response)
		return
	}

	response := builder.HeartbeatResponse{
		Success: true,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// loggingMiddleware provides request logging.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.Method, r.RequestURI, r.RemoteAddr)
		s.metrics.UpdateGoroutines(int64(runtime.NumGoroutine()))
		next.ServeHTTP(w, r)
	})
}
