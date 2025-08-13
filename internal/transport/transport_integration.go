package transport

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ajayalluri/distributed-kv-store/internal/raft"
)

// TransportIntegration provides integration between Raft nodes and gRPC transport
type TransportIntegration struct {
	mu         sync.RWMutex
	transports map[string]*GRPCTransport
	raftNodes  map[string]*raft.RaftNode
	logger     *logrus.Logger
}

// NewTransportIntegration creates a new transport integration manager
func NewTransportIntegration(logger *logrus.Logger) *TransportIntegration {
	return &TransportIntegration{
		transports: make(map[string]*GRPCTransport),
		raftNodes:  make(map[string]*raft.RaftNode),
		logger:     logger,
	}
}

// CreateNode creates a new Raft node with gRPC transport
func (ti *TransportIntegration) CreateNode(nodeID, address string, peers map[string]string) (*raft.RaftNode, error) {
	ti.mu.Lock()
	defer ti.mu.Unlock()

	// Create storage for the node
	storage := raft.NewMemoryStorage()

	// Create transport
	transport := NewGRPCTransport(address, ti.logger)

	// Create apply channel
	applyCh := make(chan raft.ApplyMsg, 100)

	// Create Raft configuration
	config := raft.Config{
		NodeID:           nodeID,
		Address:          address,
		Peers:            peers,
		ElectionTimeout:  raft.DefaultElectionTimeoutMin,
		HeartbeatTimeout: raft.DefaultHeartbeatTimeout,
		Storage:          storage,
		ApplyCh:          applyCh,
		Transport:        transport,
	}

	// Create Raft node
	node := raft.NewRaftNode(config)

	// Store references
	ti.transports[nodeID] = transport
	ti.raftNodes[nodeID] = node

	ti.logger.WithFields(logrus.Fields{
		"node_id": nodeID,
		"address": address,
		"peers":   len(peers),
	}).Info("Created Raft node with gRPC transport")

	return node, nil
}

// StartNode starts both the transport and Raft node
func (ti *TransportIntegration) StartNode(nodeID string) error {
	ti.mu.RLock()
	transport := ti.transports[nodeID]
	node := ti.raftNodes[nodeID]
	ti.mu.RUnlock()

	if transport == nil || node == nil {
		return raft.ErrNodeNotFound
	}

	// Start transport
	if err := transport.Start(); err != nil {
		return err
	}

	// Start Raft node
	node.Start()

	// Start integration between transport and Raft
	go ti.integrateTransportWithRaft(nodeID, transport, node)

	ti.logger.WithField("node_id", nodeID).Info("Started Raft node and transport")
	return nil
}

// StopNode stops both the transport and Raft node
func (ti *TransportIntegration) StopNode(nodeID string) error {
	ti.mu.RLock()
	transport := ti.transports[nodeID]
	node := ti.raftNodes[nodeID]
	ti.mu.RUnlock()

	if transport == nil || node == nil {
		return raft.ErrNodeNotFound
	}

	// Stop Raft node
	node.Stop()

	// Stop transport
	if err := transport.Stop(); err != nil {
		return err
	}

	ti.logger.WithField("node_id", nodeID).Info("Stopped Raft node and transport")
	return nil
}

// GetNode returns the Raft node for the given ID
func (ti *TransportIntegration) GetNode(nodeID string) *raft.RaftNode {
	ti.mu.RLock()
	defer ti.mu.RUnlock()
	return ti.raftNodes[nodeID]
}

// integrateTransportWithRaft handles the integration between transport and Raft node
func (ti *TransportIntegration) integrateTransportWithRaft(nodeID string, transport *GRPCTransport, node *raft.RaftNode) {
	for {
		select {
		case voteReq := <-transport.GetVoteRequestChan():
			// Forward vote request to Raft node
			node.VoteRequestCh <- voteReq

		case appendReq := <-transport.GetAppendEntriesChan():
			// Forward append entries request to Raft node
			node.AppendEntriesCh <- appendReq

		case <-node.ShutdownCh:
			ti.logger.WithField("node_id", nodeID).Debug("Transport integration shutting down")
			return
		}
	}
}

// WaitForLeader waits for a leader to be elected in the cluster
func (ti *TransportIntegration) WaitForLeader(timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", raft.ErrElectionTimeout

		case <-ticker.C:
			ti.mu.RLock()
			for nodeID, node := range ti.raftNodes {
				if _, isLeader := node.GetState(); isLeader {
					ti.mu.RUnlock()
					return nodeID, nil
				}
			}
			ti.mu.RUnlock()
		}
	}
}

// GetClusterState returns the current state of all nodes in the cluster
func (ti *TransportIntegration) GetClusterState() map[string]raft.NodeState {
	ti.mu.RLock()
	defer ti.mu.RUnlock()

	state := make(map[string]raft.NodeState)
	for nodeID, node := range ti.raftNodes {
		node.GetMu().RLock()
		state[nodeID] = node.State
		node.GetMu().RUnlock()
	}
	return state
}