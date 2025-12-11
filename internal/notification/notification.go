// Package notification provides build completion notification capabilities.
package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// BuildNotification represents a build completion notification.
type BuildNotification struct {
	JobID       string    `json:"job_id"`
	PackageName string    `json:"package_name"`
	Version     string    `json:"version"`
	Status      string    `json:"status"` // success, failed
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	Duration    string    `json:"duration"`
	BuildLog    string    `json:"build_log,omitempty"`
	Error       string    `json:"error,omitempty"`
	ArtifactURL string    `json:"artifact_url,omitempty"`
}

// Config represents notification configuration.
type Config struct {
	Email    *EmailConfig    `json:"email,omitempty"`
	Webhook  *WebhookConfig  `json:"webhook,omitempty"`
	IRC      *IRCConfig      `json:"irc,omitempty"`
	Slack    *SlackConfig    `json:"slack,omitempty"`
	Telegram *TelegramConfig `json:"telegram,omitempty"`
}

// EmailConfig represents email notification configuration.
type EmailConfig struct {
	Enabled  bool     `json:"enabled"`
	SMTPHost string   `json:"smtp_host"`
	SMTPPort int      `json:"smtp_port"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	From     string   `json:"from"`
	To       []string `json:"to"`
	Subject  string   `json:"subject"`
}

// WebhookConfig represents webhook notification configuration.
type WebhookConfig struct {
	Enabled bool              `json:"enabled"`
	URL     string            `json:"url"`
	Method  string            `json:"method"` // POST, PUT
	Headers map[string]string `json:"headers,omitempty"`
	Timeout int               `json:"timeout"` // seconds
}

// IRCConfig represents IRC notification configuration.
type IRCConfig struct {
	Enabled  bool     `json:"enabled"`
	Server   string   `json:"server"`
	Port     int      `json:"port"`
	Nick     string   `json:"nick"`
	Channels []string `json:"channels"`
	UseTLS   bool     `json:"use_tls"`
}

// SlackConfig represents Slack notification configuration.
type SlackConfig struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
	Channel    string `json:"channel,omitempty"`
	Username   string `json:"username,omitempty"`
	IconEmoji  string `json:"icon_emoji,omitempty"`
}

// TelegramConfig represents Telegram notification configuration.
type TelegramConfig struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// Notifier handles sending notifications.
type Notifier struct {
	config *Config
	client *http.Client
}

// NewNotifier creates a new notification handler.
func NewNotifier(config *Config) *Notifier {
	return &Notifier{
		config: config,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// LoadConfig loads notification configuration from a file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &config, nil
}

// Notify sends notification through all enabled channels.
func (n *Notifier) Notify(notification *BuildNotification) error {
	var errors []string

	if n.config.Email != nil && n.config.Email.Enabled {
		if err := n.sendEmail(notification); err != nil {
			errors = append(errors, fmt.Sprintf("email: %v", err))
		}
	}

	if n.config.Webhook != nil && n.config.Webhook.Enabled {
		if err := n.sendWebhook(notification); err != nil {
			errors = append(errors, fmt.Sprintf("webhook: %v", err))
		}
	}

	if n.config.IRC != nil && n.config.IRC.Enabled {
		if err := n.sendIRC(notification); err != nil {
			errors = append(errors, fmt.Sprintf("irc: %v", err))
		}
	}

	if n.config.Slack != nil && n.config.Slack.Enabled {
		if err := n.sendSlack(notification); err != nil {
			errors = append(errors, fmt.Sprintf("slack: %v", err))
		}
	}

	if n.config.Telegram != nil && n.config.Telegram.Enabled {
		if err := n.sendTelegram(notification); err != nil {
			errors = append(errors, fmt.Sprintf("telegram: %v", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("notification errors: %s", strings.Join(errors, "; "))
	}

	return nil
}

// sendEmail sends email notification.
func (n *Notifier) sendEmail(notification *BuildNotification) error {
	cfg := n.config.Email

	subject := cfg.Subject
	if subject == "" {
		subject = fmt.Sprintf("Build %s: %s-%s", notification.Status, notification.PackageName, notification.Version)
	}

	body := n.formatEmailBody(notification)

	msg := fmt.Sprintf("From: %s\r\n", cfg.From)
	msg += fmt.Sprintf("To: %s\r\n", strings.Join(cfg.To, ","))
	msg += fmt.Sprintf("Subject: %s\r\n", subject)
	msg += "Content-Type: text/plain; charset=UTF-8\r\n"
	msg += "\r\n"
	msg += body

	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPHost)
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)

	err := smtp.SendMail(addr, auth, cfg.From, cfg.To, []byte(msg))
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	log.Printf("Email notification sent to %v", cfg.To)
	return nil
}

// formatEmailBody formats the email body.
func (n *Notifier) formatEmailBody(notification *BuildNotification) string {
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf("Build Status: %s\n", strings.ToUpper(notification.Status)))
	buf.WriteString(fmt.Sprintf("Job ID: %s\n", notification.JobID))
	buf.WriteString(fmt.Sprintf("Package: %s-%s\n", notification.PackageName, notification.Version))
	buf.WriteString(fmt.Sprintf("Duration: %s\n", notification.Duration))
	buf.WriteString(fmt.Sprintf("Started: %s\n", notification.StartTime.Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("Finished: %s\n", notification.EndTime.Format(time.RFC3339)))

	if notification.ArtifactURL != "" {
		buf.WriteString(fmt.Sprintf("\nArtifact: %s\n", notification.ArtifactURL))
	}

	if notification.Error != "" {
		buf.WriteString(fmt.Sprintf("\nError: %s\n", notification.Error))
	}

	if notification.BuildLog != "" && len(notification.BuildLog) < 500 {
		buf.WriteString(fmt.Sprintf("\nBuild Log:\n%s\n", notification.BuildLog))
	}

	return buf.String()
}

// sendWebhook sends webhook notification.
func (n *Notifier) sendWebhook(notification *BuildNotification) error {
	cfg := n.config.Webhook

	payload, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	method := cfg.Method
	if method == "" {
		method = "POST"
	}

	req, err := http.NewRequest(method, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range cfg.Headers {
		req.Header.Set(key, value)
	}

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("Webhook notification sent to %s", cfg.URL)
	return nil
}

// sendIRC sends IRC notification.
func (n *Notifier) sendIRC(notification *BuildNotification) error {
	// IRC notification is implemented as a placeholder
	// In production, this would use an IRC library like github.com/thoj/go-ircevent
	cfg := n.config.IRC

	message := fmt.Sprintf("[Build %s] %s-%s (Job: %s) - Duration: %s",
		notification.Status,
		notification.PackageName,
		notification.Version,
		notification.JobID,
		notification.Duration,
	)

	log.Printf("IRC notification would be sent to %s:%d channels=%v: %s",
		cfg.Server, cfg.Port, cfg.Channels, message)

	// Actual IRC implementation would go here
	return nil
}

// sendSlack sends Slack notification.
func (n *Notifier) sendSlack(notification *BuildNotification) error {
	cfg := n.config.Slack

	color := "good"
	if notification.Status == "failed" {
		color = "danger"
	}

	payload := map[string]interface{}{
		"username":   cfg.Username,
		"icon_emoji": cfg.IconEmoji,
		"channel":    cfg.Channel,
		"attachments": []map[string]interface{}{
			{
				"color": color,
				"title": fmt.Sprintf("Build %s: %s-%s", strings.ToUpper(notification.Status), notification.PackageName, notification.Version),
				"fields": []map[string]interface{}{
					{
						"title": "Job ID",
						"value": notification.JobID,
						"short": true,
					},
					{
						"title": "Duration",
						"value": notification.Duration,
						"short": true,
					},
					{
						"title": "Package",
						"value": fmt.Sprintf("%s-%s", notification.PackageName, notification.Version),
						"short": false,
					},
				},
				"footer": "Portage Engine",
				"ts":     notification.EndTime.Unix(),
			},
		},
	}

	if notification.ArtifactURL != "" {
		payload["attachments"].([]map[string]interface{})[0]["fields"] = append(
			payload["attachments"].([]map[string]interface{})[0]["fields"].([]map[string]interface{}),
			map[string]interface{}{
				"title": "Artifact",
				"value": notification.ArtifactURL,
				"short": false,
			},
		)
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal slack payload: %w", err)
	}

	resp, err := n.client.Post(cfg.WebhookURL, "application/json", bytes.NewReader(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to send slack notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("Slack notification sent")
	return nil
}

// sendTelegram sends Telegram notification.
func (n *Notifier) sendTelegram(notification *BuildNotification) error {
	cfg := n.config.Telegram

	status := "✅ SUCCESS"
	if notification.Status == "failed" {
		status = "❌ FAILED"
	}

	text := fmt.Sprintf(
		"*Build %s*\n\n"+
			"*Package:* %s-%s\n"+
			"*Job ID:* `%s`\n"+
			"*Duration:* %s\n"+
			"*Finished:* %s",
		status,
		notification.PackageName,
		notification.Version,
		notification.JobID,
		notification.Duration,
		notification.EndTime.Format("2006-01-02 15:04:05"),
	)

	if notification.ArtifactURL != "" {
		text += fmt.Sprintf("\n*Artifact:* %s", notification.ArtifactURL)
	}

	if notification.Error != "" {
		text += fmt.Sprintf("\n*Error:* %s", notification.Error)
	}

	payload := map[string]interface{}{
		"chat_id":    cfg.ChatID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal telegram payload: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)
	resp, err := n.client.Post(url, "application/json", bytes.NewReader(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to send telegram notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("Telegram notification sent to chat %s", cfg.ChatID)
	return nil
}
