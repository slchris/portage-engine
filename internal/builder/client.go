package builder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client communicates with remote builder services
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewBuilderClient creates a new builder client
func NewBuilderClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SubmitBuild sends a build request to the remote builder
func (bc *Client) SubmitBuild(req *LocalBuildRequest) (string, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := bc.httpClient.Post(
		bc.baseURL+"/api/v1/build",
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("build submission failed: %s", string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	jobID, ok := result["job_id"].(string)
	if !ok {
		return "", fmt.Errorf("invalid response: missing job_id")
	}

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
