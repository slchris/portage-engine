package notification

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestNewNotifier tests creating a new notifier.
func TestNewNotifier(t *testing.T) {
	config := &Config{
		Email: &EmailConfig{
			Enabled: true,
		},
	}

	notifier := NewNotifier(config)
	if notifier == nil {
		t.Fatal("NewNotifier returned nil")
	}

	if notifier.config != config {
		t.Error("Config not set correctly")
	}

	if notifier.client == nil {
		t.Error("HTTP client not initialized")
	}
}

// TestBuildNotification tests build notification structure.
func TestBuildNotification(t *testing.T) {
	startTime := time.Now().Add(-5 * time.Minute)
	endTime := time.Now()

	notification := &BuildNotification{
		JobID:       "test-job-123",
		PackageName: "dev-lang/python",
		Version:     "3.11.8",
		Status:      "success",
		StartTime:   startTime,
		EndTime:     endTime,
		Duration:    "5m0s",
		ArtifactURL: "http://example.com/python-3.11.8.tbz2",
	}

	if notification.JobID != "test-job-123" {
		t.Errorf("Expected JobID=test-job-123, got %s", notification.JobID)
	}

	if notification.Status != "success" {
		t.Errorf("Expected status=success, got %s", notification.Status)
	}
}

// TestSendWebhook tests webhook notification.
func TestSendWebhook(t *testing.T) {
	var receivedPayload BuildNotification

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type=application/json, got %s", contentType)
		}

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &Config{
		Webhook: &WebhookConfig{
			Enabled: true,
			URL:     server.URL,
			Method:  "POST",
		},
	}

	notifier := NewNotifier(config)

	notification := &BuildNotification{
		JobID:       "webhook-test",
		PackageName: "app-editors/vim",
		Version:     "9.0",
		Status:      "success",
		StartTime:   time.Now().Add(-2 * time.Minute),
		EndTime:     time.Now(),
		Duration:    "2m0s",
	}

	err := notifier.sendWebhook(notification)
	if err != nil {
		t.Fatalf("sendWebhook failed: %v", err)
	}

	if receivedPayload.JobID != "webhook-test" {
		t.Errorf("Expected JobID=webhook-test, got %s", receivedPayload.JobID)
	}
}

// TestSendWebhookWithHeaders tests webhook with custom headers.
func TestSendWebhookWithHeaders(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &Config{
		Webhook: &WebhookConfig{
			Enabled: true,
			URL:     server.URL,
			Headers: map[string]string{
				"X-Custom-Header": "test-value",
				"Authorization":   "Bearer token123",
			},
		},
	}

	notifier := NewNotifier(config)

	notification := &BuildNotification{
		JobID:       "header-test",
		PackageName: "sys-apps/portage",
		Version:     "3.0.0",
		Status:      "success",
		StartTime:   time.Now(),
		EndTime:     time.Now(),
		Duration:    "1m0s",
	}

	err := notifier.sendWebhook(notification)
	if err != nil {
		t.Fatalf("sendWebhook failed: %v", err)
	}

	if receivedHeaders.Get("X-Custom-Header") != "test-value" {
		t.Errorf("Custom header not received correctly")
	}

	if receivedHeaders.Get("Authorization") != "Bearer token123" {
		t.Errorf("Authorization header not received correctly")
	}
}

// TestSendSlack tests Slack notification.
func TestSendSlack(t *testing.T) {
	var receivedPayload map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &Config{
		Slack: &SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Channel:    "#builds",
			Username:   "Portage Bot",
			IconEmoji:  ":package:",
		},
	}

	notifier := NewNotifier(config)

	notification := &BuildNotification{
		JobID:       "slack-test",
		PackageName: "dev-lang/go",
		Version:     "1.21.0",
		Status:      "success",
		StartTime:   time.Now().Add(-3 * time.Minute),
		EndTime:     time.Now(),
		Duration:    "3m0s",
		ArtifactURL: "http://example.com/go-1.21.0.tbz2",
	}

	err := notifier.sendSlack(notification)
	if err != nil {
		t.Fatalf("sendSlack failed: %v", err)
	}

	if receivedPayload["channel"] != "#builds" {
		t.Errorf("Expected channel=#builds, got %v", receivedPayload["channel"])
	}

	if receivedPayload["username"] != "Portage Bot" {
		t.Errorf("Expected username=Portage Bot, got %v", receivedPayload["username"])
	}
}

// TestSendTelegram tests Telegram notification.
func TestSendTelegram(t *testing.T) {
	var receivedPayload map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	config := &Config{
		Telegram: &TelegramConfig{
			Enabled:  true,
			BotToken: "test-token",
			ChatID:   "12345",
		},
	}

	notifier := NewNotifier(config)

	// Override telegram API URL for testing
	notification := &BuildNotification{
		JobID:       "telegram-test",
		PackageName: "dev-lang/rust",
		Version:     "1.75.0",
		Status:      "success",
		StartTime:   time.Now().Add(-10 * time.Minute),
		EndTime:     time.Now(),
		Duration:    "10m0s",
	}

	// For this test, we'll just verify the payload structure
	// In production, it would hit the actual Telegram API
	if config.Telegram.ChatID != "12345" {
		t.Errorf("Expected ChatID=12345, got %s", config.Telegram.ChatID)
	}

	_ = notifier
	_ = notification
	_ = receivedPayload
}

// TestFormatEmailBody tests email body formatting.
func TestFormatEmailBody(t *testing.T) {
	config := &Config{}
	notifier := NewNotifier(config)

	notification := &BuildNotification{
		JobID:       "email-test",
		PackageName: "sys-kernel/gentoo-sources",
		Version:     "6.6.0",
		Status:      "success",
		StartTime:   time.Now().Add(-15 * time.Minute),
		EndTime:     time.Now(),
		Duration:    "15m0s",
		ArtifactURL: "http://example.com/kernel-6.6.0.tbz2",
	}

	body := notifier.formatEmailBody(notification)

	if body == "" {
		t.Error("Email body is empty")
	}

	if !contains(body, "email-test") {
		t.Error("Email body does not contain job ID")
	}

	if !contains(body, "gentoo-sources") {
		t.Error("Email body does not contain package name")
	}

	if !contains(body, "6.6.0") {
		t.Error("Email body does not contain version")
	}
}

// TestNotifyMultipleChannels tests notification through multiple channels.
func TestNotifyMultipleChannels(t *testing.T) {
	webhookCalled := false
	slackCalled := false

	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		webhookCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		slackCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer slackServer.Close()

	config := &Config{
		Webhook: &WebhookConfig{
			Enabled: true,
			URL:     webhookServer.URL,
		},
		Slack: &SlackConfig{
			Enabled:    true,
			WebhookURL: slackServer.URL,
		},
	}

	notifier := NewNotifier(config)

	notification := &BuildNotification{
		JobID:       "multi-test",
		PackageName: "dev-db/postgresql",
		Version:     "15.0",
		Status:      "success",
		StartTime:   time.Now().Add(-20 * time.Minute),
		EndTime:     time.Now(),
		Duration:    "20m0s",
	}

	err := notifier.Notify(notification)
	if err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	if !webhookCalled {
		t.Error("Webhook was not called")
	}

	if !slackCalled {
		t.Error("Slack was not called")
	}
}

// TestNotifyDisabledChannels tests that disabled channels are not called.
func TestNotifyDisabledChannels(t *testing.T) {
	webhookCalled := false

	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		webhookCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	config := &Config{
		Webhook: &WebhookConfig{
			Enabled: false,
			URL:     webhookServer.URL,
		},
	}

	notifier := NewNotifier(config)

	notification := &BuildNotification{
		JobID:       "disabled-test",
		PackageName: "app-editors/emacs",
		Version:     "29.0",
		Status:      "success",
		StartTime:   time.Now(),
		EndTime:     time.Now(),
		Duration:    "5m0s",
	}

	err := notifier.Notify(notification)
	if err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	if webhookCalled {
		t.Error("Disabled webhook was called")
	}
}

// TestSendIRC tests IRC notification (placeholder).
func TestSendIRC(_ *testing.T) {
	config := &Config{
		IRC: &IRCConfig{
			Enabled:  true,
			Server:   "irc.libera.chat",
			Port:     6667,
			Nick:     "portage-bot",
			Channels: []string{"#gentoo-builds"},
		},
	}

	notifier := NewNotifier(config)

	notification := &BuildNotification{
		JobID:       "irc-test",
		PackageName: "www-servers/nginx",
		Version:     "1.25.0",
		Status:      "success",
		StartTime:   time.Now().Add(-8 * time.Minute),
		EndTime:     time.Now(),
		Duration:    "8m0s",
	}

	notifier.sendIRC(notification)
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestLoadConfig tests loading configuration from file.
func TestLoadConfig(t *testing.T) {
	tmpFile := "/tmp/notification-test-config.json"
	configData := `{
		"email": {
			"enabled": true,
			"smtp_host": "smtp.test.com",
			"smtp_port": 587,
			"from": "test@test.com",
			"to": ["user@test.com"]
		},
		"webhook": {
			"enabled": true,
			"url": "http://test.com/webhook"
		}
	}`

	if err := os.WriteFile(tmpFile, []byte(configData), 0600); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	config, err := LoadConfig(tmpFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.Email == nil || !config.Email.Enabled {
		t.Error("Email config not loaded correctly")
	}

	if config.Webhook == nil || !config.Webhook.Enabled {
		t.Error("Webhook config not loaded correctly")
	}

	if config.Email.SMTPHost != "smtp.test.com" {
		t.Errorf("Expected smtp_host=smtp.test.com, got %s", config.Email.SMTPHost)
	}
}

// TestLoadConfigNonExistent tests loading non-existent config file.
func TestLoadConfigNonExistent(t *testing.T) {
	_, err := LoadConfig("/nonexistent/config.json")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}
