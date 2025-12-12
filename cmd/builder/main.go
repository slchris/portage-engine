// Package main provides the Portage Builder service.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/slchris/portage-engine/internal/builder"
	"github.com/slchris/portage-engine/internal/gpg"
	"github.com/slchris/portage-engine/pkg/config"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "configs/builder.conf", "Path to configuration file")
	port := flag.Int("port", 9090, "Builder service port")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadBuilderConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Override port if specified
	if *port != 9090 {
		cfg.Port = *port
	}

	log.Printf("Starting Portage Builder Service on port %d", cfg.Port)

	// Initialize GPG signer if enabled
	var signer *gpg.Signer
	if cfg.GPGEnabled {
		signer = gpg.NewSigner(cfg.GPGKeyID, cfg.GPGKeyPath, true)
		if err := gpg.CheckGPG(); err != nil {
			log.Fatalf("GPG check failed: %v", err)
		}
		log.Printf("GPG signing enabled with key: %s", cfg.GPGKeyID)
	}

	// Create builder instance
	bldr := builder.NewLocalBuilder(cfg.Workers, signer, cfg)

	// Setup HTTP server
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Status endpoint
	mux.HandleFunc("/api/v1/status", func(w http.ResponseWriter, _ *http.Request) {
		status := bldr.GetStatus()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	})

	// Build request endpoint
	mux.HandleFunc("/api/v1/build", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req builder.LocalBuildRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		jobID, err := bldr.SubmitBuild(&req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"job_id": jobID,
			"status": "queued",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})

	// Job status endpoint
	mux.HandleFunc("/api/v1/jobs/", func(w http.ResponseWriter, r *http.Request) {
		jobID := r.URL.Path[len("/api/v1/jobs/"):]
		if jobID == "" {
			http.Error(w, "Job ID required", http.StatusBadRequest)
			return
		}

		status, err := bldr.GetJobStatus(jobID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	})

	// List all jobs
	mux.HandleFunc("/api/v1/jobs", func(w http.ResponseWriter, _ *http.Request) {
		jobs := bldr.ListJobs()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jobs)
	})

	// Start HTTP server
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down builder service...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Printf("Builder service listening on :%d", cfg.Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("%s %s %s", r.Method, r.RequestURI, r.RemoteAddr)
		next.ServeHTTP(w, r)
		log.Printf("Completed in %v", time.Since(start))
	})
}
