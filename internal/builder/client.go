package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/slchris/portage-engine/internal/metrics"
)

// HeartbeatRequest represents a heartbeat request from builder to server.
type HeartbeatRequest struct {
	BuilderID  string    `json:"builder_id"`
	Status     string    `json:"status"`
	Endpoint   string    `json:"endpoint"`
	Capacity   int       `json:"capacity"`
	ActiveJobs int       `json:"active_jobs"`
	Timestamp  time.Time `json:"timestamp"`
}

// HeartbeatResponse represents the server's response to a heartbeat.
type HeartbeatResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// Client communicates with remote builder services
type Client struct {
	baseURL       string
	httpClient    *http.Client
	heartbeatStop chan struct{}
	heartbeatWg   sync.WaitGroup
	metrics       *metrics.Metrics
}

// NewBuilderClient creates a new builder client
func NewBuilderClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		heartbeatStop: make(chan struct{}),
		metrics:       metrics.New(&metrics.Config{Enabled: false}),
	}
}

// NewBuilderClientWithMetrics creates a new builder client with metrics enabled
func NewBuilderClientWithMetrics(baseURL string, m *metrics.Metrics) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		heartbeatStop: make(chan struct{}),
		metrics:       m,
	}
}

// SubmitBuild sends a build request to the remote builder
func (bc *Client) SubmitBuild(req *LocalBuildRequest) (string, error) {
	start := time.Now()
	bc.metrics.IncHTTPRequests()
	bc.metrics.IncBuildsTotal()

	data, err := json.Marshal(req)
	if err != nil {
		bc.metrics.IncHTTPRequestErrors()
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := bc.httpClient.Post(
		bc.baseURL+"/api/v1/build",
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		bc.metrics.IncHTTPRequestErrors()
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		bc.metrics.IncHTTPRequestErrors()
		bc.metrics.IncBuildsFailed()
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("build submission failed: %s", string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		bc.metrics.IncHTTPRequestErrors()
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	jobID, ok := result["job_id"].(string)
	if !ok {
		bc.metrics.IncHTTPRequestErrors()
		return "", fmt.Errorf("invalid response: missing job_id")
	}

	bc.metrics.RecordHTTPLatency("/api/v1/build", time.Since(start))
	return jobID, nil
}

// GetJobStatus retrieves the status of a build job
func (bc *Client) GetJobStatus(jobID string) (*BuildJob, error) {
	resp, err := bc.httpClient.Get(bc.baseURL + "/api/v1/jobs/" + jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get job status: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get job status failed: %s", string(body))
	}

	var job BuildJob
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &job, nil
}

// GetBuilderStatus retrieves the status of the builder service
func (bc *Client) GetBuilderStatus() (map[string]interface{}, error) {
	resp, err := bc.httpClient.Get(bc.baseURL + "/api/v1/status")
	if err != nil {
		return nil, fmt.Errorf("failed to get builder status: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get builder status failed: %s", string(body))
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return status, nil
}

// Health checks if the builder is healthy
func (bc *Client) Health() error {
	resp, err := bc.httpClient.Get(bc.baseURL + "/health")
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("builder is unhealthy")
	}

	return nil
}

// WaitForCompletion waits for a build job to complete
func (bc *Client) WaitForCompletion(jobID string, timeout time.Duration) (*BuildJob, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		job, err := bc.GetJobStatus(jobID)
		if err != nil {
			return nil, err
		}

		if job.Status == "success" || job.Status == "failed" {
			return job, nil
		}

		time.Sleep(5 * time.Second)
	}

	return nil, fmt.Errorf("build timeout after %v", timeout)
}

// SendHeartbeat sends a single heartbeat to the server.
func (bc *Client) SendHeartbeat(req *HeartbeatRequest) error {
	bc.metrics.IncHTTPRequests()
	bc.metrics.IncHeartbeatsTotal()

	data, err := json.Marshal(req)
	if err != nil {
		bc.metrics.IncHTTPRequestErrors()
		bc.metrics.IncHeartbeatsFailed()
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	resp, err := bc.httpClient.Post(
		bc.baseURL+"/api/v1/heartbeat",
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		bc.metrics.IncHTTPRequestErrors()
		bc.metrics.IncHeartbeatsFailed()
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		bc.metrics.IncHTTPRequestErrors()
		bc.metrics.IncHeartbeatsFailed()
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("heartbeat failed: %s", string(body))
	}

	var result HeartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		bc.metrics.IncHTTPRequestErrors()
		bc.metrics.IncHeartbeatsFailed()
		return fmt.Errorf("failed to decode heartbeat response: %w", err)
	}

	if !result.Success {
		bc.metrics.IncHeartbeatsFailed()
		return fmt.Errorf("heartbeat rejected: %s", result.Message)
	}

	return nil
}

// StartHeartbeat starts sending periodic heartbeats to the server.
func (bc *Client) StartHeartbeat(ctx context.Context, req *HeartbeatRequest, interval time.Duration) {
	bc.heartbeatWg.Add(1)
	go func() {
		defer bc.heartbeatWg.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-bc.heartbeatStop:
				return
			case <-ticker.C:
				// Update timestamp before sending
				req.Timestamp = time.Now()
				if err := bc.SendHeartbeat(req); err != nil {
					// Log error but continue
					_ = err
				}
			}
		}
	}()
}

// StopHeartbeat stops the heartbeat goroutine.
func (bc *Client) StopHeartbeat() {
	close(bc.heartbeatStop)
	bc.heartbeatWg.Wait()
}
