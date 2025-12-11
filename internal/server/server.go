// Package server implements the core Portage Engine server functionality.
package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/slchris/portage-engine/internal/binpkg"
	"github.com/slchris/portage-engine/internal/builder"
	"github.com/slchris/portage-engine/pkg/config"
)

// Server represents the Portage Engine server.
type Server struct {
	config      *config.ServerConfig
	binpkgStore *binpkg.Store
	builder     *builder.Manager
}

// New creates a new Server instance.
func New(cfg *config.ServerConfig) *Server {
	return &Server{
		config:      cfg,
		binpkgStore: binpkg.NewStore(cfg.BinpkgPath),
		builder:     builder.NewManager(cfg),
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
	mux.HandleFunc("/api/v1/cluster/status", s.handleClusterStatus)

	// Health check
	mux.HandleFunc("/health", s.handleHealth)

	return s.loggingMiddleware(mux)
}

// handlePackageQuery handles package availability queries.
func (s *Server) handlePackageQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req binpkg.QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Query binpkg store
	pkg, found := s.binpkgStore.Query(&req)

	response := binpkg.QueryResponse{
		Found:   found,
		Package: pkg,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// handleBuildRequest handles build requests for missing packages.
func (s *Server) handleBuildRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req builder.BuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Submit build request
	jobID, err := s.builder.SubmitBuild(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

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
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
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

// loggingMiddleware provides request logging.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.Method, r.RequestURI, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}
