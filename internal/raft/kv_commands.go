package raft

import (
	"encoding/json"
	"fmt"
)

// CommandType represents the type of KV operation
type CommandType string

const (
	CommandPut    CommandType = "PUT"
	CommandDelete CommandType = "DELETE"
)

// KVCommand represents a key-value operation that will be replicated via Raft
type KVCommand struct {
	Type  CommandType `json:"type"`
	Key   string      `json:"key"`
	Value string      `json:"value,omitempty"` // Only used for PUT operations
}

// SerializeCommand serializes a KV command to bytes for storage in Raft log
func SerializeCommand(cmd *KVCommand) ([]byte, error) {
	return json.Marshal(cmd)
}

// DeserializeCommand deserializes bytes back to a KV command
func DeserializeCommand(data []byte) (*KVCommand, error) {
	var cmd KVCommand
	err := json.Unmarshal(data, &cmd)
	return &cmd, err
}

// String returns a string representation of the command
func (cmd *KVCommand) String() string {
	switch cmd.Type {
	case CommandPut:
		return fmt.Sprintf("PUT %s=%s", cmd.Key, cmd.Value)
	case CommandDelete:
		return fmt.Sprintf("DELETE %s", cmd.Key)
	default:
		return fmt.Sprintf("UNKNOWN %s", cmd.Type)
	}
}

// Validate checks if the command is valid
func (cmd *KVCommand) Validate() error {
	if cmd.Key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	switch cmd.Type {
	case CommandPut:
		// PUT commands can have empty values
		return nil
	case CommandDelete:
		// DELETE commands should not have values
		if cmd.Value != "" {
			return fmt.Errorf("DELETE command should not have a value")
		}
		return nil
	default:
		return fmt.Errorf("unknown command type: %s", cmd.Type)
	}
}