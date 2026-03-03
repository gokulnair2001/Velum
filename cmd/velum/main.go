package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/velum/internal/api"
	"github.com/velum/internal/config"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize structured logger
	var logLevel slog.Level
	if cfg.Server.Environment == "development" {
		logLevel = slog.LevelDebug
	} else {
		logLevel = slog.LevelInfo
	}

	var handler slog.Handler
	if cfg.Server.Environment == "production" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	}
	slog.SetDefault(slog.New(handler))

	// Parse timeouts
	readTimeout, err := time.ParseDuration(cfg.Server.ReadTimeout)
	if err != nil {
		slog.Warn("invalid read_timeout, using default", "value", cfg.Server.ReadTimeout, "default", "10s")
		readTimeout = 10 * time.Second
	}

	writeTimeout, err := time.ParseDuration(cfg.Server.WriteTimeout)
	if err != nil {
		slog.Warn("invalid write_timeout, using default", "value", cfg.Server.WriteTimeout, "default", "30s")
		writeTimeout = 30 * time.Second
	}

	idleTimeout, err := time.ParseDuration(cfg.Server.IdleTimeout)
	if err != nil {
		slog.Warn("invalid idle_timeout, using default", "value", cfg.Server.IdleTimeout, "default", "60s")
		idleTimeout = 60 * time.Second
	}

	shutdownTimeout, err := time.ParseDuration(cfg.Server.ShutdownTimeout)
	if err != nil {
		slog.Warn("invalid shutdown_timeout, using default", "value", cfg.Server.ShutdownTimeout, "default", "15s")
		shutdownTimeout = 15 * time.Second
	}

	// Initialize the API server
	apiServer := api.NewServer(cfg)

	// Create HTTP server with timeouts
	addr := cfg.Server.Host + ":" + cfg.Server.Port
	srv := &http.Server{
		Addr:         addr,
		Handler:      apiServer.Router(),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	// Channel to listen for interrupt signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		slog.Info("velum server starting", "addr", addr, "env", cfg.Server.Environment, "read_timeout", readTimeout, "write_timeout", writeTimeout, "idle_timeout", idleTimeout)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("failed to start server", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	<-stop
	slog.Info("shutting down gracefully...")

	// Create context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(ctx); err != nil {
		slog.Warn("forced shutdown", "error", err)
	} else {
		slog.Info("server stopped gracefully")
	}

	// Close storage connections and stop background goroutines
	if err := apiServer.Shutdown(); err != nil {
		slog.Warn("resource cleanup error", "error", err)
	} else {
		slog.Info("resources released")
	}
}
