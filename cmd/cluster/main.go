package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ajayalluri/distributed-kv-store/internal/api"
	"github.com/ajayalluri/distributed-kv-store/internal/cluster"
	"github.com/ajayalluri/distributed-kv-store/internal/transport"
)

var (
	configPath = flag.String("config", "config/multi-node-cluster.yaml", "Path to cluster configuration file")
	nodeID     = flag.String("node", "", "Node ID (overrides RAFT_NODE_ID env var)")
	httpPort   = flag.Int("port", 8080, "HTTP server port")
	version    = "1.0.0"
)

func main() {
	flag.Parse()

	// Initialize logger
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.JSONFormatter{})

	logger.WithFields(logrus.Fields{
		"version": version,
	}).Info("Starting distributed key-value store cluster node")

	// Load cluster configuration
	clusterConfig, err := cluster.LoadClusterConfig(*configPath, logger)
	if err != nil {
		logger.WithError(err).Fatal("Failed to load cluster configuration")
	}

	// Determine node ID
	nodeIDToUse := determineNodeID(*nodeID, clusterConfig, logger)
	if nodeIDToUse == "" {
		logger.Fatal("Could not determine node ID")
	}

	// Get node address from cluster config
	nodeAddress, exists := clusterConfig.Nodes[nodeIDToUse]
	if !exists {
		logger.WithField("node_id", nodeIDToUse).Fatal("Node not found in cluster configuration")
	}

	logger.WithFields(logrus.Fields{
		"node_id":      nodeIDToUse,
		"raft_address": nodeAddress,
		"http_port":    *httpPort,
		"cluster_size": len(clusterConfig.Nodes),
	}).Info("Initializing cluster node")

	// Create data directories
	if err := cluster.CreateDataDirectories(clusterConfig); err != nil {
		logger.WithError(err).Fatal("Failed to create data directories")
	}

	// Create cluster manager
	manager := cluster.NewManager(*clusterConfig, nodeIDToUse)

	// Start the cluster manager (this starts Raft)
	if err := manager.Start(); err != nil {
		logger.WithError(err).Fatal("Failed to start cluster manager")
	}

	// Create cluster-aware HTTP server
	clusterServer := api.NewClusterServer(manager, logger)
	
	httpAddress := fmt.Sprintf(":%d", *httpPort)
	httpServer := &http.Server{
		Addr:         httpAddress,
		Handler:      clusterServer,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start HTTP server in a goroutine
	go func() {
		logger.WithField("address", httpAddress).Info("Starting HTTP server")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("HTTP server failed")
		}
	}()

	// Wait for leadership to be established
	logger.Info("Waiting for cluster leadership to be established...")
	go func() {
		for {
			time.Sleep(2 * time.Second)
			if leader := manager.GetLeader(); leader != nil {
				logger.WithFields(logrus.Fields{
					"leader_id":   leader.ID,
					"leader_addr": leader.Address,
					"is_local":    leader.ID == nodeIDToUse,
				}).Info("Cluster leadership established")
				break
			}
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down cluster node...")

	// Create a deadline for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.WithError(err).Error("HTTP server forced to shutdown")
	} else {
		logger.Info("HTTP server stopped gracefully")
	}

	// Stop cluster manager
	if err := manager.Stop(); err != nil {
		logger.WithError(err).Error("Error stopping cluster manager")
	} else {
		logger.Info("Cluster manager stopped gracefully")
	}

	logger.Info("Cluster node shutdown complete")
}

// determineNodeID determines which node ID to use
func determineNodeID(flagNodeID string, config *cluster.ClusterConfig, logger *logrus.Logger) string {
	// Command line flag takes priority
	if flagNodeID != "" {
		if _, exists := config.Nodes[flagNodeID]; exists {
			logger.WithField("source", "flag").WithField("node_id", flagNodeID).Info("Using node ID from command line flag")
			return flagNodeID
		}
		logger.WithField("node_id", flagNodeID).Warn("Node ID from flag not found in cluster configuration")
	}

	// Try environment variable
	if envNodeID := os.Getenv("RAFT_NODE_ID"); envNodeID != "" {
		if _, exists := config.Nodes[envNodeID]; exists {
			logger.WithField("source", "env").WithField("node_id", envNodeID).Info("Using node ID from environment variable")
			return envNodeID
		}
		logger.WithField("node_id", envNodeID).Warn("Node ID from environment not found in cluster configuration")
	}

	// Auto-detect based on available ports (development mode)
	logger.Info("No node ID specified, attempting auto-detection...")
	for nodeID, address := range config.Nodes {
		if isPortAvailable(address, logger) {
			logger.WithField("node_id", nodeID).WithField("address", address).Info("Auto-detected available node")
			return nodeID
		}
	}

	logger.Error("Could not auto-detect node ID. Please set RAFT_NODE_ID environment variable or use -node flag")
	return ""
}

// isPortAvailable checks if a port is available for binding
func isPortAvailable(address string, logger *logrus.Logger) bool {
	// Extract port from address
	host, portStr, err := parseAddress(address)
	if err != nil {
		logger.WithError(err).WithField("address", address).Debug("Failed to parse address")
		return false
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		logger.WithError(err).WithField("port", portStr).Debug("Failed to parse port")
		return false
	}

	// Try to create a gRPC transport to see if the port is available
	testTransport := transport.NewGRPCTransport(fmt.Sprintf("%s:%d", host, port), logger)
	err = testTransport.Start()
	if err != nil {
		logger.WithError(err).WithField("address", address).Debug("Port not available")
		return false
	}

	// Clean up
	testTransport.Stop()
	return true
}

// parseAddress parses an address into host and port
func parseAddress(address string) (host, port string, err error) {
	// Simple parsing for host:port format
	for i := len(address) - 1; i >= 0; i-- {
		if address[i] == ':' {
			return address[:i], address[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid address format: %s", address)
}