package cluster

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ajayalluri/distributed-kv-store/internal/raft"
	"github.com/ajayalluri/distributed-kv-store/internal/transport"
)

// NodeInfo contains information about a cluster node
type NodeInfo struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	IsLeader bool   `json:"is_leader"`
	State    string `json:"state"`
	Term     uint64 `json:"term"`
}

// ClusterConfig contains configuration for the entire cluster
type ClusterConfig struct {
	Nodes            map[string]string `json:"nodes"`            // Node ID -> Address
	BootstrapID      string            `json:"bootstrap_id"`     // Which node starts the cluster
	DataDir          string            `json:"data_dir"`
	ElectionTimeout  time.Duration     `json:"election_timeout"` // Raft election timeout
	HeartbeatTimeout time.Duration     `json:"heartbeat_timeout"`// Raft heartbeat timeout
	Logger           *logrus.Logger    `json:"-"`
}

// Manager manages a cluster of Raft nodes
type Manager struct {
	mu sync.RWMutex
	
	// Configuration
	config ClusterConfig
	logger *logrus.Logger
	
	// Local node information
	localNodeID string
	localNode   *raft.RaftNode
	localKV     *raft.RaftKVStore
	transport   *transport.GRPCTransport
	
	// Cluster state
	nodes       map[string]*NodeInfo
	currentTerm uint64
	leaderID    string
	
	// Control
	started bool
	stopCh  chan struct{}
}

// NewManager creates a new cluster manager
func NewManager(config ClusterConfig, nodeID string) *Manager {
	return &Manager{
		config:      config,
		localNodeID: nodeID,
		logger:      config.Logger,
		nodes:       make(map[string]*NodeInfo),
		stopCh:      make(chan struct{}),
	}
}

// Start initializes and starts the local node
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.started {
		return fmt.Errorf("cluster manager already started")
	}
	
	// Validate that this node is in the cluster configuration
	localAddress, exists := m.config.Nodes[m.localNodeID]
	if !exists {
		return fmt.Errorf("local node %s not found in cluster configuration", m.localNodeID)
	}
	
	m.logger.WithFields(logrus.Fields{
		"node_id":      m.localNodeID,
		"address":      localAddress,
		"cluster_size": len(m.config.Nodes),
	}).Info("Starting cluster manager")
	
	// Create peers map (excluding self)
	peers := make(map[string]string)
	for nodeID, address := range m.config.Nodes {
		if nodeID != m.localNodeID {
			peers[nodeID] = address
		}
	}
	
	// Create transport
	m.transport = transport.NewGRPCTransport(localAddress, m.logger)
	
	// Create node configuration with optimized timing from cluster config
	nodeConfig := raft.NodeConfig{
		NodeID:           m.localNodeID,
		Address:          localAddress,
		Peers:            peers,
		ElectionTimeout:  m.config.ElectionTimeout,
		HeartbeatTimeout: m.config.HeartbeatTimeout,
		DataDir:          fmt.Sprintf("%s/%s", m.config.DataDir, m.localNodeID),
		Logger:           m.logger,
	}
	
	// Create Raft node and KV store
	var err error
	m.localNode, m.localKV, err = raft.CreateRaftNode(nodeConfig, m.transport)
	if err != nil {
		return fmt.Errorf("failed to create Raft node: %w", err)
	}
	
	// Start the node
	if err := raft.StartRaftNode(m.localNode, m.transport); err != nil {
		return fmt.Errorf("failed to start Raft node: %w", err)
	}
	
	// Initialize cluster state tracking
	m.initializeClusterState()
	
	// Start monitoring cluster state
	go m.monitorCluster()
	
	m.started = true
	
	m.logger.WithField("node_id", m.localNodeID).Info("Cluster manager started successfully")
	return nil
}

// Stop stops the local node and cluster manager
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if !m.started {
		return nil
	}
	
	m.logger.WithField("node_id", m.localNodeID).Info("Stopping cluster manager")
	
	// Signal stop
	close(m.stopCh)
	
	// Stop Raft components
	var lastErr error
	if err := raft.StopRaftNode(m.localNode, m.localKV, m.transport); err != nil {
		lastErr = err
		m.logger.WithError(err).Error("Error stopping Raft node")
	}
	
	m.started = false
	
	m.logger.WithField("node_id", m.localNodeID).Info("Cluster manager stopped")
	return lastErr
}

// GetKVStore returns the local KV store
func (m *Manager) GetKVStore() *raft.RaftKVStore {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.localKV
}

// GetLocalNode returns the local Raft node
func (m *Manager) GetLocalNode() *raft.RaftNode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.localNode
}

// IsLeader returns true if the local node is the current leader
func (m *Manager) IsLeader() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.localKV != nil && m.localKV.IsLeader()
}

// GetLeader returns the current leader's information
func (m *Manager) GetLeader() *NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if m.leaderID == "" {
		return nil
	}
	
	if node, exists := m.nodes[m.leaderID]; exists {
		return node
	}
	
	return nil
}

// GetClusterInfo returns information about all nodes in the cluster
func (m *Manager) GetClusterInfo() map[string]*NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	// Return a copy to prevent external modification
	result := make(map[string]*NodeInfo)
	for id, info := range m.nodes {
		result[id] = &NodeInfo{
			ID:       info.ID,
			Address:  info.Address,
			IsLeader: info.IsLeader,
			State:    info.State,
			Term:     info.Term,
		}
	}
	
	return result
}

// GetLeaderAddress returns the address of the current leader
func (m *Manager) GetLeaderAddress() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if m.leaderID == "" {
		return ""
	}
	
	if address, exists := m.config.Nodes[m.leaderID]; exists {
		return address
	}
	
	return ""
}

// GetLeaderHTTPAddress returns the HTTP address of the current leader
func (m *Manager) GetLeaderHTTPAddress() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if m.leaderID == "" {
		return ""
	}
	
	if raftAddress, exists := m.config.Nodes[m.leaderID]; exists {
		return m.convertRaftToHTTPAddress(raftAddress)
	}
	
	return ""
}

// convertRaftToHTTPAddress converts a Raft address to HTTP address using the port mapping
func (m *Manager) convertRaftToHTTPAddress(raftAddress string) string {
	// Simple mapping from Raft ports to HTTP ports
	// node-1: localhost:9001 -> localhost:8081
	// node-2: localhost:9002 -> localhost:8082  
	// node-3: localhost:9003 -> localhost:8083
	switch raftAddress {
	case "localhost:9001":
		return "localhost:8081"
	case "localhost:9002":
		return "localhost:8082"
	case "localhost:9003":
		return "localhost:8083"
	default:
		// Fallback: try to extract host and convert port
		if host, port, err := parseAddress(raftAddress); err == nil {
			if raftPort, err := strconv.Atoi(port); err == nil {
				httpPort := raftPort - 1000 // 9001->8001, 9002->8002, etc.
				return fmt.Sprintf("%s:%d", host, httpPort)
			}
		}
		return raftAddress // Return original if conversion fails
	}
}

// initializeClusterState initializes the cluster state tracking
func (m *Manager) initializeClusterState() {
	for nodeID, address := range m.config.Nodes {
		m.nodes[nodeID] = &NodeInfo{
			ID:       nodeID,
			Address:  address,
			IsLeader: false,
			State:    "Unknown",
			Term:     0,
		}
	}
}

// monitorCluster monitors the cluster state and updates leadership information
func (m *Manager) monitorCluster() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.stopCh:
			return
			
		case <-ticker.C:
			m.updateClusterState()
		}
	}
}

// updateClusterState updates the current cluster state
func (m *Manager) updateClusterState() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if !m.started || m.localNode == nil {
		return
	}
	
	// Update local node state
	term, isLeader := m.localNode.GetState()
	m.currentTerm = term
	
	localInfo := m.nodes[m.localNodeID]
	if localInfo != nil {
		localInfo.IsLeader = isLeader
		localInfo.Term = term
		localInfo.State = m.localNode.State.String()
		
		// Update leader tracking
		if isLeader {
			m.leaderID = m.localNodeID
			m.logger.WithFields(logrus.Fields{
				"node_id": m.localNodeID,
				"term":    term,
			}).Debug("Local node is leader")
		} else if m.leaderID == m.localNodeID {
			// We were leader but no longer are
			m.leaderID = ""
			m.logger.WithField("node_id", m.localNodeID).Debug("Local node is no longer leader")
		}
	}
	
	// Reset all nodes to not leader, then set the actual leader
	for _, node := range m.nodes {
		node.IsLeader = false
	}
	
	// Get current leader from Raft
	currentLeader := m.localNode.GetLeader()
	if currentLeader != "" && currentLeader != m.leaderID {
		m.leaderID = currentLeader
		if leaderInfo, exists := m.nodes[currentLeader]; exists {
			leaderInfo.IsLeader = true
		}
		
		m.logger.WithFields(logrus.Fields{
			"leader_id": currentLeader,
			"term":      term,
		}).Debug("Detected new leader")
	}
}

// parseAddress parses an address into host and port components
func parseAddress(address string) (string, string, error) {
	parts := strings.Split(address, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid address format: %s", address)
	}
	return parts[0], parts[1], nil
}