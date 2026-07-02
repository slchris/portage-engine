package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/slchris/portage-engine/internal/binpkg"
	"github.com/slchris/portage-engine/internal/builder"
)

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

	// Reject requests with no package rather than creating an empty queued job.
	if strings.TrimSpace(req.PackageName) == "" {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, "package_name (or category+package) is required", http.StatusBadRequest)
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

	// Translate to a Manager BuildRequest carrying the full bundle, which is
	// forwarded verbatim to a remote builder so the exact configuration is used.
	buildReq := &builder.BuildRequest{
		PackageName:  req.PackageName,
		Version:      req.Version,
		Arch:         req.Arch,
		ConfigBundle: req.ConfigBundle,
	}
	if buildReq.PackageName == "" && len(req.ConfigBundle.Packages.Packages) > 0 {
		buildReq.PackageName = req.ConfigBundle.Packages.Packages[0].Atom
	}

	s.metrics.IncBuildsTotal()
	jobID, err := s.builder.SubmitBuild(buildReq)
	if err != nil {
		s.metrics.IncHTTPRequestErrors()
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(builder.BuildResponse{JobID: jobID, Status: "queued"})
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
