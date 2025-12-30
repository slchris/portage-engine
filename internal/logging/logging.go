// Package logging provides structured logging with rotation support.
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Level represents log level.
type Level int

const (
	// LevelDebug represents debug level.
	LevelDebug Level = iota
	// LevelInfo represents info level.
	LevelInfo
	// LevelWarn represents warning level.
	LevelWarn
	// LevelError represents error level.
	LevelError
)

// String returns string representation of log level.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Config holds logging configuration.
type Config struct {
	Enabled       bool
	Level         string
	Dir           string
	MaxSizeMB     int
	MaxAgeDays    int
	MaxBackups    int
	EnableConsole bool
	EnableFile    bool
}

// Logger provides structured logging with rotation.
type Logger struct {
	mu            sync.RWMutex
	enabled       bool
	level         Level
	dir           string
	maxSize       int64
	maxAge        time.Duration
	maxBackups    int
	currentFile   *os.File
	currentSize   int64
	enableConsole bool
	enableFile    bool
	writers       []io.Writer
}

// New creates a new Logger instance.
func New(cfg *Config) (*Logger, error) {
	if cfg == nil {
		cfg = &Config{
			Enabled:       false,
			EnableConsole: true,
		}
	}

	level := parseLevel(cfg.Level)

	l := &Logger{
		enabled:       cfg.Enabled,
		level:         level,
		dir:           cfg.Dir,
		maxSize:       int64(cfg.MaxSizeMB) * 1024 * 1024,
		maxAge:        time.Duration(cfg.MaxAgeDays) * 24 * time.Hour,
		maxBackups:    cfg.MaxBackups,
		enableConsole: cfg.EnableConsole,
		enableFile:    cfg.EnableFile,
	}

	if !cfg.Enabled {
		return l, nil
	}

	if cfg.EnableFile && cfg.Dir != "" {
		if err := os.MkdirAll(cfg.Dir, 0750); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}

		if err := l.rotate(); err != nil {
			return nil, fmt.Errorf("failed to initialize log file: %w", err)
		}

		// Clean old logs
		go l.cleanOldLogs()
	}

	l.updateWriters()
	return l, nil
}

// parseLevel parses log level string.
func parseLevel(s string) Level {
	switch s {
	case "DEBUG", "debug":
		return LevelDebug
	case "INFO", "info":
		return LevelInfo
	case "WARN", "warn", "WARNING", "warning":
		return LevelWarn
	case "ERROR", "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// updateWriters updates the list of writers based on configuration.
func (l *Logger) updateWriters() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writers = nil
	if l.enableConsole {
		l.writers = append(l.writers, os.Stdout)
	}
	if l.enableFile && l.currentFile != nil {
		l.writers = append(l.writers, l.currentFile)
	}
}

// rotate rotates the log file.
func (l *Logger) rotate() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.currentFile != nil {
		_ = l.currentFile.Close()
	}

	filename := filepath.Join(l.dir, fmt.Sprintf("app-%s.log", time.Now().Format("2006-01-02")))

	// Check if file exists and get size
	if info, err := os.Stat(filename); err == nil {
		l.currentSize = info.Size()
		if l.maxSize > 0 && l.currentSize >= l.maxSize {
			// Need to create new file with timestamp
			filename = filepath.Join(l.dir, fmt.Sprintf("app-%s.log", time.Now().Format("2006-01-02-150405")))
			l.currentSize = 0
		}
	} else {
		l.currentSize = 0
	}

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}

	l.currentFile = file
	return nil
}

// cleanOldLogs removes old log files.
func (l *Logger) cleanOldLogs() {
	if l.maxAge <= 0 && l.maxBackups <= 0 {
		return
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		l.performCleanup()
	}
}

// performCleanup performs the actual cleanup of old log files.
func (l *Logger) performCleanup() {
	l.mu.RLock()
	dir := l.dir
	maxAge := l.maxAge
	maxBackups := l.maxBackups
	l.mu.RUnlock()

	if dir == "" {
		return
	}

	files, err := filepath.Glob(filepath.Join(dir, "app-*.log"))
	if err != nil {
		return
	}

	toDelete := l.getFilesToDelete(files, maxAge, maxBackups)
	l.deleteFiles(toDelete)
}

// getFilesToDelete determines which files should be deleted.
func (l *Logger) getFilesToDelete(files []string, maxAge time.Duration, maxBackups int) []string {
	var toDelete []string
	now := time.Now()

	if maxAge > 0 {
		toDelete = append(toDelete, l.getOldFiles(files, now, maxAge)...)
	}

	if maxBackups > 0 && len(files) > maxBackups {
		toDelete = append(toDelete, l.getExcessFiles(files, maxBackups)...)
	}

	return toDelete
}

// getOldFiles returns files older than maxAge.
func (l *Logger) getOldFiles(files []string, now time.Time, maxAge time.Duration) []string {
	var oldFiles []string
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			oldFiles = append(oldFiles, file)
		}
	}
	return oldFiles
}

// getExcessFiles returns files exceeding the backup count.
func (l *Logger) getExcessFiles(files []string, maxBackups int) []string {
	type fileInfo struct {
		path    string
		modTime time.Time
	}

	fileInfos := make([]fileInfo, 0, len(files))
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		fileInfos = append(fileInfos, fileInfo{path: file, modTime: info.ModTime()})
	}

	// Sort by mod time (oldest first)
	for i := 0; i < len(fileInfos)-1; i++ {
		for j := i + 1; j < len(fileInfos); j++ {
			if fileInfos[i].modTime.After(fileInfos[j].modTime) {
				fileInfos[i], fileInfos[j] = fileInfos[j], fileInfos[i]
			}
		}
	}

	var excessFiles []string
	for i := 0; i < len(fileInfos)-maxBackups; i++ {
		excessFiles = append(excessFiles, fileInfos[i].path)
	}

	return excessFiles
}

// deleteFiles deletes the specified files.
func (l *Logger) deleteFiles(files []string) {
	for _, file := range files {
		_ = os.Remove(file)
	}
}

// log writes a log message.
func (l *Logger) log(level Level, format string, v ...interface{}) {
	if !l.enabled || level < l.level {
		return
	}

	l.mu.RLock()
	writers := l.writers
	currentSize := l.currentSize
	maxSize := l.maxSize
	l.mu.RUnlock()

	// Check if rotation is needed
	if l.enableFile && maxSize > 0 && currentSize >= maxSize {
		if err := l.rotate(); err != nil {
			log.Printf("Failed to rotate log: %v", err)
		}
		l.updateWriters()
		l.mu.RLock()
		writers = l.writers
		l.mu.RUnlock()
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level.String(), fmt.Sprintf(format, v...))

	for _, w := range writers {
		n, _ := w.Write([]byte(msg))
		if w == l.currentFile {
			l.mu.Lock()
			l.currentSize += int64(n)
			l.mu.Unlock()
		}
	}
}

// Debug logs a debug message.
func (l *Logger) Debug(format string, v ...interface{}) {
	l.log(LevelDebug, format, v...)
}

// Info logs an info message.
func (l *Logger) Info(format string, v ...interface{}) {
	l.log(LevelInfo, format, v...)
}

// Warn logs a warning message.
func (l *Logger) Warn(format string, v ...interface{}) {
	l.log(LevelWarn, format, v...)
}

// Error logs an error message.
func (l *Logger) Error(format string, v ...interface{}) {
	l.log(LevelError, format, v...)
}

// Close closes the logger.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.currentFile != nil {
		return l.currentFile.Close()
	}
	return nil
}

// IsEnabled returns whether logging is enabled.
func (l *Logger) IsEnabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.enabled
}

// GetLevel returns the current log level.
func (l *Logger) GetLevel() Level {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

// SetLevel sets the log level.
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}
