// Package dashboard implements the web dashboard for monitoring build cluster.
package dashboard

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
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

	// API endpoints
	mux.HandleFunc("/api/status", d.handleStatus)
	mux.HandleFunc("/api/builds", d.handleBuilds)
	mux.HandleFunc("/api/instances", d.handleInstances)

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
func (d *Dashboard) handleBuilds(w http.ResponseWriter, _ *http.Request) {
	// Query the server for build list
	resp, err := d.httpClient.Get(fmt.Sprintf("%s/api/v1/builds/list", d.config.ServerURL))
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

// handleStatic serves static files.
func (d *Dashboard) handleStatic(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, r.URL.Path[1:])
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
            padding: 8px;
            background: #ffffcc;
            border: 1px solid #999;
            font-size: 11px;
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>{{.Title}}</h1>
            <p class="subtitle">Portage Build Cluster Monitor</p>
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
            <span class="refresh-info" id="refresh-info">Auto-refresh: 5s</span>
        </div>

        <table class="builds-table" id="builds-table">
            <thead>
                <tr>
                    <th>Package</th>
                    <th>Version</th>
                    <th>Arch</th>
                    <th>Status</th>
                    <th>Job ID</th>
                    <th>Created</th>
                </tr>
            </thead>
            <tbody id="builds-tbody">
                <tr><td colspan="6">Loading...</td></tr>
            </tbody>
        </table>
    </div>

    <script>
        let currentFilter = 'all';
        let allBuilds = [];

        function filterBuilds(filter) {
            currentFilter = filter;
            document.querySelectorAll('.filter-btn').forEach(btn => {
                btn.classList.remove('active');
            });
            event.target.classList.add('active');
            renderBuilds();
        }

        function renderBuilds() {
            const tbody = document.getElementById('builds-tbody');
            let filteredBuilds = allBuilds;
            if (currentFilter !== 'all') {
                filteredBuilds = allBuilds.filter(b => b.status === currentFilter);
            }

            if (filteredBuilds.length === 0) {
                tbody.innerHTML = '<tr><td colspan="6">No builds found</td></tr>';
                return;
            }

            tbody.innerHTML = filteredBuilds.map(build => {
                const createdDate = new Date(build.created_at);
                const timeStr = createdDate.toLocaleString();
                const shortId = build.job_id.substring(0, 8);

                return '<tr>' +
                    '<td>' + (build.package_name || 'N/A') + '</td>' +
                    '<td>' + build.version + '</td>' +
                    '<td>' + build.arch + '</td>' +
                    '<td class="status-' + build.status + '">' + build.status + '</td>' +
                    '<td>' + shortId + '</td>' +
                    '<td>' + timeStr + '</td>' +
                    '</tr>';
            }).join('');
        }

        function updateStatus() {
            fetch('/api/status')
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
            fetch('/api/builds')
                .then(r => r.json())
                .then(data => {
                    allBuilds = data || [];
                    renderBuilds();
                })
                .catch(err => console.error('Builds fetch failed:', err));
        }

        updateStatus();
        updateBuilds();
        setInterval(() => {
            updateStatus();
            updateBuilds();
        }, 5000);
    </script>
</body>
</html>`
