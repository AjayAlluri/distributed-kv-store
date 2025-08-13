package cluster

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// ClusterConfigFile represents the YAML structure for cluster configuration
type ClusterConfigFile struct {
	Cluster struct {
		Name        string            `yaml:"name"`
		BootstrapID string            `yaml:"bootstrap_id"`
		DataDir     string            `yaml:"data_dir"`
		Nodes       map[string]string `yaml:"nodes"`
	} `yaml:"cluster"`
	
	Raft struct {
		ElectionTimeoutMs  int `yaml:"election_timeout_ms"`
		HeartbeatTimeoutMs int `yaml:"heartbeat_timeout_ms"`
	} `yaml:"raft"`
	
	Logging struct {
		Level string `yaml:"level"`
	} `yaml:"logging"`
}

// LoadClusterConfig loads cluster configuration from a YAML file
func LoadClusterConfig(configPath string, logger *logrus.Logger) (*ClusterConfig, error) {
	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	
	// Parse YAML
	var config ClusterConfigFile
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}
	
	// Validate configuration
	if err := validateClusterConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid cluster configuration: %w", err)
	}
	
	// Convert to internal format
	clusterConfig := &ClusterConfig{
		Nodes:       config.Cluster.Nodes,
		BootstrapID: config.Cluster.BootstrapID,
		DataDir:     config.Cluster.DataDir,
		Logger:      logger,
	}
	
	logger.WithFields(logrus.Fields{
		"cluster_name": config.Cluster.Name,
		"nodes":        len(config.Cluster.Nodes),
		"bootstrap_id": config.Cluster.BootstrapID,
		"data_dir":     config.Cluster.DataDir,
	}).Info("Loaded cluster configuration")
	
	return clusterConfig, nil
}

// validateClusterConfig validates the cluster configuration
func validateClusterConfig(config *ClusterConfigFile) error {
	if config.Cluster.Name == "" {
		return fmt.Errorf("cluster name cannot be empty")
	}
	
	if len(config.Cluster.Nodes) == 0 {
		return fmt.Errorf("cluster must have at least one node")
	}
	
	if config.Cluster.BootstrapID == "" {
		return fmt.Errorf("bootstrap_id cannot be empty")
	}
	
	// Check that bootstrap node exists in the nodes list
	if _, exists := config.Cluster.Nodes[config.Cluster.BootstrapID]; !exists {
		return fmt.Errorf("bootstrap node %s not found in nodes list", config.Cluster.BootstrapID)
	}
	
	if config.Cluster.DataDir == "" {
		return fmt.Errorf("data_dir cannot be empty")
	}
	
	// Validate node addresses
	for nodeID, address := range config.Cluster.Nodes {
		if nodeID == "" {
			return fmt.Errorf("node ID cannot be empty")
		}
		if address == "" {
			return fmt.Errorf("node address cannot be empty for node %s", nodeID)
		}
	}
	
	// Check for odd number of nodes (recommended for Raft)
	if len(config.Cluster.Nodes)%2 == 0 {
		// This is a warning, not an error
		logrus.Warn("Even number of nodes detected - odd numbers are recommended for Raft clusters")
	}
	
	return nil
}

// CreateExampleConfig creates an example cluster configuration file
func CreateExampleConfig(path string) error {
	config := ClusterConfigFile{
		Cluster: struct {
			Name        string            `yaml:"name"`
			BootstrapID string            `yaml:"bootstrap_id"`
			DataDir     string            `yaml:"data_dir"`
			Nodes       map[string]string `yaml:"nodes"`
		}{
			Name:        "distributed-kv-cluster",
			BootstrapID: "node-1",
			DataDir:     "./data/cluster",
			Nodes: map[string]string{
				"node-1": "localhost:9001",
				"node-2": "localhost:9002",
				"node-3": "localhost:9003",
			},
		},
		Raft: struct {
			ElectionTimeoutMs  int `yaml:"election_timeout_ms"`
			HeartbeatTimeoutMs int `yaml:"heartbeat_timeout_ms"`
		}{
			ElectionTimeoutMs:  200,
			HeartbeatTimeoutMs: 50,
		},
		Logging: struct {
			Level string `yaml:"level"`
		}{
			Level: "info",
		},
	}
	
	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	
	// Marshal to YAML
	data, err := yaml.Marshal(&config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	
	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	
	return nil
}

// GetNodeFromEnv gets the node ID from environment variable or command line
func GetNodeFromEnv(configuredNodes map[string]string) (string, error) {
	// Try environment variable first
	if nodeID := os.Getenv("RAFT_NODE_ID"); nodeID != "" {
		if _, exists := configuredNodes[nodeID]; exists {
			return nodeID, nil
		}
		return "", fmt.Errorf("node ID %s from environment not found in cluster configuration", nodeID)
	}
	
	// If no environment variable, require it to be set
	return "", fmt.Errorf("RAFT_NODE_ID environment variable must be set")
}

// CreateDataDirectories creates data directories for all nodes
func CreateDataDirectories(config *ClusterConfig) error {
	for nodeID := range config.Nodes {
		nodeDataDir := filepath.Join(config.DataDir, nodeID)
		if err := os.MkdirAll(nodeDataDir, 0755); err != nil {
			return fmt.Errorf("failed to create data directory for node %s: %w", nodeID, err)
		}
	}
	return nil
}