package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

// responseWriter wraps http.ResponseWriter to capture the status code and bytes written.
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
	wroteHeader  bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // default if WriteHeader is never called
	}
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.statusCode = code
		rw.wroteHeader = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

// requestIDMiddleware generates a unique request ID for each request and adds it
// to the response headers. If the client sends an X-Request-ID header, it is
// reused for request tracing across services (Server → Builder).
func (s *Server) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Set on response for client correlation
		w.Header().Set("X-Request-ID", requestID)

		// Store in request context via header so downstream handlers can read it
		r.Header.Set("X-Request-ID", requestID)

		next.ServeHTTP(w, r)
	})
}

// enhancedLoggingMiddleware provides structured request logging with:
// - Request ID for correlation
// - Response status code
// - Processing duration
// - Response body size
// - Goroutine count (for resource monitoring)
func (s *Server) enhancedLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap ResponseWriter to capture status code and bytes
		rw := newResponseWriter(w)

		// Process request
		next.ServeHTTP(rw, r)

		// Calculate duration
		duration := time.Since(start)
		requestID := r.Header.Get("X-Request-ID")

		// Update goroutine metric
		s.metrics.UpdateGoroutines(int64(runtime.NumGoroutine()))

		// Structured log output
		level := slog.LevelInfo
		if rw.statusCode >= 500 {
			level = slog.LevelError
		} else if rw.statusCode >= 400 {
			level = slog.LevelWarn
		}

		// Skip noisy health check logs unless they fail
		path := r.URL.Path
		if (path == "/health" || path == "/readyz" || path == "/livez") && rw.statusCode < 400 {
			return
		}

		slog.Log(r.Context(), level, "http request",
			"method", r.Method,
			"path", r.RequestURI,
			"status", rw.statusCode,
			"duration_ms", fmt.Sprintf("%.2f", float64(duration.Microseconds())/1000.0),
			"bytes", rw.bytesWritten,
			"remote", stripPort(r.RemoteAddr),
			"request_id", requestID,
		)
	})
}

// stripPort removes the port from a remote address for cleaner logging.
func stripPort(addr string) string {
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}
