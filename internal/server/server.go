// Package server implements the core Portage Engine server functionality.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"sync"
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
	mux.HandleFunc("/api/v1/builds/status", s.handleBuildStatus)
	mux.HandleFunc("/api/v1/builds/logs", s.handleBuildLogs)
	mux.HandleFunc("/api/v1/cluster/status", s.handleClusterStatus)
	mux.HandleFunc("/api/v1/scheduler/status", s.handleSchedulerStatus)

	// Builder endpoints
	mux.HandleFunc("/api/v1/builders/register", s.handleBuilderRegister)
	mux.HandleFunc("/api/v1/builders/list", s.handleBuildersList)
	mux.HandleFunc("/api/v1/builders/status", s.handleBuildersStatus)

	// GPG endpoint
	mux.HandleFunc("/api/v1/gpg/public-key", s.handleGPGPublicKey)

	// Heartbeat endpoint
	mux.HandleFunc("/api/v1/heartbeat", s.handleHeartbeat)

	// Metrics endpoint
	if s.metrics.IsEnabled() {
		mux.Handle("/metrics", s.metrics.Handler())
	}

	// Health check
	mux.HandleFunc("/health", s.handleHealth)

	return s.corsMiddleware(s.loggingMiddleware(mux))
}

// corsMiddleware adds CORS headers to allow cross-origin requests.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
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

	// Support both formats: {"package_name":"cat/pkg"} and {"category":"cat","package":"pkg"}
	var rawReq map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&rawReq); err != nil {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var req builder.BuildRequest

	// Convert to BuildRequest format
	if packageName, ok := rawReq["package_name"].(string); ok {
		req.PackageName = packageName
	} else if category, okCat := rawReq["category"].(string); okCat {
		if pkg, okPkg := rawReq["package"].(string); okPkg {
			req.PackageName = category + "/" + pkg
		}
	}

	if version, ok := rawReq["version"].(string); ok {
		req.Version = version
	}

	if useFlags, ok := rawReq["use_flags"].([]interface{}); ok {
		req.UseFlags = make([]string, len(useFlags))
		for i, flag := range useFlags {
			if flagStr, ok := flag.(string); ok {
				req.UseFlags[i] = flagStr
			}
		}
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

	// Get limit parameter (default 0 = all, max 200)
	limitStr := r.URL.Query().Get("limit")
	limit := 0
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
			if limit > 200 {
				limit = 200
			}
		}
	}

	builds := s.builder.ListAllBuilds()

	// Sort by created_at descending (newest first) for stable ordering
	sort.Slice(builds, func(i, j int) bool {
		return builds[i].CreatedAt.After(builds[j].CreatedAt)
	})

	// Apply limit if specified
	if limit > 0 && len(builds) > limit {
		builds = builds[:limit]
	}

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

	// Fetch real-time status from all configured remote builders
	builders := s.fetchAllBuilderStatus()

	// Calculate aggregate statistics
	stats := calculateBuilderStats(builders)

	response := map[string]interface{}{
		"stats":    stats,
		"builders": builders,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// BuilderStatusInfo represents status information from a builder.
type BuilderStatusInfo struct {
	ID            string  `json:"id"`
	Endpoint      string  `json:"endpoint"`
	Architecture  string  `json:"architecture"`
	Status        string  `json:"status"`
	Capacity      int     `json:"capacity"`
	CurrentLoad   int     `json:"current_load"`
	Enabled       bool    `json:"enabled"`
	CPUUsage      float64 `json:"cpu_usage"`
	MemoryUsage   float64 `json:"memory_usage"`
	DiskUsage     float64 `json:"disk_usage"`
	TotalBuilds   int     `json:"total_builds"`
	SuccessBuilds int     `json:"success_builds"`
	FailedBuilds  int     `json:"failed_builds"`
}

// fetchAllBuilderStatus queries all configured remote builders for their status.
func (s *Server) fetchAllBuilderStatus() []BuilderStatusInfo {
	remoteBuilders := s.config.RemoteBuilders
	if len(remoteBuilders) == 0 {
		return nil
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		builders []BuilderStatusInfo
	)

	client := &http.Client{Timeout: 5 * time.Second}

	for _, addr := range remoteBuilders {
		wg.Add(1)
		go func(address string) {
			defer wg.Done()

			url := fmt.Sprintf("http://%s/api/v1/status", address)
			resp, err := client.Get(url)
			if err != nil {
				log.Printf("Failed to query builder %s: %v", address, err)
				// Add offline entry for unreachable builder
				mu.Lock()
				builders = append(builders, BuilderStatusInfo{
					ID:       address,
					Endpoint: fmt.Sprintf("http://%s", address),
					Status:   "offline",
					Enabled:  false,
				})
				mu.Unlock()
				return
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				log.Printf("Builder %s returned status %d", address, resp.StatusCode)
				mu.Lock()
				builders = append(builders, BuilderStatusInfo{
					ID:       address,
					Endpoint: fmt.Sprintf("http://%s", address),
					Status:   "error",
					Enabled:  false,
				})
				mu.Unlock()
				return
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Printf("Failed to read response from builder %s: %v", address, err)
				return
			}

			var status map[string]interface{}
			if err := json.Unmarshal(body, &status); err != nil {
				log.Printf("Failed to parse response from builder %s: %v", address, err)
				return
			}

			info := BuilderStatusInfo{
				ID:            getStringValue(status, "instance_id", address),
				Endpoint:      fmt.Sprintf("http://%s", address),
				Architecture:  getStringValue(status, "architecture", "unknown"),
				Status:        getStringValue(status, "status", "online"),
				Capacity:      getIntValue(status, "capacity", 0),
				CurrentLoad:   getIntValue(status, "current_load", 0),
				Enabled:       getBoolValue(status, "enabled", true),
				CPUUsage:      getFloatValue(status, "cpu_usage", 0),
				MemoryUsage:   getFloatValue(status, "memory_usage", 0),
				DiskUsage:     getFloatValue(status, "disk_usage", 0),
				TotalBuilds:   getIntValue(status, "total_builds", 0),
				SuccessBuilds: getIntValue(status, "success_builds", 0),
				FailedBuilds:  getIntValue(status, "failed_builds", 0),
			}

			mu.Lock()
			builders = append(builders, info)
			mu.Unlock()
		}(addr)
	}

	wg.Wait()
	return builders
}

// calculateBuilderStats calculates aggregate statistics from builder list.
func calculateBuilderStats(builders []BuilderStatusInfo) map[string]interface{} {
	totalBuilders := len(builders)
	onlineBuilders := 0
	offlineBuilders := 0
	totalCapacity := 0
	totalLoad := 0
	totalBuilds := 0
	totalSuccess := 0
	totalFailed := 0

	for _, b := range builders {
		if b.Status == "online" || b.Status == "busy" {
			onlineBuilders++
		} else {
			offlineBuilders++
		}
		totalCapacity += b.Capacity
		totalLoad += b.CurrentLoad
		totalBuilds += b.TotalBuilds
		totalSuccess += b.SuccessBuilds
		totalFailed += b.FailedBuilds
	}

	successRate := 0.0
	if totalBuilds > 0 {
		successRate = float64(totalSuccess) / float64(totalBuilds) * 100
	}

	return map[string]interface{}{
		"total_builders":   totalBuilders,
		"online_builders":  onlineBuilders,
		"offline_builders": offlineBuilders,
		"total_capacity":   totalCapacity,
		"total_load":       totalLoad,
		"total_builds":     totalBuilds,
		"success_builds":   totalSuccess,
		"failed_builds":    totalFailed,
		"success_rate":     successRate,
	}
}

// Helper functions for type conversion from map[string]interface{}

func getStringValue(m map[string]interface{}, key, defaultVal string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

func getIntValue(m map[string]interface{}, key string, defaultVal int) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return defaultVal
}

func getFloatValue(m map[string]interface{}, key string, defaultVal float64) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case int64:
			return float64(n)
		}
	}
	return defaultVal
}

func getBoolValue(m map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
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

// handleGPGPublicKey serves the GPG public key for builders.
func (s *Server) handleGPGPublicKey(w http.ResponseWriter, r *http.Request) {
	s.metrics.IncHTTPRequests()

	if r.Method != http.MethodGet {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if GPG is enabled
	if !s.config.GPGEnabled {
		http.Error(w, "GPG not enabled on server", http.StatusNotFound)
		return
	}

	// Check if key file exists
	if s.config.GPGKeyPath == "" {
		http.Error(w, "GPG key path not configured", http.StatusInternalServerError)
		return
	}

	// Read and serve the public key file
	http.ServeFile(w, r, s.config.GPGKeyPath)
}

// loggingMiddleware provides request logging.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.Method, r.RequestURI, r.RemoteAddr)
		s.metrics.UpdateGoroutines(int64(runtime.NumGoroutine()))
		next.ServeHTTP(w, r)
	})
}
