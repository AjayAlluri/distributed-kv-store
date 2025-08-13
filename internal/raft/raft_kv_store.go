package raft

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// RaftKVStore implements a distributed key-value store using Raft consensus
type RaftKVStore struct {
	mu          sync.RWMutex
	raftNode    *RaftNode
	stateMachine *KVStateMachine
	applyCh     chan ApplyMsg
	
	// Configuration
	applyTimeout time.Duration
	logger       *logrus.Logger
	
	// State
	closed bool
	stopCh chan struct{}
}

// NewRaftKVStore creates a new Raft-based key-value store
func NewRaftKVStore(raftNode *RaftNode, logger *logrus.Logger) *RaftKVStore {
	stateMachine := NewKVStateMachine(logger)
	
	store := &RaftKVStore{
		raftNode:     raftNode,
		stateMachine: stateMachine,
		applyCh:      raftNode.ApplyCh,
		applyTimeout: 5 * time.Second,
		logger:       logger,
		stopCh:       make(chan struct{}),
	}
	
	// Start processing applied commands
	go store.processAppliedCommands()
	
	return store
}

// Get retrieves a value by key (read-only, no consensus needed)
func (rks *RaftKVStore) Get(key string) (string, error) {
	rks.mu.RLock()
	defer rks.mu.RUnlock()
	
	if rks.closed {
		return "", fmt.Errorf("store is closed")
	}
	
	if key == "" {
		return "", fmt.Errorf("key cannot be empty")
	}
	
	value, exists := rks.stateMachine.Get(key)
	if !exists {
		return "", fmt.Errorf("key not found")
	}
	
	rks.logger.WithFields(logrus.Fields{
		"key":   key,
		"value": value,
	}).Debug("Retrieved value")
	
	return value, nil
}

// Put stores a key-value pair (requires consensus)
func (rks *RaftKVStore) Put(key, value string) error {
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}
	
	cmd := &KVCommand{
		Type:  CommandPut,
		Key:   key,
		Value: value,
	}
	
	return rks.submitCommand(cmd)
}

// Delete removes a key-value pair (requires consensus)
func (rks *RaftKVStore) Delete(key string) error {
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}
	
	cmd := &KVCommand{
		Type: CommandDelete,
		Key:  key,
	}
	
	return rks.submitCommand(cmd)
}

// submitCommand submits a command to the Raft cluster and waits for it to be applied
func (rks *RaftKVStore) submitCommand(cmd *KVCommand) error {
	rks.mu.RLock()
	if rks.closed {
		rks.mu.RUnlock()
		return fmt.Errorf("store is closed")
	}
	rks.mu.RUnlock()
	
	// Serialize the command
	cmdData, err := SerializeCommand(cmd)
	if err != nil {
		return fmt.Errorf("failed to serialize command: %w", err)
	}
	
	rks.logger.WithFields(logrus.Fields{
		"command": cmd.String(),
	}).Debug("Submitting command to Raft")
	
	// Submit to Raft
	index, term, isLeader := rks.raftNode.Submit(cmdData)
	if !isLeader {
		return fmt.Errorf("not leader, cannot accept writes")
	}
	
	rks.logger.WithFields(logrus.Fields{
		"command": cmd.String(),
		"index":   index,
		"term":    term,
	}).Debug("Command submitted to Raft")
	
	// Wait for the command to be applied
	ctx, cancel := context.WithTimeout(context.Background(), rks.applyTimeout)
	defer cancel()
	
	return rks.waitForApply(ctx, index, cmd)
}

// waitForApply waits for a command at the given index to be applied
func (rks *RaftKVStore) waitForApply(ctx context.Context, expectedIndex uint64, expectedCmd *KVCommand) error {
	// Create a channel to receive notifications when our command is applied
	notifyCh := make(chan error, 1)
	
	// Check if the command might already be applied
	rks.mu.RLock()
	currentIndex := rks.raftNode.LastApplied
	rks.mu.RUnlock()
	
	if currentIndex >= expectedIndex {
		// The command might already be applied, but we can't be sure it's our command
		// In a full implementation, we'd need request IDs to track specific commands
		rks.logger.WithFields(logrus.Fields{
			"expected_index": expectedIndex,
			"current_index":  currentIndex,
		}).Debug("Command may already be applied")
		return nil
	}
	
	// Set up a goroutine to wait for the apply
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				notifyCh <- fmt.Errorf("timeout waiting for command to be applied")
				return
				
			case <-ticker.C:
				rks.mu.RLock()
				lastApplied := rks.raftNode.LastApplied
				rks.mu.RUnlock()
				
				if lastApplied >= expectedIndex {
					notifyCh <- nil
					return
				}
				
			case <-rks.stopCh:
				notifyCh <- fmt.Errorf("store is shutting down")
				return
			}
		}
	}()
	
	return <-notifyCh
}

// processAppliedCommands processes commands that have been applied by Raft
func (rks *RaftKVStore) processAppliedCommands() {
	for {
		select {
		case applyMsg := <-rks.applyCh:
			if applyMsg.CommandValid {
				rks.handleAppliedCommand(applyMsg)
			} else if applyMsg.SnapshotValid {
				rks.handleSnapshot(applyMsg)
			}
			
		case <-rks.stopCh:
			rks.logger.Debug("Stopping applied command processor")
			return
		}
	}
}

// handleAppliedCommand handles a command that has been applied by Raft
func (rks *RaftKVStore) handleAppliedCommand(applyMsg ApplyMsg) {
	rks.logger.WithFields(logrus.Fields{
		"index": applyMsg.CommandIndex,
	}).Debug("Processing applied command")
	
	// Apply the command to the state machine
	result, err := rks.stateMachine.Apply(applyMsg.Command)
	if err != nil {
		rks.logger.WithFields(logrus.Fields{
			"index": applyMsg.CommandIndex,
			"error": err,
		}).Error("Failed to apply command to state machine")
		return
	}
	
	rks.logger.WithFields(logrus.Fields{
		"index":  applyMsg.CommandIndex,
		"result": result,
	}).Debug("Successfully applied command to state machine")
}

// handleSnapshot handles a snapshot that has been applied by Raft
func (rks *RaftKVStore) handleSnapshot(applyMsg ApplyMsg) {
	rks.logger.WithFields(logrus.Fields{
		"snapshot_index": applyMsg.SnapshotIndex,
		"snapshot_term":  applyMsg.SnapshotTerm,
	}).Info("Applying snapshot")
	
	if err := rks.stateMachine.Restore(applyMsg.Snapshot); err != nil {
		rks.logger.WithError(err).Error("Failed to restore from snapshot")
		return
	}
	
	rks.logger.Info("Successfully restored from snapshot")
}

// Close closes the store and stops processing
func (rks *RaftKVStore) Close() error {
	rks.mu.Lock()
	defer rks.mu.Unlock()
	
	if rks.closed {
		return nil
	}
	
	rks.logger.Info("Closing Raft KV store")
	rks.closed = true
	close(rks.stopCh)
	
	return nil
}

// IsLeader returns true if this node is the current Raft leader
func (rks *RaftKVStore) IsLeader() bool {
	_, isLeader := rks.raftNode.GetState()
	return isLeader
}

// GetLeader returns the current leader's ID
func (rks *RaftKVStore) GetLeader() string {
	return rks.raftNode.GetLeader()
}

// GetAll returns all key-value pairs (read-only)
func (rks *RaftKVStore) GetAll() map[string]string {
	return rks.stateMachine.GetAll()
}

// Stats returns statistics about the store
func (rks *RaftKVStore) Stats() map[string]interface{} {
	term, isLeader := rks.raftNode.GetState()
	
	return map[string]interface{}{
		"node_id":        rks.raftNode.ID,
		"is_leader":      isLeader,
		"current_term":   term,
		"leader_id":      rks.GetLeader(),
		"last_applied":   rks.raftNode.LastApplied,
		"commit_index":   rks.raftNode.CommitIndex,
		"kv_store_size":  rks.stateMachine.Size(),
		"log_size":       len(rks.raftNode.Log),
	}
}