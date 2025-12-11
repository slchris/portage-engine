// Package main implements the Portage Engine Dashboard.
// The dashboard provides monitoring and management interface for the build cluster.
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

	"github.com/slchris/portage-engine/internal/dashboard"
	"github.com/slchris/portage-engine/pkg/config"
)

var (
	configPath = flag.String("config", "configs/dashboard.yaml", "Path to configuration file")
	port       = flag.Int("port", 8081, "Dashboard port")
)

func main() {
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadDashboardConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Override port if specified
	if *port != 8081 {
		cfg.Port = *port
	}

	// Create dashboard instance
	dash := dashboard.New(cfg)

	// HTTP server configuration
	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      dash.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Starting Portage Engine Dashboard on port %d", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Dashboard failed to start: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down dashboard...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Dashboard forced to shutdown: %v", err)
		log.Printf("Dashboard forced to shutdown: %v", err)
		return
	}

	log.Println("Dashboard exited")
}
