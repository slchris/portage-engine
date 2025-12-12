package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: false,
		},
		{
			name: "disabled logger",
			cfg: &Config{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "console only",
			cfg: &Config{
				Enabled:       true,
				EnableConsole: true,
				EnableFile:    false,
				Level:         "INFO",
			},
			wantErr: false,
		},
		{
			name: "file only",
			cfg: &Config{
				Enabled:       true,
				EnableConsole: false,
				EnableFile:    true,
				Level:         "DEBUG",
				Dir:           t.TempDir(),
				MaxSizeMB:     10,
				MaxAgeDays:    7,
				MaxBackups:    5,
			},
			wantErr: false,
		},
		{
			name: "both console and file",
			cfg: &Config{
				Enabled:       true,
				EnableConsole: true,
				EnableFile:    true,
				Level:         "WARN",
				Dir:           t.TempDir(),
				MaxSizeMB:     1,
				MaxAgeDays:    1,
				MaxBackups:    3,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if logger == nil {
				t.Error("Expected non-nil logger")
				return
			}
			if logger != nil {
				_ = logger.Close()
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"DEBUG", LevelDebug},
		{"debug", LevelDebug},
		{"INFO", LevelInfo},
		{"info", LevelInfo},
		{"WARN", LevelWarn},
		{"warn", LevelWarn},
		{"WARNING", LevelWarn},
		{"warning", LevelWarn},
		{"ERROR", LevelError},
		{"error", LevelError},
		{"invalid", LevelInfo},
		{"", LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLevel(tt.input)
			if got != tt.expected {
				t.Errorf("parseLevel(%s) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{Level(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.level.String()
			if got != tt.expected {
				t.Errorf("Level.String() = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestLogLevels(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		Enabled:       true,
		EnableConsole: false,
		EnableFile:    true,
		Level:         "WARN",
		Dir:           tmpDir,
		MaxSizeMB:     10,
		MaxAgeDays:    7,
		MaxBackups:    5,
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer func() { _ = logger.Close() }()

	// These should not be logged (below WARN level)
	logger.Debug("debug message")
	logger.Info("info message")

	// These should be logged
	logger.Warn("warn message")
	logger.Error("error message")

	// Give time for writes
	time.Sleep(100 * time.Millisecond)

	// Check log file
	files, err := filepath.Glob(filepath.Join(tmpDir, "app-*.log"))
	if err != nil {
		t.Fatalf("Failed to list log files: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("No log files created")
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	contentStr := string(content)
	if strings.Contains(contentStr, "debug message") {
		t.Error("Debug message should not be logged")
	}
	if strings.Contains(contentStr, "info message") {
		t.Error("Info message should not be logged")
	}
	if !strings.Contains(contentStr, "warn message") {
		t.Error("Warn message should be logged")
	}
	if !strings.Contains(contentStr, "error message") {
		t.Error("Error message should be logged")
	}
}

func TestLogRotation(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		Enabled:       true,
		EnableConsole: false,
		EnableFile:    true,
		Level:         "INFO",
		Dir:           tmpDir,
		MaxSizeMB:     1, // 1MB max size
		MaxAgeDays:    7,
		MaxBackups:    3,
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer func() { _ = logger.Close() }()

	// Write enough data to trigger rotation
	largeMsg := strings.Repeat("A", 1024*100) // 100KB per message
	for i := 0; i < 15; i++ {
		logger.Info("Message %d: %s", i, largeMsg)
	}

	time.Sleep(200 * time.Millisecond)

	// Check if multiple log files were created
	files, err := filepath.Glob(filepath.Join(tmpDir, "app-*.log"))
	if err != nil {
		t.Fatalf("Failed to list log files: %v", err)
	}

	if len(files) < 2 {
		t.Errorf("Expected at least 2 log files after rotation, got %d", len(files))
	}
}

func TestLogDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		Enabled:       false,
		EnableConsole: false,
		EnableFile:    true,
		Level:         "INFO",
		Dir:           tmpDir,
		MaxSizeMB:     10,
		MaxAgeDays:    7,
		MaxBackups:    5,
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer func() { _ = logger.Close() }()

	if logger.IsEnabled() {
		t.Error("Logger should be disabled")
	}

	logger.Info("test message")
	time.Sleep(100 * time.Millisecond)

	// Check no log files were created
	files, err := filepath.Glob(filepath.Join(tmpDir, "app-*.log"))
	if err != nil {
		t.Fatalf("Failed to list log files: %v", err)
	}

	if len(files) > 0 {
		t.Errorf("Expected no log files when disabled, got %d", len(files))
	}
}

func TestSetLevel(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		Enabled:       true,
		EnableConsole: false,
		EnableFile:    true,
		Level:         "INFO",
		Dir:           tmpDir,
		MaxSizeMB:     10,
		MaxAgeDays:    7,
		MaxBackups:    5,
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer func() { _ = logger.Close() }()

	if logger.GetLevel() != LevelInfo {
		t.Errorf("Expected initial level INFO, got %v", logger.GetLevel())
	}

	logger.SetLevel(LevelDebug)
	if logger.GetLevel() != LevelDebug {
		t.Errorf("Expected level DEBUG after SetLevel, got %v", logger.GetLevel())
	}

	// Now debug should be logged
	logger.Debug("debug after level change")
	time.Sleep(100 * time.Millisecond)

	files, err := filepath.Glob(filepath.Join(tmpDir, "app-*.log"))
	if err != nil {
		t.Fatalf("Failed to list log files: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("No log files created")
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), "debug after level change") {
		t.Error("Debug message should be logged after level change")
	}
}

func TestLogFormat(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		Enabled:       true,
		EnableConsole: false,
		EnableFile:    true,
		Level:         "INFO",
		Dir:           tmpDir,
		MaxSizeMB:     10,
		MaxAgeDays:    7,
		MaxBackups:    5,
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer func() { _ = logger.Close() }()

	logger.Info("test message with arg: %s", "value")
	time.Sleep(100 * time.Millisecond)

	files, err := filepath.Glob(filepath.Join(tmpDir, "app-*.log"))
	if err != nil {
		t.Fatalf("Failed to list log files: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("No log files created")
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "[INFO]") {
		t.Error("Log should contain [INFO] level")
	}
	if !strings.Contains(contentStr, "test message with arg: value") {
		t.Error("Log should contain formatted message")
	}
	// Check timestamp format (YYYY-MM-DD HH:MM:SS)
	if !strings.Contains(contentStr, time.Now().Format("2006-01-02")) {
		t.Error("Log should contain timestamp")
	}
}

func TestCleanOldLogs(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some old log files
	oldDate := time.Now().Add(-10 * 24 * time.Hour)
	oldFile := filepath.Join(tmpDir, "app-"+oldDate.Format("2006-01-02")+".log")
	if err := os.WriteFile(oldFile, []byte("old log"), 0644); err != nil {
		t.Fatalf("Failed to create old log file: %v", err)
	}

	// Set modification time to old date
	if err := os.Chtimes(oldFile, oldDate, oldDate); err != nil {
		t.Fatalf("Failed to set old file time: %v", err)
	}

	cfg := &Config{
		Enabled:       true,
		EnableConsole: false,
		EnableFile:    true,
		Level:         "INFO",
		Dir:           tmpDir,
		MaxSizeMB:     10,
		MaxAgeDays:    7, // Keep only 7 days
		MaxBackups:    5,
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer func() { _ = logger.Close() }()

	logger.Info("new message")
	time.Sleep(100 * time.Millisecond)

	// Manually trigger cleanup
	logger.performCleanup()

	// Check if old file was deleted
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("Old log file should have been deleted")
	}
}

func TestMaxBackups(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple log files
	for i := 0; i < 10; i++ {
		date := time.Now().Add(-time.Duration(i) * 24 * time.Hour)
		filename := filepath.Join(tmpDir, "app-"+date.Format("2006-01-02")+".log")
		if err := os.WriteFile(filename, []byte("log"), 0644); err != nil {
			t.Fatalf("Failed to create log file: %v", err)
		}
		if err := os.Chtimes(filename, date, date); err != nil {
			t.Fatalf("Failed to set file time: %v", err)
		}
	}

	cfg := &Config{
		Enabled:       true,
		EnableConsole: false,
		EnableFile:    true,
		Level:         "INFO",
		Dir:           tmpDir,
		MaxSizeMB:     10,
		MaxAgeDays:    0, // No age limit
		MaxBackups:    5, // Keep only 5 files
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer func() { _ = logger.Close() }()

	// Manually trigger cleanup
	logger.performCleanup()

	// Count remaining files
	files, err := filepath.Glob(filepath.Join(tmpDir, "app-*.log"))
	if err != nil {
		t.Fatalf("Failed to list log files: %v", err)
	}

	if len(files) > 6 { // 5 backups + current
		t.Errorf("Expected at most 6 log files, got %d", len(files))
	}
}
