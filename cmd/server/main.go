// Package main implements the Portage Engine Server.
// The server handles package queries, build requests, and package synchronization.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/slchris/portage-engine/internal/server"
	"github.com/slchris/portage-engine/pkg/config"
)

var (
	configPath  = flag.String("config", "configs/server.conf", "Path to configuration file")
	port        = flag.Int("port", 8080, "Server port")
	showVersion = flag.Bool("version", false, "Print version and exit")
)

// Version information (injected at build time via -ldflags).
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("portage-server %s (commit: %s, built: %s)\n", version, commit, buildTime)
		return
	}

	// Load configuration
	cfg, err := config.LoadServerConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Validate configuration and print warnings
	if warnings := cfg.Validate(); len(warnings) > 0 {
		for _, w := range warnings {
			log.Printf("WARNING: %s", w)
		}
	}

	// Override port if specified
	if *port != 8080 {
		cfg.Port = *port
	}

	// Propagate version info to the server package
	server.Version = version
	server.Commit = commit
	server.BuildTime = buildTime

	// Create server instance
	srv := server.New(cfg)

	// Initialize server (GPG keys, etc.)
	if err := srv.Initialize(); err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	// HTTP server configuration.
	//
	// No ReadTimeout/WriteTimeout: these are whole-request deadlines that would
	// abort large binary-package uploads/downloads mid-stream. ReadHeaderTimeout
	// still bounds slow-header (Slowloris) attacks, and IdleTimeout reaps idle
	// keep-alive connections; per-request work uses request context deadlines.
	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           srv.Router(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Starting Portage Engine Server on port %d", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop the HTTP server first so no new requests can reach the builder's work
	// queue, and in-flight requests drain, before we close that queue. Doing this
	// in the opposite order lets an in-flight build request panic on send to a
	// closed channel.
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	// Now shut down server components (saves state, closes the work queue).
	srv.Shutdown()

	log.Println("Server exited")
}
