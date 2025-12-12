package builder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNewBuilderClient tests the creation of a builder client.
func TestNewBuilderClient(t *testing.T) {
	baseURL := "http://localhost:8080"
	client := NewBuilderClient(baseURL)

	if client == nil {
		t.Fatal("NewBuilderClient returned nil")
	}

	if client.baseURL != baseURL {
		t.Errorf("Expected baseURL=%s, got %s", baseURL, client.baseURL)
	}

	if client.httpClient == nil {
		t.Error("httpClient is nil")
	}
}

// TestSubmitBuild tests submitting a build request.
func TestSubmitBuild(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		if r.URL.Path != "/api/v1/build" {
			t.Errorf("Expected /api/v1/build, got %s", r.URL.Path)
		}

		response := map[string]interface{}{
			"job_id": "test-job-123",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewBuilderClient(server.URL)
	req := &LocalBuildRequest{
		PackageName: "dev-lang/python",
		Version:     "3.11",
	}

	jobID, err := client.SubmitBuild(req)
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	if jobID != "test-job-123" {
		t.Errorf("Expected job_id=test-job-123, got %s", jobID)
	}
}

// TestGetJobStatus tests getting job status.
func TestGetJobStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
		}

		job := &BuildJob{
			ID:        "test-job-123",
			Status:    "success",
			StartTime: time.Now().Add(-5 * time.Minute),
			EndTime:   time.Now(),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(job)
	}))
	defer server.Close()

	client := NewBuilderClient(server.URL)
	job, err := client.GetJobStatus("test-job-123")
	if err != nil {
		t.Fatalf("GetJobStatus failed: %v", err)
	}

	if job.ID != "test-job-123" {
		t.Errorf("Expected ID=test-job-123, got %s", job.ID)
	}

	if job.Status != "success" {
		t.Errorf("Expected status=success, got %s", job.Status)
	}
}

// TestGetBuilderStatus tests getting builder status.
func TestGetBuilderStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
		}

		status := map[string]interface{}{
			"workers":   4,
			"queued":    2,
			"building":  1,
			"completed": 10,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	}))
	defer server.Close()

	client := NewBuilderClient(server.URL)
	status, err := client.GetBuilderStatus()
	if err != nil {
		t.Fatalf("GetBuilderStatus failed: %v", err)
	}

	if status["workers"].(float64) != 4 {
		t.Errorf("Expected workers=4, got %v", status["workers"])
	}
}

// TestSendHeartbeat tests sending a heartbeat to the server.
func TestSendHeartbeat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		if r.URL.Path != "/api/v1/heartbeat" {
			t.Errorf("Expected /api/v1/heartbeat, got %s", r.URL.Path)
		}

		var req HeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if req.BuilderID == "" {
			t.Error("BuilderID is empty")
		}

		if req.Status == "" {
			t.Error("Status is empty")
		}

		response := HeartbeatResponse{
			Success: true,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewBuilderClient(server.URL)
	req := &HeartbeatRequest{
		BuilderID:  "builder-1",
		Status:     "healthy",
		Endpoint:   "http://localhost:9090",
		Capacity:   4,
		ActiveJobs: 2,
		Timestamp:  time.Now(),
	}

	err := client.SendHeartbeat(req)
	if err != nil {
		t.Fatalf("SendHeartbeat failed: %v", err)
	}
}

// TestSendHeartbeatFailure tests heartbeat failure scenarios.
func TestSendHeartbeatFailure(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		responseBody   interface{}
		expectedErrMsg string
	}{
		{
			name:           "server error",
			statusCode:     http.StatusInternalServerError,
			responseBody:   "internal server error",
			expectedErrMsg: "heartbeat failed",
		},
		{
			name:       "heartbeat rejected",
			statusCode: http.StatusOK,
			responseBody: HeartbeatResponse{
				Success: false,
				Message: "builder not registered",
			},
			expectedErrMsg: "heartbeat rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				if str, ok := tt.responseBody.(string); ok {
					_, _ = w.Write([]byte(str))
				} else {
					_ = json.NewEncoder(w).Encode(tt.responseBody)
				}
			}))
			defer server.Close()

			client := NewBuilderClient(server.URL)
			req := &HeartbeatRequest{
				BuilderID: "builder-1",
				Status:    "healthy",
				Timestamp: time.Now(),
			}

			err := client.SendHeartbeat(req)
			if err == nil {
				t.Fatal("Expected error but got nil")
			}
		})
	}
}

// TestStartStopHeartbeat tests starting and stopping heartbeat.
func TestStartStopHeartbeat(t *testing.T) {
	heartbeatCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		heartbeatCount++
		response := HeartbeatResponse{
			Success: true,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewBuilderClient(server.URL)
	req := &HeartbeatRequest{
		BuilderID: "builder-1",
		Status:    "healthy",
		Endpoint:  "http://localhost:9090",
		Capacity:  4,
		Timestamp: time.Now(),
	}

	// Start heartbeat with short interval
	ctx := testContext(t)
	client.StartHeartbeat(ctx, req, 100*time.Millisecond)

	// Wait for a few heartbeats
	time.Sleep(350 * time.Millisecond)

	// Stop heartbeat
	client.StopHeartbeat()

	// Wait to ensure no more heartbeats are sent
	beforeStop := heartbeatCount
	time.Sleep(200 * time.Millisecond)
	afterStop := heartbeatCount

	if beforeStop < 2 {
		t.Errorf("Expected at least 2 heartbeats, got %d", beforeStop)
	}

	if afterStop != beforeStop {
		t.Errorf("Heartbeat continued after stop: before=%d, after=%d", beforeStop, afterStop)
	}
}

// testContext creates a test context.
func testContext(t *testing.T) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}
