package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ajayalluri/distributed-kv-store/internal/api"
	"github.com/ajayalluri/distributed-kv-store/internal/config"
	"github.com/ajayalluri/distributed-kv-store/internal/logging"
	"github.com/ajayalluri/distributed-kv-store/internal/storage"
	"github.com/sirupsen/logrus"
)

var (
	configPath = flag.String("config", "", "Path to configuration file")
	version    = "1.0.0"
	buildTime  = "unknown"
	gitCommit  = "unknown"
)

// initializeStorage creates the appropriate storage implementation based on configuration
func initializeStorage(cfg *config.Config, logger *logging.Logger) (api.KVStore, error) {
	switch cfg.Storage.Type {
	case "file":
		dataPath, err := cfg.GetDataPath()
		if err != nil {
			return nil, fmt.Errorf("failed to get data path: %w", err)
		}
		
		store, err := storage.NewBoltStore(dataPath)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize BoltDB storage: %w", err)
		}
		
		logger.WithField("storage_type", "file").
			WithField("data_path", dataPath).
			Info("File-based storage initialized successfully")
		
		return store, nil
		
	case "database":
		pgConfig := cfg.GetPostgreSQLConfig()
		
		store, err := storage.NewPostgreSQLStore(pgConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize PostgreSQL storage: %w", err)
		}
		
		logger.WithField("storage_type", "database").
			WithFields(logrus.Fields{
				"db_host":     pgConfig.Host,
				"db_port":     pgConfig.Port,
				"db_name":     pgConfig.Database,
				"db_user":     pgConfig.Username,
				"max_conns":   pgConfig.MaxConns,
				"min_conns":   pgConfig.MinConns,
			}).Info("PostgreSQL storage initialized successfully")
		
		return store, nil
		
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Storage.Type)
	}
}

func main() {
	flag.Parse()

	// Load configuration
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger, err := logging.NewLogger(logging.Config{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
		File:   cfg.Logging.File,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	logger.WithFields(map[string]interface{}{
		"version":    version,
		"build_time": buildTime,
		"git_commit": gitCommit,
		"node_id":    cfg.Raft.NodeID,
	}).Info("Starting distributed key-value store")

	// Initialize storage
	store, err := initializeStorage(cfg, logger)
	if err != nil {
		logger.WithError(err).Fatal("Failed to initialize storage")
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.WithError(err).Error("Failed to close storage")
		}
	}()

	// Initialize HTTP server
	server := api.NewServer(store)
	
	httpServer := &http.Server{
		Addr:         cfg.GetServerAddress(),
		Handler:      server,
		ReadTimeout:  parseDuration(cfg.Server.ReadTimeout, 10*time.Second),
		WriteTimeout: parseDuration(cfg.Server.WriteTimeout, 10*time.Second),
	}

	// Start server in a goroutine
	go func() {
		logger.WithField("address", cfg.GetServerAddress()).Info("Starting HTTP server")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("HTTP server failed")
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Create a deadline for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.WithError(err).Error("HTTP server forced to shutdown")
	} else {
		logger.Info("HTTP server stopped gracefully")
	}

	logger.Info("Server shutdown complete")
}

func loadConfig(configPath string) (*config.Config, error) {
	if configPath != "" {
		return config.LoadConfig(configPath)
	}

	// Try to load from environment variables first
	if cfg, err := config.LoadConfigFromEnv(); err == nil {
		return cfg, nil
	}

	// Fall back to default configuration
	return config.DefaultConfig(), nil
}

func parseDuration(s string, defaultDuration time.Duration) time.Duration {
	if s == "" {
		return defaultDuration
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultDuration
	}

	return d
}