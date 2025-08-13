package raft

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileStorage implements PersistentStorage using files
type FileStorage struct {
	mu       sync.RWMutex
	dataDir  string
	stateFile string
	snapFile  string
}

// NewFileStorage creates a new file-based storage
func NewFileStorage(dataDir string) (*FileStorage, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	return &FileStorage{
		dataDir:   dataDir,
		stateFile: filepath.Join(dataDir, "raft_state.json"),
		snapFile:  filepath.Join(dataDir, "snapshot.dat"),
	}, nil
}

// SaveState saves the persistent state to disk
func (fs *FileStorage) SaveState(state *PersistentState) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Marshal state to JSON
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to temporary file first
	tempFile := fs.stateFile + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, fs.stateFile); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// LoadState loads the persistent state from disk
func (fs *FileStorage) LoadState() (*PersistentState, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	// Check if state file exists
	if _, err := os.Stat(fs.stateFile); os.IsNotExist(err) {
		// Return default state if file doesn't exist
		return &PersistentState{
			CurrentTerm: 0,
			VotedFor:    "",
			Log: []LogEntry{{
				Term:    0,
				Index:   0,
				Command: nil,
				Data:    nil,
			}},
		}, nil
	}

	// Read state file
	data, err := os.ReadFile(fs.stateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	// Unmarshal state
	var state PersistentState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return &state, nil
}

// SaveSnapshot saves a snapshot to disk
func (fs *FileStorage) SaveSnapshot(snapshot []byte, lastIncludedIndex, lastIncludedTerm uint64) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Create snapshot metadata
	metadata := map[string]interface{}{
		"last_included_index": lastIncludedIndex,
		"last_included_term":  lastIncludedTerm,
		"snapshot":           snapshot,
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Write to temporary file first
	tempFile := fs.snapFile + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp snapshot file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, fs.snapFile); err != nil {
		return fmt.Errorf("failed to rename temp snapshot file: %w", err)
	}

	return nil
}

// LoadSnapshot loads a snapshot from disk
func (fs *FileStorage) LoadSnapshot() ([]byte, uint64, uint64, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	// Check if snapshot file exists
	if _, err := os.Stat(fs.snapFile); os.IsNotExist(err) {
		return nil, 0, 0, nil // No snapshot exists
	}

	// Read snapshot file
	data, err := os.ReadFile(fs.snapFile)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to read snapshot file: %w", err)
	}

	// Unmarshal snapshot metadata
	var metadata map[string]interface{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, 0, 0, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	// Extract fields
	lastIncludedIndex := uint64(metadata["last_included_index"].(float64))
	lastIncludedTerm := uint64(metadata["last_included_term"].(float64))
	snapshot := []byte(metadata["snapshot"].(string))

	return snapshot, lastIncludedIndex, lastIncludedTerm, nil
}

// MemoryStorage implements PersistentStorage using in-memory storage (for testing)
type MemoryStorage struct {
	mu       sync.RWMutex
	state    *PersistentState
	snapshot []byte
	snapIndex uint64
	snapTerm  uint64
}

// NewMemoryStorage creates a new in-memory storage
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		state: &PersistentState{
			CurrentTerm: 0,
			VotedFor:    "",
			Log: []LogEntry{{
				Term:    0,
				Index:   0,
				Command: nil,
				Data:    nil,
			}},
		},
	}
}

// SaveState saves the persistent state in memory
func (ms *MemoryStorage) SaveState(state *PersistentState) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Deep copy the state
	stateCopy := &PersistentState{
		CurrentTerm: state.CurrentTerm,
		VotedFor:    state.VotedFor,
		Log:         make([]LogEntry, len(state.Log)),
	}
	copy(stateCopy.Log, state.Log)

	ms.state = stateCopy
	return nil
}

// LoadState loads the persistent state from memory
func (ms *MemoryStorage) LoadState() (*PersistentState, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if ms.state == nil {
		return &PersistentState{
			CurrentTerm: 0,
			VotedFor:    "",
			Log: []LogEntry{{
				Term:    0,
				Index:   0,
				Command: nil,
				Data:    nil,
			}},
		}, nil
	}

	// Return a copy to avoid race conditions
	stateCopy := &PersistentState{
		CurrentTerm: ms.state.CurrentTerm,
		VotedFor:    ms.state.VotedFor,
		Log:         make([]LogEntry, len(ms.state.Log)),
	}
	copy(stateCopy.Log, ms.state.Log)

	return stateCopy, nil
}

// SaveSnapshot saves a snapshot in memory
func (ms *MemoryStorage) SaveSnapshot(snapshot []byte, lastIncludedIndex, lastIncludedTerm uint64) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.snapshot = make([]byte, len(snapshot))
	copy(ms.snapshot, snapshot)
	ms.snapIndex = lastIncludedIndex
	ms.snapTerm = lastIncludedTerm

	return nil
}

// LoadSnapshot loads a snapshot from memory
func (ms *MemoryStorage) LoadSnapshot() ([]byte, uint64, uint64, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if ms.snapshot == nil {
		return nil, 0, 0, nil
	}

	snapshot := make([]byte, len(ms.snapshot))
	copy(snapshot, ms.snapshot)

	return snapshot, ms.snapIndex, ms.snapTerm, nil
}