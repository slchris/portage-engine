// Package dashboard implements the web dashboard for monitoring build cluster.
package dashboard

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/slchris/portage-engine/pkg/config"
)

// Dashboard represents the web dashboard.
type Dashboard struct {
	config     *config.DashboardConfig
	templates  *template.Template
	httpClient *http.Client
}

// ClusterStatus represents the overall cluster status.
type ClusterStatus struct {
	ActiveBuilds    int       `json:"active_builds"`
	QueuedBuilds    int       `json:"queued_builds"`
	ActiveInstances int       `json:"active_instances"`
	TotalBuilds     int       `json:"total_builds"`
	SuccessRate     float64   `json:"success_rate"`
	LastUpdated     time.Time `json:"last_updated"`
}

// New creates a new Dashboard instance.
func New(cfg *config.DashboardConfig) *Dashboard {
	tmpl := template.Must(template.New("dashboard").Parse(dashboardHTML))
	template.Must(tmpl.New("build-detail").Parse(buildDetailHTML))
	template.Must(tmpl.New("logs").Parse(logsHTML))
	template.Must(tmpl.New("monitor").Parse(monitorHTML))

	return &Dashboard{
		config:     cfg,
		templates:  tmpl,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Router returns the HTTP router for the dashboard.
func (d *Dashboard) Router() http.Handler {
	mux := http.NewServeMux()

	// Web interface
	mux.HandleFunc("/", d.handleIndex)
	mux.HandleFunc("/login", d.handleLogin)
	mux.HandleFunc("/builds", d.handleBuildsPage)
	mux.HandleFunc("/build/", d.handleBuildDetail)
	mux.HandleFunc("/logs/", d.handleBuildLogs)
	mux.HandleFunc("/monitor", d.handleBuildersMonitor)

	// API endpoints
	mux.HandleFunc("/api/status", d.handleStatus)
	mux.HandleFunc("/api/builds", d.handleBuilds)
	mux.HandleFunc("/api/builds/detail", d.handleBuildDetailAPI)
	mux.HandleFunc("/api/builds/logs", d.handleBuildLogsAPI)
	mux.HandleFunc("/api/instances", d.handleInstances)
	mux.HandleFunc("/api/scheduler/status", d.handleSchedulerStatus)
	mux.HandleFunc("/api/builders/status", d.handleBuildersStatusAPI)

	// Static files
	mux.HandleFunc("/static/", d.handleStatic)

	// Apply middleware
	var handler http.Handler = mux
	if d.config.AuthEnabled {
		handler = d.authMiddleware(handler)
	}
	handler = d.loggingMiddleware(handler)

	return handler
}

// handleIndex serves the main dashboard page.
func (d *Dashboard) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data := map[string]interface{}{
		"Title":     "Portage Engine Dashboard",
		"ServerURL": d.config.ServerURL,
		"Anonymous": d.config.AllowAnonymous,
	}

	if err := d.templates.Execute(w, data); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		log.Printf("Template error: %v", err)
	}
}

// handleLogin handles user authentication.
func (d *Dashboard) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// In production, this would validate credentials and issue JWT
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"token": "sample-jwt-token",
		"user":  "admin",
	})
}

// handleStatus returns the cluster status.
func (d *Dashboard) handleStatus(w http.ResponseWriter, _ *http.Request) {
	// Query the server for current status
	status, err := d.fetchClusterStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

// handleBuilds returns the list of builds from the server.
func (d *Dashboard) handleBuilds(w http.ResponseWriter, r *http.Request) {
	// Get limit parameter (default 50, max 200)
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
			if limit > 200 {
				limit = 200
			}
		}
	}

	// Query the server for build list
	url := fmt.Sprintf("%s/api/v1/builds/list?limit=%d", d.config.ServerURL, limit)
	resp, err := d.httpClient.Get(url)
	if err != nil {
		log.Printf("Failed to query builds: %v", err)
		// Return sample data on error
		builds := []map[string]interface{}{
			{
				"job_id":       "sample-job-1",
				"package_name": "gcc",
				"version":      "13.2.0",
				"arch":         "x86_64",
				"status":       "building",
				"created_at":   time.Now().Add(-10 * time.Minute).Format(time.RFC3339),
			},
			{
				"job_id":       "sample-job-2",
				"package_name": "python",
				"version":      "3.11.5",
				"arch":         "x86_64",
				"status":       "queued",
				"created_at":   time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(builds)
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Forward the response from server
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
}

// handleInstances returns the list of active instances.
func (d *Dashboard) handleInstances(w http.ResponseWriter, _ *http.Request) {
	// In production, this would query the server for instance list
	instances := []map[string]interface{}{
		{
			"id":         "gcp-12345678",
			"provider":   "gcp",
			"status":     "running",
			"ip_address": "10.0.1.100",
			"arch":       "amd64",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(instances)
}

// handleBuildsPage serves the builds list page.
func (d *Dashboard) handleBuildsPage(w http.ResponseWriter, _ *http.Request) {
	data := map[string]interface{}{
		"Title":     "Build Jobs",
		"ServerURL": d.config.ServerURL,
	}
	if err := d.templates.ExecuteTemplate(w, "builds", data); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}

// handleBuildDetail serves the build detail page.
func (d *Dashboard) handleBuildDetail(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimPrefix(r.URL.Path, "/build/")
	data := map[string]interface{}{
		"Title":     "Build Details",
		"JobID":     jobID,
		"ServerURL": d.config.ServerURL,
	}
	if err := d.templates.ExecuteTemplate(w, "build-detail", data); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}

// handleBuildLogs serves the real-time build logs page.
func (d *Dashboard) handleBuildLogs(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimPrefix(r.URL.Path, "/logs/")
	data := map[string]interface{}{
		"Title":     "Build Logs",
		"JobID":     jobID,
		"ServerURL": d.config.ServerURL,
	}
	if err := d.templates.ExecuteTemplate(w, "logs", data); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}

// handleBuildDetailAPI returns detailed information about a specific build.
func (d *Dashboard) handleBuildDetailAPI(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		http.Error(w, "job_id required", http.StatusBadRequest)
		return
	}

	url := fmt.Sprintf("%s/api/v1/builds/status?job_id=%s", d.config.ServerURL, jobID)
	resp, err := d.httpClient.Get(url)
	if err != nil {
		log.Printf("Failed to query build detail: %v", err)
		sampleDetail := map[string]interface{}{
			"job_id":       jobID,
			"package_name": "dev-lang/python",
			"version":      "3.11.5",
			"status":       "building",
			"builder_node": "builder-node-1",
			"builder_url":  "http://builder-node-1:9090",
			"created_at":   time.Now().Add(-15 * time.Minute).Format(time.RFC3339),
			"started_at":   time.Now().Add(-10 * time.Minute).Format(time.RFC3339),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sampleDetail)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
}

// handleBuildLogsAPI returns build logs.
func (d *Dashboard) handleBuildLogsAPI(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		http.Error(w, "job_id required", http.StatusBadRequest)
		return
	}

	url := fmt.Sprintf("%s/api/v1/builds/logs?job_id=%s", d.config.ServerURL, jobID)
	resp, err := d.httpClient.Get(url)
	if err != nil {
		log.Printf("Failed to query build logs: %v", err)
		sampleLogs := map[string]interface{}{
			"job_id": jobID,
			"logs":   "Compiling...\nBuilding package...\n",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sampleLogs)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
}

// handleBuildersMonitor serves the builders status monitor page.
func (d *Dashboard) handleBuildersMonitor(w http.ResponseWriter, _ *http.Request) {
	data := map[string]interface{}{
		"Title":     "Builders Monitor",
		"ServerURL": d.config.ServerURL,
	}
	if err := d.templates.ExecuteTemplate(w, "monitor", data); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}

// handleBuildersStatusAPI returns builders status from the server.
func (d *Dashboard) handleBuildersStatusAPI(w http.ResponseWriter, _ *http.Request) {
	url := fmt.Sprintf("%s/api/v1/builders/status", d.config.ServerURL)
	resp, err := d.httpClient.Get(url)
	if err != nil {
		log.Printf("Failed to query builders status: %v", err)
		// Return sample data on error
		sampleData := map[string]interface{}{
			"stats": map[string]interface{}{
				"total_builders":   3,
				"online_builders":  2,
				"offline_builders": 1,
				"total_capacity":   10,
				"total_load":       5,
				"success_rate":     92.5,
			},
			"builders": []map[string]interface{}{
				{
					"id":             "builder-1",
					"endpoint":       "http://localhost:9090",
					"architecture":   "amd64",
					"status":         "online",
					"capacity":       4,
					"current_load":   2,
					"enabled":        true,
					"cpu_usage":      45.2,
					"memory_usage":   62.8,
					"disk_usage":     55.3,
					"total_builds":   150,
					"success_builds": 142,
					"failed_builds":  8,
				},
				{
					"id":             "builder-2",
					"endpoint":       "http://localhost:9091",
					"architecture":   "arm64",
					"status":         "online",
					"capacity":       2,
					"current_load":   1,
					"enabled":        true,
					"cpu_usage":      30.5,
					"memory_usage":   48.2,
					"disk_usage":     42.7,
					"total_builds":   85,
					"success_builds": 80,
					"failed_builds":  5,
				},
				{
					"id":             "builder-3",
					"endpoint":       "http://localhost:9092",
					"architecture":   "amd64",
					"status":         "offline",
					"capacity":       4,
					"current_load":   0,
					"enabled":        false,
					"cpu_usage":      0.0,
					"memory_usage":   0.0,
					"disk_usage":     0.0,
					"total_builds":   200,
					"success_builds": 185,
					"failed_builds":  15,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sampleData)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
}

// handleSchedulerStatus returns scheduler and task assignment status.
func (d *Dashboard) handleSchedulerStatus(w http.ResponseWriter, _ *http.Request) {
	url := fmt.Sprintf("%s/api/v1/scheduler/status", d.config.ServerURL)
	resp, err := d.httpClient.Get(url)
	if err != nil {
		log.Printf("Failed to query scheduler status: %v", err)
		sampleStatus := map[string]interface{}{
			"builders": []map[string]interface{}{
				{
					"id":           "builder-1",
					"url":          "http://localhost:9090",
					"capacity":     4,
					"current_load": 2,
					"enabled":      true,
					"healthy":      true,
				},
			},
			"queued_tasks":  5,
			"running_tasks": 2,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sampleStatus)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
}

// handleStatic serves static files.
func (d *Dashboard) handleStatic(w http.ResponseWriter, r *http.Request) {
	// Define the static files root directory
	staticRoot := "./static"

	// Extract the requested path and remove the /static/ prefix
	requestPath := strings.TrimPrefix(r.URL.Path, "/static/")

	// Clean the path to prevent directory traversal attacks
	requestPath = filepath.Clean(requestPath)

	// Prevent accessing files outside the static directory
	// Check for any attempt to traverse up (..)
	if strings.Contains(requestPath, "..") || strings.HasPrefix(requestPath, "/") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		log.Printf("Blocked path traversal attempt: %s", r.URL.Path)
		return
	}

	// Construct the full file path
	fullPath := filepath.Join(staticRoot, requestPath)

	// Verify the resolved path is still within staticRoot
	absStaticRoot, err := filepath.Abs(staticRoot)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Failed to resolve static root: %v", err)
		return
	}

	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Failed to resolve file path: %v", err)
		return
	}

	// Ensure the file is within the static directory
	if !strings.HasPrefix(absFullPath, absStaticRoot) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		log.Printf("Blocked access outside static directory: %s -> %s", r.URL.Path, absFullPath)
		return
	}

	// Serve the file
	http.ServeFile(w, r, fullPath)
}

// fetchClusterStatus fetches cluster status from the server.
func (d *Dashboard) fetchClusterStatus() (*ClusterStatus, error) {
	resp, err := d.httpClient.Get(fmt.Sprintf("%s/api/v1/cluster/status", d.config.ServerURL))
	if err != nil {
		// Return sample data on error
		log.Printf("Failed to fetch cluster status: %v", err)
		return &ClusterStatus{
			ActiveBuilds:    2,
			QueuedBuilds:    5,
			ActiveInstances: 3,
			TotalBuilds:     127,
			SuccessRate:     94.5,
			LastUpdated:     time.Now(),
		}, nil
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var status ClusterStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}

	return &status, nil
}

// authMiddleware handles authentication.
func (d *Dashboard) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for login endpoint
		if r.URL.Path == "/login" {
			next.ServeHTTP(w, r)
			return
		}

		// Allow anonymous access if enabled
		if d.config.AllowAnonymous && r.Header.Get("Authorization") == "" {
			next.ServeHTTP(w, r)
			return
		}

		// In production, validate JWT token
		token := r.Header.Get("Authorization")
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware provides request logging.
func (d *Dashboard) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.Method, r.RequestURI, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

// dashboardHTML is a lightweight HTML page for the dashboard.
const dashboardHTML = `<!DOCTYPE html>
<html>
<head>
    <title>{{.Title}}</title>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <style>
        body {
            font-family: monospace;
            background: #f5f5f5;
            margin: 0;
            padding: 10px;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
            background: white;
            border: 1px solid #ccc;
        }
        header {
            background: #333;
            color: white;
            padding: 10px;
            border-bottom: 1px solid #000;
        }
        h1 {
            font-size: 18px;
            margin: 0;
        }
        .subtitle {
            font-size: 12px;
            color: #ccc;
        }
        .nav-links {
            margin-top: 8px;
        }
        .nav-links a {
            color: #aaa;
            text-decoration: none;
            margin-right: 15px;
            font-size: 12px;
        }
        .nav-links a:hover {
            color: white;
            text-decoration: underline;
        }
        .stats {
            display: table;
            width: 100%;
            border-collapse: collapse;
        }
        .stat-row {
            display: table-row;
        }
        .stat-cell {
            display: table-cell;
            padding: 8px;
            border: 1px solid #ddd;
        }
        .stat-label {
            font-weight: bold;
        }
        .stat-value {
            text-align: right;
        }
        .filters {
            padding: 8px;
            border-bottom: 1px solid #ddd;
        }
        .filter-btn {
            background: white;
            border: 1px solid #999;
            padding: 4px 8px;
            margin-right: 5px;
            cursor: pointer;
        }
        .filter-btn.active {
            background: #333;
            color: white;
        }
        .auto-refresh-toggle {
            float: right;
            display: inline-flex;
            align-items: center;
            gap: 8px;
        }
        .auto-refresh-toggle label {
            cursor: pointer;
            user-select: none;
        }
        .auto-refresh-toggle input[type="checkbox"] {
            cursor: pointer;
        }
        .refresh-btn {
            background: #0066cc;
            color: white;
            border: none;
            padding: 4px 12px;
            cursor: pointer;
            border-radius: 3px;
        }
        .refresh-btn:hover {
            background: #0052a3;
        }
        .refresh-btn:disabled {
            background: #999;
            cursor: not-allowed;
        }
        .builds-table {
            width: 100%;
            border-collapse: collapse;
            font-size: 12px;
        }
        .builds-table th {
            background: #333;
            color: white;
            padding: 6px;
            text-align: left;
            border: 1px solid #000;
        }
        .builds-table td {
            padding: 6px;
            border: 1px solid #ddd;
        }
        .builds-table tr:nth-child(even) {
            background: #f9f9f9;
        }
        .status-building { color: #ff6600; }
        .status-completed { color: #00aa00; }
        .status-failed { color: #cc0000; }
        .status-queued { color: #0066cc; }
        .refresh-info {
            float: right;
            color: #666;
            font-size: 11px;
            line-height: 24px;
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>{{.Title}}</h1>
            <p class="subtitle">Portage Build Cluster Monitor</p>
            <div class="nav-links">
                <a href="/">Dashboard</a>
                <a href="/monitor">Builders Monitor</a>
            </div>
        </header>

        <div class="stats">
            <div class="stat-row">
                <div class="stat-cell stat-label">Active Builds:</div>
                <div class="stat-cell stat-value" id="active-builds">-</div>
                <div class="stat-cell stat-label">Queued:</div>
                <div class="stat-cell stat-value" id="queued-builds">-</div>
                <div class="stat-cell stat-label">Instances:</div>
                <div class="stat-cell stat-value" id="active-instances">-</div>
                <div class="stat-cell stat-label">Success Rate:</div>
                <div class="stat-cell stat-value" id="success-rate">-</div>
            </div>
        </div>

        <div class="filters">
            <button class="filter-btn active" onclick="filterBuilds('all')">All</button>
            <button class="filter-btn" onclick="filterBuilds('building')">Building</button>
            <button class="filter-btn" onclick="filterBuilds('queued')">Queued</button>
            <button class="filter-btn" onclick="filterBuilds('completed')">Completed</button>
            <button class="filter-btn" onclick="filterBuilds('failed')">Failed</button>
            <div class="auto-refresh-toggle">
                <button class="refresh-btn" id="refresh-btn" onclick="manualRefresh()">Refresh</button>
                <label>
                    <input type="checkbox" id="auto-refresh-toggle" checked>
                    Auto-refresh
                </label>
                <span id="refresh-info">5s</span>
            </div>
        </div>

        <table class="builds-table" id="builds-table">
            <thead>
                <tr>
                    <th>Package</th>
                    <th>Version</th>
                    <th>Arch</th>
                    <th>Status</th>
                    <th>Node</th>
                    <th>Job ID</th>
                    <th>Created</th>
                </tr>
            </thead>
            <tbody id="builds-tbody">
                <tr><td colspan="7">Loading...</td></tr>
            </tbody>
        </table>
    </div>

    <script>
        let currentFilter = 'all';
        let allBuilds = [];
        let autoRefreshEnabled = true;
        let refreshIntervalId = null;
        let sortField = 'created_at';
        let sortDescending = true;

        // Load auto-refresh preference from localStorage
        const savedAutoRefresh = localStorage.getItem('dashboard_auto_refresh');
        if (savedAutoRefresh !== null) {
            autoRefreshEnabled = savedAutoRefresh === 'true';
            document.addEventListener('DOMContentLoaded', function() {
                document.getElementById('auto-refresh-toggle').checked = autoRefreshEnabled;
                updateRefreshInfo();
            });
        }

        function toggleAutoRefresh() {
            autoRefreshEnabled = document.getElementById('auto-refresh-toggle').checked;
            localStorage.setItem('dashboard_auto_refresh', autoRefreshEnabled);
            updateRefreshInfo();
            if (autoRefreshEnabled) {
                startAutoRefresh();
            } else {
                stopAutoRefresh();
            }
        }

        function updateRefreshInfo() {
            const info = document.getElementById('refresh-info');
            info.textContent = autoRefreshEnabled ? '5s' : 'off';
            info.style.color = autoRefreshEnabled ? '#666' : '#999';
        }

        function startAutoRefresh() {
            if (refreshIntervalId) {
                clearInterval(refreshIntervalId);
            }
            refreshIntervalId = setInterval(() => {
                updateStatus();
                updateBuilds();
            }, 5000);
        }

        function stopAutoRefresh() {
            if (refreshIntervalId) {
                clearInterval(refreshIntervalId);
                refreshIntervalId = null;
            }
        }

        function manualRefresh() {
            const btn = document.getElementById('refresh-btn');
            btn.disabled = true;
            btn.textContent = 'Loading...';
            Promise.all([updateStatus(), updateBuilds()]).finally(() => {
                btn.disabled = false;
                btn.textContent = 'Refresh';
            });
        }

        document.getElementById('auto-refresh-toggle').addEventListener('change', toggleAutoRefresh);

        function filterBuilds(filter) {
            currentFilter = filter;
            document.querySelectorAll('.filter-btn').forEach(btn => {
                btn.classList.remove('active');
            });
            event.target.classList.add('active');
            renderBuilds();
        }

        function sortBuilds(builds) {
            return builds.slice().sort((a, b) => {
                let aVal = a[sortField] || '';
                let bVal = b[sortField] || '';
                if (sortField === 'created_at') {
                    aVal = new Date(aVal).getTime() || 0;
                    bVal = new Date(bVal).getTime() || 0;
                }
                if (sortDescending) {
                    return bVal > aVal ? 1 : bVal < aVal ? -1 : 0;
                }
                return aVal > bVal ? 1 : aVal < bVal ? -1 : 0;
            });
        }

        function renderBuilds() {
            const tbody = document.getElementById('builds-tbody');
            let filteredBuilds = allBuilds;
            if (currentFilter !== 'all') {
                filteredBuilds = allBuilds.filter(b => b.status === currentFilter);
            }

            // Sort builds to maintain stable order
            filteredBuilds = sortBuilds(filteredBuilds);

            if (filteredBuilds.length === 0) {
                tbody.innerHTML = '<tr><td colspan="7">No builds found</td></tr>';
                return;
            }

            tbody.innerHTML = filteredBuilds.map(build => {
                const createdDate = new Date(build.created_at);
                const timeStr = createdDate.toLocaleString();
                const shortId = build.job_id.substring(0, 8);
                const nodeInfo = build.instance_id ? build.instance_id.split(':')[0] : '';

                return '<tr onclick="window.location.href=\'/build/' + build.job_id + '\'">' +
                    '<td>' + (build.package_name || 'N/A') + '</td>' +
                    '<td>' + (build.version || '-') + '</td>' +
                    '<td>' + (build.arch || '-') + '</td>' +
                    '<td class="status-' + build.status + '">' + build.status + '</td>' +
                    '<td>' + (nodeInfo || '-') + '</td>' +
                    '<td>' + shortId + '</td>' +
                    '<td>' + timeStr + '</td>' +
                    '</tr>';
            }).join('');
        }

        function updateStatus() {
            return fetch('/api/status')
                .then(r => r.json())
                .then(data => {
                    document.getElementById('active-builds').textContent = data.active_builds;
                    document.getElementById('queued-builds').textContent = data.queued_builds;
                    document.getElementById('active-instances').textContent = data.active_instances;
                    document.getElementById('success-rate').textContent = data.success_rate.toFixed(1) + '%';
                })
                .catch(err => console.error('Status fetch failed:', err));
        }

        function updateBuilds() {
            return fetch('/api/builds?limit=50')
                .then(r => r.json())
                .then(data => {
                    allBuilds = data || [];
                    renderBuilds();
                })
                .catch(err => console.error('Builds fetch failed:', err));
        }

        updateStatus();
        updateBuilds();
        updateRefreshInfo();
        if (autoRefreshEnabled) {
            startAutoRefresh();
        }
    </script>
</body>
</html>`

// buildDetailHTML is the HTML page for build details.
const buildDetailHTML = `<!DOCTYPE html>
<html>
<head>
    <title>{{.Title}} - Portage Engine</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f5f5f5; padding: 20px; }
        .container { max-width: 1200px; margin: 0 auto; }
        .header { background: white; padding: 20px; border-radius: 8px; margin-bottom: 20px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .header h1 { color: #333; margin-bottom: 10px; }
        .header .nav { margin-top: 15px; }
        .header .nav a { color: #0066cc; text-decoration: none; margin-right: 20px; }
        .header .nav a:hover { text-decoration: underline; }
        .info-card { background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); margin-bottom: 20px; }
        .info-card h2 { color: #333; margin-bottom: 15px; font-size: 18px; }
        .info-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 15px; }
        .info-item { padding: 10px; background: #f9f9f9; border-radius: 4px; }
        .info-item .label { font-weight: bold; color: #666; font-size: 12px; margin-bottom: 5px; }
        .info-item .value { color: #333; font-size: 14px; word-break: break-all; }
        .status-badge { display: inline-block; padding: 4px 12px; border-radius: 12px; font-size: 12px; font-weight: bold; }
        .status-queued { background: #fff3cd; color: #856404; }
        .status-building { background: #cce5ff; color: #004085; }
        .status-completed { background: #d4edda; color: #155724; }
        .status-failed { background: #f8d7da; color: #721c24; }
        .btn { display: inline-block; padding: 10px 20px; background: #0066cc; color: white; text-decoration: none; border-radius: 4px; margin-top: 15px; }
        .btn:hover { background: #0052a3; }
        .logs-preview { background: #1e1e1e; color: #d4d4d4; padding: 15px; border-radius: 4px; font-family: 'Courier New', monospace; font-size: 13px; max-height: 300px; overflow-y: auto; white-space: pre-wrap; word-wrap: break-word; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Build Details</h1>
            <div class="nav">
                <a href="/">← Back to Dashboard</a>
                <a href="/logs/{{.JobID}}">View Full Logs</a>
            </div>
        </div>

        <div class="info-card">
            <h2>Build Information</h2>
            <div id="build-info" class="info-grid">
                <div class="info-item"><div class="label">Job ID</div><div class="value">Loading...</div></div>
            </div>
        </div>

        <div class="info-card">
            <h2>Build Logs Preview</h2>
            <div id="logs-preview" class="logs-preview">Loading logs...</div>
            <a href="/logs/{{.JobID}}" class="btn">View Full Logs</a>
        </div>
    </div>

    <script>
        const jobID = "{{.JobID}}";
        const serverURL = "{{.ServerURL}}";

        async function loadBuildDetail() {
            try {
                const resp = await fetch(serverURL + '/api/v1/builds/status?job_id=' + jobID);
                const build = await resp.json();

                const statusClass = 'status-' + build.status;
                document.getElementById('build-info').innerHTML = ` + "`" + `
                    <div class="info-item"><div class="label">Job ID</div><div class="value">${build.job_id}</div></div>
                    <div class="info-item"><div class="label">Package</div><div class="value">${build.package_name}</div></div>
                    <div class="info-item"><div class="label">Version</div><div class="value">${build.version}</div></div>
                    <div class="info-item"><div class="label">Architecture</div><div class="value">${build.arch}</div></div>
                    <div class="info-item"><div class="label">Status</div><div class="value"><span class="status-badge ${statusClass}">${build.status}</span></div></div>
                    <div class="info-item"><div class="label">Builder Node</div><div class="value">${build.instance_id || 'Not assigned'}</div></div>
                    <div class="info-item"><div class="label">Created</div><div class="value">${new Date(build.created_at).toLocaleString()}</div></div>
                    <div class="info-item"><div class="label">Updated</div><div class="value">${new Date(build.updated_at).toLocaleString()}</div></div>
                ` + "`" + `;
            } catch (err) {
                console.error('Failed to load build detail:', err);
            }
        }

        async function loadLogsPreview() {
            try {
                const resp = await fetch(serverURL + '/api/v1/builds/logs?job_id=' + jobID);
                const data = await resp.json();
                const lines = data.logs.split('\n').slice(0, 20).join('\n');
                document.getElementById('logs-preview').textContent = lines + '\n\n... (showing first 20 lines)';
            } catch (err) {
                console.error('Failed to load logs:', err);
            }
        }

        loadBuildDetail();
        loadLogsPreview();
        setInterval(() => {
            loadBuildDetail();
            loadLogsPreview();
        }, 5000);
    </script>
</body>
</html>`

// logsHTML is the HTML page for real-time build logs.
const logsHTML = `<!DOCTYPE html>
<html>
<head>
    <title>{{.Title}} - Portage Engine</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f5f5f5; padding: 20px; }
        .container { max-width: 1400px; margin: 0 auto; }
        .header { background: white; padding: 20px; border-radius: 8px; margin-bottom: 20px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .header h1 { color: #333; margin-bottom: 10px; }
        .header .info { color: #666; font-size: 14px; margin-bottom: 10px; }
        .header .nav { margin-top: 15px; }
        .header .nav a { color: #0066cc; text-decoration: none; margin-right: 20px; }
        .header .nav a:hover { text-decoration: underline; }
        .logs-container { background: #1e1e1e; color: #d4d4d4; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); font-family: 'Courier New', monospace; font-size: 13px; min-height: 600px; max-height: 800px; overflow-y: auto; white-space: pre-wrap; word-wrap: break-word; }
        .controls { background: white; padding: 15px; border-radius: 8px; margin-bottom: 20px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .controls label { margin-right: 10px; }
        .controls input[type="checkbox"] { margin-right: 5px; }
        .status-indicator { display: inline-block; width: 10px; height: 10px; border-radius: 50%; margin-right: 8px; }
        .status-live { background: #28a745; animation: pulse 1.5s infinite; }
        @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.5; } }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Build Logs</h1>
            <div class="info">
                Job ID: <strong>{{.JobID}}</strong>
                <span id="status-indicator"></span>
            </div>
            <div class="nav">
                <a href="/">← Back to Dashboard</a>
                <a href="/build/{{.JobID}}">← Back to Build Details</a>
            </div>
        </div>

        <div class="controls">
            <label>
                <input type="checkbox" id="auto-scroll" checked>
                Auto-scroll to bottom
            </label>
            <label style="margin-left: 20px;">
                <input type="checkbox" id="live-update" checked>
                <span class="status-indicator status-live"></span>
                Live updates
            </label>
        </div>

        <div id="logs" class="logs-container">Loading logs...</div>
    </div>

    <script>
        const jobID = "{{.JobID}}";
        const serverURL = "{{.ServerURL}}";
        let autoScroll = true;
        let liveUpdate = true;

        document.getElementById('auto-scroll').addEventListener('change', (e) => {
            autoScroll = e.target.checked;
        });

        document.getElementById('live-update').addEventListener('change', (e) => {
            liveUpdate = e.target.checked;
        });

        async function loadLogs() {
            if (!liveUpdate) return;

            try {
                const resp = await fetch(serverURL + '/api/v1/builds/logs?job_id=' + jobID);
                const data = await resp.json();
                const logsDiv = document.getElementById('logs');
                logsDiv.textContent = data.logs;

                if (autoScroll) {
                    logsDiv.scrollTop = logsDiv.scrollHeight;
                }

                // Update status indicator
                const statusResp = await fetch(serverURL + '/api/v1/builds/status?job_id=' + jobID);
                const build = await statusResp.json();
                document.getElementById('status-indicator').innerHTML =
                    ` + "`Status: <strong>${build.status}</strong>`" + `;
            } catch (err) {
                console.error('Failed to load logs:', err);
            }
        }

        loadLogs();
        setInterval(loadLogs, 2000);
    </script>
</body>
</html>`

// monitorHTML is the HTML page for builders status monitor (similar to OpenBuildService).
const monitorHTML = `<!DOCTYPE html>
<html>
<head>
    <title>{{.Title}} - Portage Engine</title>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: "Courier New", monospace;
            background: #eee;
            padding: 10px;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
            background: white;
            border: 1px solid #ccc;
        }
        header {
            background: #333;
            color: white;
            padding: 10px;
            border-bottom: 1px solid #000;
        }
        h1 {
            font-size: 18px;
            margin: 0;
        }
        .subtitle {
            font-size: 12px;
            color: #ccc;
        }
        .nav-links {
            margin-top: 8px;
        }
        .nav-links a {
            color: #aaa;
            text-decoration: none;
            margin-right: 15px;
            font-size: 12px;
        }
        .nav-links a:hover {
            color: white;
            text-decoration: underline;
        }
        .stats {
            display: table;
            width: 100%;
            border-collapse: collapse;
        }
        .stat-row {
            display: table-row;
        }
        .stat-cell {
            display: table-cell;
            padding: 8px;
            border: 1px solid #ddd;
        }
        .stat-label {
            font-weight: bold;
        }
        .stat-value {
            text-align: right;
        }
        .section-header {
            background: #333;
            color: white;
            padding: 8px;
            font-weight: bold;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        .refresh-controls {
            display: flex;
            align-items: center;
            gap: 10px;
        }
        .refresh-btn {
            background: #0066cc;
            color: white;
            border: none;
            padding: 4px 12px;
            cursor: pointer;
            font-family: "Courier New", monospace;
            font-size: 12px;
        }
        .refresh-btn:hover {
            background: #0052a3;
        }
        .refresh-btn:disabled {
            background: #999;
            cursor: not-allowed;
        }
        .builders-table {
            width: 100%;
            border-collapse: collapse;
            font-size: 12px;
        }
        .builders-table th {
            background: #333;
            color: white;
            padding: 6px;
            text-align: left;
            border: 1px solid #000;
        }
        .builders-table td {
            padding: 6px;
            border: 1px solid #ddd;
        }
        .builders-table tr:nth-child(even) {
            background: #f9f9f9;
        }
        .status-online { color: #00aa00; font-weight: bold; }
        .status-offline { color: #cc0000; font-weight: bold; }
        .status-busy { color: #ff6600; font-weight: bold; }
        .progress-bar {
            width: 100%;
            height: 14px;
            background: #ddd;
            border: 1px solid #999;
            position: relative;
        }
        .progress-fill {
            height: 100%;
            background: #0066cc;
            transition: width 0.3s ease;
        }
        .progress-fill.high { background: #cc0000; }
        .progress-fill.warning { background: #ff6600; }
        .progress-text {
            position: absolute;
            width: 100%;
            text-align: center;
            font-size: 10px;
            line-height: 14px;
            color: #333;
        }
        .no-builders {
            text-align: center;
            padding: 20px;
            color: #666;
        }
        .build-counts {
            font-size: 11px;
        }
        .build-counts .success { color: #00aa00; }
        .build-counts .failed { color: #cc0000; }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>{{.Title}}</h1>
            <p class="subtitle">Builders Status Monitor</p>
            <div class="nav-links">
                <a href="/">Dashboard</a>
                <a href="/builds">Build Jobs</a>
                <a href="/monitor">Builders Monitor</a>
            </div>
        </header>

        <div class="stats">
            <div class="stat-row">
                <div class="stat-cell stat-label">Total Builders:</div>
                <div class="stat-cell stat-value" id="total-builders">-</div>
                <div class="stat-cell stat-label">Online:</div>
                <div class="stat-cell stat-value" id="online-builders">-</div>
                <div class="stat-cell stat-label">Capacity:</div>
                <div class="stat-cell stat-value" id="total-capacity">-</div>
                <div class="stat-cell stat-label">Success Rate:</div>
                <div class="stat-cell stat-value" id="success-rate">-</div>
            </div>
        </div>

        <div class="section-header">
            <span>Builders List</span>
            <div class="refresh-controls">
                <button class="refresh-btn" id="refresh-btn" onclick="manualRefresh()">Refresh</button>
                <label style="cursor:pointer;font-weight:normal;font-size:12px;">
                    <input type="checkbox" id="auto-refresh-toggle" checked style="cursor:pointer;">
                    Auto-refresh
                </label>
                <span id="refresh-info" style="font-size:11px;font-weight:normal;">5s</span>
            </div>
        </div>

        <table class="builders-table">
            <thead>
                <tr>
                    <th>Builder ID</th>
                    <th>Status</th>
                    <th>Architecture</th>
                    <th>Load</th>
                    <th>CPU</th>
                    <th>Memory</th>
                    <th>Disk</th>
                    <th>Builds (S/F/T)</th>
                    <th>Endpoint</th>
                </tr>
            </thead>
            <tbody id="builders-tbody">
                <tr><td colspan="9" class="no-builders">Loading builders...</td></tr>
            </tbody>
        </table>
    </div>

    <script>
        const serverURL = "{{.ServerURL}}";

        function getProgressBarClass(value) {
            if (value >= 80) return 'high';
            if (value >= 60) return 'warning';
            return '';
        }

        function formatPercentage(value) {
            return value ? value.toFixed(1) + '%' : '0%';
        }

        function renderProgressBar(value) {
            const pct = value || 0;
            const cls = getProgressBarClass(pct);
            return ` + "`" + `<div class="progress-bar">
                <div class="progress-fill ${cls}" style="width: ${pct}%"></div>
                <span class="progress-text">${formatPercentage(value)}</span>
            </div>` + "`" + `;
        }

        function renderBuilders(data) {
            const stats = data.stats || {};
            const builders = data.builders || [];

            // Update stats
            document.getElementById('total-builders').textContent = stats.total_builders || 0;
            document.getElementById('online-builders').textContent =
                (stats.online_builders || 0) + ' / ' + (stats.offline_builders || 0) + ' offline';
            document.getElementById('total-capacity').textContent =
                (stats.total_load || 0) + ' / ' + (stats.total_capacity || 0);
            document.getElementById('success-rate').textContent =
                formatPercentage(stats.success_rate) + ' (' + (stats.total_builds || 0) + ' builds)';

            // Render builders table
            const tbody = document.getElementById('builders-tbody');
            if (builders.length === 0) {
                tbody.innerHTML = '<tr><td colspan="9" class="no-builders">No builders registered</td></tr>';
                return;
            }

            tbody.innerHTML = builders.map(builder => {
                const statusClass = builder.status === 'offline' ? 'status-offline' :
                                   builder.status === 'busy' ? 'status-busy' : 'status-online';
                const loadPct = builder.capacity > 0 ?
                    (builder.current_load / builder.capacity * 100) : 0;

                return ` + "`" + `<tr>
                    <td><strong>${builder.id}</strong></td>
                    <td class="${statusClass}">${builder.status}${builder.enabled ? '' : ' (disabled)'}</td>
                    <td>${builder.architecture || 'unknown'}</td>
                    <td>${builder.current_load || 0} / ${builder.capacity || 0}</td>
                    <td>${renderProgressBar(builder.cpu_usage)}</td>
                    <td>${renderProgressBar(builder.memory_usage)}</td>
                    <td>${renderProgressBar(builder.disk_usage)}</td>
                    <td class="build-counts">
                        <span class="success">${builder.success_builds || 0}</span> /
                        <span class="failed">${builder.failed_builds || 0}</span> /
                        ${builder.total_builds || 0}
                    </td>
                    <td style="font-size:10px;">${builder.endpoint || 'N/A'}</td>
                </tr>` + "`" + `;
            }).join('');
        }

        async function updateBuildersStatus() {
            try {
                const resp = await fetch('/api/builders/status');
                const data = await resp.json();
                renderBuilders(data);
            } catch (err) {
                console.error('Failed to fetch builders status:', err);
            }
        }

        let autoRefreshEnabled = true;
        let refreshIntervalId = null;

        // Load auto-refresh preference from localStorage
        const savedAutoRefresh = localStorage.getItem('monitor_auto_refresh');
        if (savedAutoRefresh !== null) {
            autoRefreshEnabled = savedAutoRefresh === 'true';
            document.addEventListener('DOMContentLoaded', function() {
                document.getElementById('auto-refresh-toggle').checked = autoRefreshEnabled;
                updateRefreshInfo();
            });
        }

        function toggleAutoRefresh() {
            autoRefreshEnabled = document.getElementById('auto-refresh-toggle').checked;
            localStorage.setItem('monitor_auto_refresh', autoRefreshEnabled);
            updateRefreshInfo();
            if (autoRefreshEnabled) {
                startAutoRefresh();
            } else {
                stopAutoRefresh();
            }
        }

        function updateRefreshInfo() {
            const info = document.getElementById('refresh-info');
            info.textContent = autoRefreshEnabled ? '5s' : 'off';
            info.style.color = autoRefreshEnabled ? '#333' : '#999';
        }

        function startAutoRefresh() {
            if (refreshIntervalId) {
                clearInterval(refreshIntervalId);
            }
            refreshIntervalId = setInterval(updateBuildersStatus, 5000);
        }

        function stopAutoRefresh() {
            if (refreshIntervalId) {
                clearInterval(refreshIntervalId);
                refreshIntervalId = null;
            }
        }

        function manualRefresh() {
            const btn = document.getElementById('refresh-btn');
            btn.disabled = true;
            btn.textContent = 'Loading...';
            updateBuildersStatus().finally(() => {
                btn.disabled = false;
                btn.textContent = 'Refresh';
            });
        }

        document.getElementById('auto-refresh-toggle').addEventListener('change', toggleAutoRefresh);

        updateBuildersStatus();
        updateRefreshInfo();
        if (autoRefreshEnabled) {
            startAutoRefresh();
        }
    </script>
</body>
</html>`
