package raft

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"
)

// KVStateMachine implements the StateMachine interface for key-value operations
type KVStateMachine struct {
	mu    sync.RWMutex
	data  map[string]string
	logger *logrus.Logger
}

// NewKVStateMachine creates a new key-value state machine
func NewKVStateMachine(logger *logrus.Logger) *KVStateMachine {
	return &KVStateMachine{
		data:   make(map[string]string),
		logger: logger,
	}
}

// Apply applies a command to the state machine
func (sm *KVStateMachine) Apply(command interface{}) (interface{}, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Handle different command types
	switch cmd := command.(type) {
	case *KVCommand:
		return sm.applyKVCommand(cmd)
	case []byte:
		// Try to deserialize as KVCommand
		kvCmd, err := DeserializeCommand(cmd)
		if err != nil {
			sm.logger.WithError(err).Error("Failed to deserialize command")
			return nil, fmt.Errorf("failed to deserialize command: %w", err)
		}
		return sm.applyKVCommand(kvCmd)
	default:
		return nil, fmt.Errorf("unsupported command type: %T", command)
	}
}

// applyKVCommand applies a KV command to the state machine
func (sm *KVStateMachine) applyKVCommand(cmd *KVCommand) (interface{}, error) {
	if err := cmd.Validate(); err != nil {
		return nil, err
	}

	switch cmd.Type {
	case CommandPut:
		oldValue, existed := sm.data[cmd.Key]
		sm.data[cmd.Key] = cmd.Value
		
		sm.logger.WithFields(logrus.Fields{
			"key":       cmd.Key,
			"value":     cmd.Value,
			"existed":   existed,
			"old_value": oldValue,
		}).Debug("Applied PUT command")
		
		return map[string]interface{}{
			"success":   true,
			"operation": "PUT",
			"key":       cmd.Key,
			"existed":   existed,
		}, nil

	case CommandDelete:
		oldValue, existed := sm.data[cmd.Key]
		if !existed {
			return nil, fmt.Errorf("key not found: %s", cmd.Key)
		}
		
		delete(sm.data, cmd.Key)
		
		sm.logger.WithFields(logrus.Fields{
			"key":       cmd.Key,
			"old_value": oldValue,
		}).Debug("Applied DELETE command")
		
		return map[string]interface{}{
			"success":   true,
			"operation": "DELETE",
			"key":       cmd.Key,
			"old_value": oldValue,
		}, nil

	default:
		return nil, fmt.Errorf("unknown command type: %s", cmd.Type)
	}
}

// Get retrieves a value from the state machine (read-only, no consensus needed)
func (sm *KVStateMachine) Get(key string) (string, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	value, exists := sm.data[key]
	return value, exists
}

// GetAll returns all key-value pairs (read-only)
func (sm *KVStateMachine) GetAll() map[string]string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	// Return a copy to prevent external modifications
	result := make(map[string]string, len(sm.data))
	for k, v := range sm.data {
		result[k] = v
	}
	return result
}

// Size returns the number of key-value pairs
func (sm *KVStateMachine) Size() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.data)
}

// Snapshot creates a snapshot of the current state (implements StateMachine interface)
func (sm *KVStateMachine) Snapshot() ([]byte, error) {
	return sm.CreateSnapshot()
}

// CreateSnapshot creates a snapshot of the current state  
func (sm *KVStateMachine) CreateSnapshot() ([]byte, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	snapshot := map[string]interface{}{
		"type": "kv_snapshot",
		"data": sm.data,
	}
	
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal snapshot: %w", err)
	}
	
	sm.logger.WithField("size", len(sm.data)).Debug("Created state machine snapshot")
	return data, nil
}

// Restore restores the state machine from a snapshot (implements StateMachine interface)
func (sm *KVStateMachine) Restore(snapshot []byte) error {
	return sm.RestoreSnapshot(snapshot)
}

// RestoreSnapshot restores the state machine from a snapshot
func (sm *KVStateMachine) RestoreSnapshot(snapshot []byte) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	var snapshotData map[string]interface{}
	if err := json.Unmarshal(snapshot, &snapshotData); err != nil {
		return fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}
	
	// Validate snapshot type
	if snapshotType, ok := snapshotData["type"].(string); !ok || snapshotType != "kv_snapshot" {
		return fmt.Errorf("invalid snapshot type: %v", snapshotData["type"])
	}
	
	// Extract the data
	dataInterface, ok := snapshotData["data"]
	if !ok {
		return fmt.Errorf("snapshot missing data field")
	}
	
	// Convert to map[string]string
	newData := make(map[string]string)
	if dataMap, ok := dataInterface.(map[string]interface{}); ok {
		for k, v := range dataMap {
			if strValue, ok := v.(string); ok {
				newData[k] = strValue
			} else {
				return fmt.Errorf("invalid value type in snapshot data: %T", v)
			}
		}
	} else {
		return fmt.Errorf("invalid snapshot data format: %T", dataInterface)
	}
	
	// Replace the current data
	sm.data = newData
	
	sm.logger.WithField("size", len(sm.data)).Info("Restored state machine from snapshot")
	return nil
}