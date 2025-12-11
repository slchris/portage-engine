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
	configPath = flag.String("config", "configs/server.yaml", "Path to configuration file")
	port       = flag.Int("port", 8080, "Server port")
)

func main() {
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadServerConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Override port if specified
	if *port != 8080 {
		cfg.Port = *port
	}

	// Create server instance
	srv := server.New(cfg)

	// HTTP server configuration
	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      srv.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
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

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
		log.Printf("Server forced to shutdown: %v", err)
		return
	}

	log.Println("Server exited")
}
