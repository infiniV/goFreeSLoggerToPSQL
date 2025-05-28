package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gofreeswitchesl/api"
	"gofreeswitchesl/config"
	"gofreeswitchesl/esl"
	"gofreeswitchesl/store"
	"gofreeswitchesl/utils"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)

func main() {
	// Initialize logger
	logger := utils.NewLogger()
	logger.Info("Application starting...")

	// Load configuration
	cfg := config.LoadConfig()
	logger.WithFields(logrus.Fields{
		"esl_addr": cfg.ESLAddr,
		"api_port": cfg.APIPort,
		// Avoid logging sensitive info like passwords or full DSNs in production
	}).Info("Configuration loaded")

	// Create root context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure all resources are cleaned up

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize Database Connection
	dbPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Fatalf("Unable to connect to database: %v\n", err)
	}
	defer dbPool.Close()

	logger.Info("Successfully connected to PostgreSQL database.")

	// Initialize Store
	appStore := store.NewStore(dbPool, logger)

	// Initialize database schema (idempotent)
	if err := appStore.InitSchema(ctx); err != nil {
		logger.Fatalf("Failed to initialize database schema: %v", err)
	}

	// Initialize ESL Client
	eslClient := esl.NewClient(cfg.ESLAddr, cfg.ESLPass, appStore, logger)
	if err := eslClient.Start(ctx); err != nil {
		// Log non-fatal error, as ESL client has internal retry logic
		logger.WithError(err).Error("ESL client failed to start initially, will attempt reconnection in background.")
	}

	// Initialize API Server
	apiServer := api.NewServer(appStore, logger)
	apiAddr := fmt.Sprintf(":%s", cfg.APIPort)

	httpServer := &http.Server{
		Addr:         apiAddr,
		Handler:      apiServer.GetRouter(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start API server in a goroutine
	go func() {
		logger.Infof("API server listening on %s", apiAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Could not listen on %s: %v\n", apiAddr, err)
		}
		logger.Info("API server stopped.")
	}()

	// Wait for shutdown signal
	<-sigChan
	logger.Info("Received shutdown signal. Initiating graceful shutdown...")

	// Trigger context cancellation for all components
	cancel()

	// Gracefully shutdown HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second) // Increased timeout
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Error("API server shutdown error")
	}

	// Close ESL client connection
	if err := eslClient.Close(); err != nil {
		logger.WithError(err).Error("ESL client close error")
	}

	// Database pool is closed by defer dbPool.Close()

	logger.Info("Application shut down gracefully.")
}
