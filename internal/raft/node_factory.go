package raft

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// NodeConfig contains all configuration needed to create a Raft node
type NodeConfig struct {
	// Node identification
	NodeID  string
	Address string
	Peers   map[string]string // Node ID -> Address mapping
	
	// Raft configuration
	ElectionTimeout  time.Duration
	HeartbeatTimeout time.Duration
	
	// Storage configuration
	DataDir string
	
	// Network configuration
	Logger *logrus.Logger
}

// CreateRaftNode creates a complete Raft node with KV store integration
func CreateRaftNode(config NodeConfig, transport RPCTransport) (*RaftNode, *RaftKVStore, error) {
	// Validate configuration
	if err := validateNodeConfig(config); err != nil {
		return nil, nil, fmt.Errorf("invalid node configuration: %w", err)
	}
	
	// Create storage for Raft state
	storage, err := NewFileStorage(config.DataDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create storage: %w", err)
	}
	
	// Create apply channel
	applyCh := make(chan ApplyMsg, 100)
	
	// Create Raft configuration
	raftConfig := Config{
		NodeID:           config.NodeID,
		Address:          config.Address,
		Peers:            config.Peers,
		ElectionTimeout:  config.ElectionTimeout,
		HeartbeatTimeout: config.HeartbeatTimeout,
		Storage:          storage,
		ApplyCh:          applyCh,
		Transport:        transport,
	}
	
	// Create Raft node
	raftNode := NewRaftNode(raftConfig)
	
	// Create KV store
	kvStore := NewRaftKVStore(raftNode, config.Logger)
	
	config.Logger.WithFields(logrus.Fields{
		"node_id": config.NodeID,
		"address": config.Address,
		"peers":   len(config.Peers),
	}).Info("Created Raft node with KV store")
	
	return raftNode, kvStore, nil
}

// StartRaftNode starts all components of a Raft node
func StartRaftNode(raftNode *RaftNode, transport RPCTransport) error {
	// Start transport first
	if err := transport.Start(); err != nil {
		return fmt.Errorf("failed to start transport: %w", err)
	}
	
	// Start Raft node
	raftNode.Start()
	
	return nil
}

// StopRaftNode stops all components of a Raft node
func StopRaftNode(raftNode *RaftNode, kvStore *RaftKVStore, transport RPCTransport) error {
	var lastErr error
	
	// Stop KV store
	if err := kvStore.Close(); err != nil {
		lastErr = err
	}
	
	// Stop Raft node
	raftNode.Stop()
	
	// Stop transport
	if err := transport.Stop(); err != nil {
		lastErr = err
	}
	
	return lastErr
}

// validateNodeConfig validates the node configuration
func validateNodeConfig(config NodeConfig) error {
	if config.NodeID == "" {
		return fmt.Errorf("node ID cannot be empty")
	}
	
	if config.Address == "" {
		return fmt.Errorf("address cannot be empty")
	}
	
	if config.DataDir == "" {
		return fmt.Errorf("data directory cannot be empty")
	}
	
	if config.ElectionTimeout <= 0 {
		return fmt.Errorf("election timeout must be positive")
	}
	
	if config.HeartbeatTimeout <= 0 {
		return fmt.Errorf("heartbeat timeout must be positive")
	}
	
	if config.HeartbeatTimeout >= config.ElectionTimeout {
		return fmt.Errorf("heartbeat timeout must be less than election timeout")
	}
	
	if config.Logger == nil {
		return fmt.Errorf("logger cannot be nil")
	}
	
	// Check that this node is not in its own peers list
	if _, exists := config.Peers[config.NodeID]; exists {
		return fmt.Errorf("node cannot be in its own peers list")
	}
	
	return nil
}

// DefaultNodeConfig returns a default configuration for a Raft node
func DefaultNodeConfig(nodeID, address string, peers map[string]string, logger *logrus.Logger) NodeConfig {
	return NodeConfig{
		NodeID:           nodeID,
		Address:          address,
		Peers:            peers,
		ElectionTimeout:  DefaultElectionTimeoutMin + 50*time.Millisecond,
		HeartbeatTimeout: DefaultHeartbeatTimeout,
		DataDir:          fmt.Sprintf("./data/raft-%s", nodeID),
		Logger:           logger,
	}
}