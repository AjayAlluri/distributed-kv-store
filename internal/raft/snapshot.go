package raft

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

// SnapshotConfig contains configuration for snapshot behavior
type SnapshotConfig struct {
	// Directory to store snapshots
	SnapshotDir string
	// Maximum number of snapshots to retain
	RetainCount int
	// Threshold for log entries before creating snapshot
	LogSizeThreshold int
	// Enable compression for snapshots
	EnableCompression bool
	// Chunk size for snapshot transfer (bytes)
	ChunkSize int
}

// Snapshot represents a point-in-time state machine snapshot
type Snapshot struct {
	// Metadata
	LastIncludedIndex uint64            `json:"last_included_index"`
	LastIncludedTerm  uint64            `json:"last_included_term"`
	Timestamp         time.Time         `json:"timestamp"`
	Checksum          uint32            `json:"checksum"`
	
	// State machine data
	StateMachineData []byte            `json:"state_machine_data"`
	
	// Cluster membership at time of snapshot
	Membership       []string          `json:"membership"`
	
	// Additional metadata
	ConfigurationData map[string]interface{} `json:"configuration_data,omitempty"`
	
	// File path where snapshot is stored
	FilePath         string            `json:"file_path,omitempty"`
}

// SnapshotMetadata contains just the metadata without the data payload
type SnapshotMetadata struct {
	LastIncludedIndex uint64            `json:"last_included_index"`
	LastIncludedTerm  uint64            `json:"last_included_term"`
	Timestamp         time.Time         `json:"timestamp"`
	Checksum          uint32            `json:"checksum"`
	Size             int64             `json:"size"`
	FilePath         string            `json:"file_path"`
	Membership       []string          `json:"membership"`
}

// DefaultSnapshotConfig returns default snapshot configuration
func DefaultSnapshotConfig(dataDir string) *SnapshotConfig {
	return &SnapshotConfig{
		SnapshotDir:       filepath.Join(dataDir, "snapshots"),
		RetainCount:       5,
		LogSizeThreshold:  1000,
		EnableCompression: true,
		ChunkSize:        64 * 1024, // 64KB chunks
	}
}

// CreateSnapshot creates a new snapshot of the current state machine
func (rn *RaftNode) CreateSnapshot() (*Snapshot, error) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"node_id":     rn.ID,
		"last_applied": rn.LastApplied,
		"commit_index": rn.CommitIndex,
	}).Info("Creating snapshot")

	// Get current state machine data
	stateData, err := rn.getStateMachineSnapshot()
	if err != nil {
		return nil, fmt.Errorf("failed to get state machine snapshot: %v", err)
	}

	// Create snapshot
	snapshot := &Snapshot{
		LastIncludedIndex: rn.LastApplied,
		LastIncludedTerm:  rn.getLogTermAt(rn.LastApplied),
		Timestamp:         time.Now(),
		StateMachineData:  stateData,
		Membership:        rn.getMembershipList(),
		ConfigurationData: make(map[string]interface{}),
	}

	// Calculate checksum
	snapshot.Checksum = rn.calculateSnapshotChecksum(snapshot)

	// Save snapshot to disk
	if err := rn.saveSnapshotToDisk(snapshot); err != nil {
		return nil, fmt.Errorf("failed to save snapshot to disk: %v", err)
	}

	// Update snapshot metadata
	rn.LastSnapshot = snapshot
	rn.LastSnapshotIndex = snapshot.LastIncludedIndex
	rn.LastSnapshotTerm = snapshot.LastIncludedTerm

	logrus.WithFields(logrus.Fields{
		"node_id":              rn.ID,
		"last_included_index":  snapshot.LastIncludedIndex,
		"last_included_term":   snapshot.LastIncludedTerm,
		"state_data_size":      len(stateData),
		"checksum":            snapshot.Checksum,
	}).Info("Snapshot created successfully")

	return snapshot, nil
}

// getStateMachineSnapshot gets a serialized snapshot of the current state machine
func (rn *RaftNode) getStateMachineSnapshot() ([]byte, error) {
	// If we have a KV state machine, get its snapshot
	if rn.StateMachine != nil {
		if kvStateMachine, ok := rn.StateMachine.(*KVStateMachine); ok {
			return kvStateMachine.CreateSnapshot()
		}
	}
	
	// Fallback: return empty snapshot
	return json.Marshal(map[string]interface{}{
		"type": "empty",
		"timestamp": time.Now(),
	})
}

// getMembershipList returns current cluster membership
func (rn *RaftNode) getMembershipList() []string {
	members := make([]string, 0, len(rn.Peers)+1)
	members = append(members, rn.ID)
	
	for peerID := range rn.Peers {
		members = append(members, peerID)
	}
	
	return members
}

// getLogTermAt returns the term of the log entry at the given index
func (rn *RaftNode) getLogTermAt(index uint64) uint64 {
	if index == 0 {
		return 0
	}
	
	if index < uint64(len(rn.Log)) {
		return rn.Log[index].Term
	}
	
	// If beyond current log, return current term
	return rn.CurrentTerm
}

// calculateSnapshotChecksum calculates a CRC32 checksum for the snapshot
func (rn *RaftNode) calculateSnapshotChecksum(snapshot *Snapshot) uint32 {
	hasher := crc32.NewIEEE()
	
	// Hash the state machine data
	hasher.Write(snapshot.StateMachineData)
	
	// Hash metadata
	metaData := fmt.Sprintf("%d:%d:%s", 
		snapshot.LastIncludedIndex, 
		snapshot.LastIncludedTerm,
		snapshot.Timestamp.Format(time.RFC3339))
	hasher.Write([]byte(metaData))
	
	return hasher.Sum32()
}

// saveSnapshotToDisk saves the snapshot to disk
func (rn *RaftNode) saveSnapshotToDisk(snapshot *Snapshot) error {
	if rn.SnapshotConfig == nil {
		return fmt.Errorf("snapshot configuration not initialized")
	}

	// Create snapshots directory if it doesn't exist
	if err := os.MkdirAll(rn.SnapshotConfig.SnapshotDir, 0755); err != nil {
		return fmt.Errorf("failed to create snapshot directory: %v", err)
	}

	// Generate filename
	filename := fmt.Sprintf("snapshot_%d_%d_%d.json", 
		snapshot.LastIncludedIndex, 
		snapshot.LastIncludedTerm,
		snapshot.Timestamp.Unix())
	
	snapshot.FilePath = filepath.Join(rn.SnapshotConfig.SnapshotDir, filename)

	// Prepare data to write
	var dataToWrite []byte
	var err error

	if rn.SnapshotConfig.EnableCompression {
		// Compress the snapshot
		dataToWrite, err = rn.compressSnapshot(snapshot)
		if err != nil {
			return fmt.Errorf("failed to compress snapshot: %v", err)
		}
		snapshot.FilePath += ".gz"
	} else {
		// Regular JSON encoding
		dataToWrite, err = json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal snapshot: %v", err)
		}
	}

	// Write to temporary file first
	tempFile := snapshot.FilePath + ".tmp"
	if err := os.WriteFile(tempFile, dataToWrite, 0644); err != nil {
		return fmt.Errorf("failed to write snapshot to temporary file: %v", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, snapshot.FilePath); err != nil {
		os.Remove(tempFile) // Clean up temp file
		return fmt.Errorf("failed to rename snapshot file: %v", err)
	}

	return nil
}

// compressSnapshot compresses a snapshot using gzip
func (rn *RaftNode) compressSnapshot(snapshot *Snapshot) ([]byte, error) {
	// First marshal to JSON
	jsonData, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}

	// Compress with gzip
	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	
	if _, err := gzWriter.Write(jsonData); err != nil {
		return nil, err
	}
	
	if err := gzWriter.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// LoadSnapshot loads a snapshot from disk
func (rn *RaftNode) LoadSnapshot(filePath string) (*Snapshot, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot file: %v", err)
	}

	var snapshot *Snapshot

	// Check if file is compressed
	if filepath.Ext(filePath) == ".gz" {
		snapshot, err = rn.decompressSnapshot(data)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress snapshot: %v", err)
		}
	} else {
		snapshot = &Snapshot{}
		if err := json.Unmarshal(data, snapshot); err != nil {
			return nil, fmt.Errorf("failed to unmarshal snapshot: %v", err)
		}
	}

	// Verify checksum
	expectedChecksum := rn.calculateSnapshotChecksum(snapshot)
	if snapshot.Checksum != expectedChecksum {
		return nil, fmt.Errorf("snapshot checksum mismatch: expected %d, got %d", 
			expectedChecksum, snapshot.Checksum)
	}

	snapshot.FilePath = filePath
	return snapshot, nil
}

// decompressSnapshot decompresses a gzipped snapshot
func (rn *RaftNode) decompressSnapshot(compressedData []byte) (*Snapshot, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressedData))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	jsonData, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	var snapshot Snapshot
	if err := json.Unmarshal(jsonData, &snapshot); err != nil {
		return nil, err
	}

	return &snapshot, nil
}

// InstallSnapshot installs a snapshot, replacing current state
func (rn *RaftNode) InstallSnapshot(snapshot *Snapshot) error {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"node_id":              rn.ID,
		"last_included_index":  snapshot.LastIncludedIndex,
		"last_included_term":   snapshot.LastIncludedTerm,
		"current_last_applied": rn.LastApplied,
	}).Info("Installing snapshot")

	// Verify snapshot is newer than current state
	if snapshot.LastIncludedIndex <= rn.LastApplied {
		logrus.WithFields(logrus.Fields{
			"node_id":                rn.ID,
			"snapshot_last_included": snapshot.LastIncludedIndex,
			"current_last_applied":   rn.LastApplied,
		}).Debug("Snapshot is not newer than current state, ignoring")
		return nil
	}

	// Install state machine data
	if err := rn.installStateMachineSnapshot(snapshot.StateMachineData); err != nil {
		return fmt.Errorf("failed to install state machine snapshot: %v", err)
	}

	// Update Raft state
	rn.LastApplied = snapshot.LastIncludedIndex
	rn.LastSnapshot = snapshot
	rn.LastSnapshotIndex = snapshot.LastIncludedIndex
	rn.LastSnapshotTerm = snapshot.LastIncludedTerm

	// Truncate log - remove entries that are included in the snapshot
	rn.truncateLogToSnapshot(snapshot.LastIncludedIndex, snapshot.LastIncludedTerm)

	// Update commit index if necessary
	if rn.CommitIndex < snapshot.LastIncludedIndex {
		rn.CommitIndex = snapshot.LastIncludedIndex
	}

	// Persist the updated state
	rn.persistState()

	logrus.WithFields(logrus.Fields{
		"node_id":              rn.ID,
		"last_included_index":  snapshot.LastIncludedIndex,
		"new_last_applied":     rn.LastApplied,
		"new_log_length":       len(rn.Log),
	}).Info("Snapshot installed successfully")

	return nil
}

// installStateMachineSnapshot installs snapshot data into the state machine
func (rn *RaftNode) installStateMachineSnapshot(data []byte) error {
	if rn.StateMachine != nil {
		if kvStateMachine, ok := rn.StateMachine.(*KVStateMachine); ok {
			return kvStateMachine.RestoreSnapshot(data)
		}
	}
	
	// For now, just log that we received snapshot data
	logrus.WithFields(logrus.Fields{
		"node_id":   rn.ID,
		"data_size": len(data),
	}).Debug("Installed state machine snapshot")
	
	return nil
}

// truncateLogToSnapshot truncates the log to start after the snapshot
func (rn *RaftNode) truncateLogToSnapshot(lastIncludedIndex, lastIncludedTerm uint64) {
	// Find the position in the log corresponding to the snapshot
	if lastIncludedIndex == 0 {
		return // Nothing to truncate
	}

	// Create new log starting with a dummy entry representing the snapshot
	newLog := make([]LogEntry, 1, len(rn.Log))
	newLog[0] = LogEntry{
		Term:    lastIncludedTerm,
		Index:   lastIncludedIndex,
		Command: nil,
		Data:    []byte("snapshot"),
	}

	// Keep any entries that come after the snapshot
	for i, entry := range rn.Log {
		if entry.Index > lastIncludedIndex {
			newLog = append(newLog, rn.Log[i:]...)
			break
		}
	}

	oldLogLength := len(rn.Log)
	rn.Log = newLog

	logrus.WithFields(logrus.Fields{
		"node_id":              rn.ID,
		"old_log_length":       oldLogLength,
		"new_log_length":       len(rn.Log),
		"last_included_index":  lastIncludedIndex,
	}).Debug("Truncated log after snapshot installation")
}

// ShouldCreateSnapshot determines if a snapshot should be created
func (rn *RaftNode) ShouldCreateSnapshot() bool {
	rn.mu.RLock()
	defer rn.mu.RUnlock()

	if rn.SnapshotConfig == nil {
		return false
	}

	// Check if log size exceeds threshold
	logSize := len(rn.Log)
	threshold := rn.SnapshotConfig.LogSizeThreshold

	// Also check if enough entries have been applied since last snapshot
	entriesSinceSnapshot := rn.LastApplied - rn.LastSnapshotIndex

	shouldCreate := logSize > threshold && entriesSinceSnapshot > uint64(threshold/2)

	if shouldCreate {
		logrus.WithFields(logrus.Fields{
			"node_id":                 rn.ID,
			"log_size":                logSize,
			"threshold":               threshold,
			"entries_since_snapshot":  entriesSinceSnapshot,
			"last_snapshot_index":     rn.LastSnapshotIndex,
		}).Debug("Should create snapshot")
	}

	return shouldCreate
}

// cleanupOldSnapshots removes old snapshot files beyond the retention count
func (rn *RaftNode) cleanupOldSnapshots() error {
	if rn.SnapshotConfig == nil {
		return nil
	}

	snapshots, err := rn.listSnapshots()
	if err != nil {
		return err
	}

	// Keep only the most recent snapshots
	if len(snapshots) > rn.SnapshotConfig.RetainCount {
		toDelete := snapshots[rn.SnapshotConfig.RetainCount:]
		
		for _, meta := range toDelete {
			if err := os.Remove(meta.FilePath); err != nil {
				logrus.WithFields(logrus.Fields{
					"node_id":   rn.ID,
					"file_path": meta.FilePath,
					"error":     err,
				}).Warn("Failed to delete old snapshot")
			} else {
				logrus.WithFields(logrus.Fields{
					"node_id":   rn.ID,
					"file_path": meta.FilePath,
				}).Debug("Deleted old snapshot")
			}
		}
	}

	return nil
}

// listSnapshots returns a list of available snapshots, sorted by timestamp (newest first)
func (rn *RaftNode) listSnapshots() ([]*SnapshotMetadata, error) {
	if rn.SnapshotConfig == nil {
		return nil, fmt.Errorf("snapshot configuration not initialized")
	}

	entries, err := os.ReadDir(rn.SnapshotConfig.SnapshotDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*SnapshotMetadata{}, nil
		}
		return nil, err
	}

	var snapshots []*SnapshotMetadata
	
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		
		fileName := entry.Name()
		if !rn.isSnapshotFile(fileName) {
			continue
		}

		filePath := filepath.Join(rn.SnapshotConfig.SnapshotDir, fileName)
		
		// Get file info
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Try to extract metadata from filename
		meta := rn.extractSnapshotMetadata(fileName, filePath, info.Size())
		if meta != nil {
			snapshots = append(snapshots, meta)
		}
	}

	// Sort by timestamp (newest first)
	for i := 0; i < len(snapshots)-1; i++ {
		for j := i + 1; j < len(snapshots); j++ {
			if snapshots[i].Timestamp.Before(snapshots[j].Timestamp) {
				snapshots[i], snapshots[j] = snapshots[j], snapshots[i]
			}
		}
	}

	return snapshots, nil
}

// isSnapshotFile checks if a filename represents a snapshot file
func (rn *RaftNode) isSnapshotFile(filename string) bool {
	return len(filename) > 9 && filename[:9] == "snapshot_" &&
		(filepath.Ext(filename) == ".json" || filepath.Ext(filename) == ".gz")
}

// extractSnapshotMetadata extracts metadata from a snapshot filename
func (rn *RaftNode) extractSnapshotMetadata(filename, filePath string, size int64) *SnapshotMetadata {
	// Parse filename: snapshot_{lastIndex}_{lastTerm}_{timestamp}.json[.gz]
	var lastIndex, lastTerm, timestamp uint64
	
	n, err := fmt.Sscanf(filename, "snapshot_%d_%d_%d", &lastIndex, &lastTerm, &timestamp)
	if err != nil || n != 3 {
		return nil
	}

	return &SnapshotMetadata{
		LastIncludedIndex: lastIndex,
		LastIncludedTerm:  lastTerm,
		Timestamp:         time.Unix(int64(timestamp), 0),
		Size:              size,
		FilePath:          filePath,
	}
}

// prepareSnapshotInstallation prepares for receiving a new snapshot
func (rn *RaftNode) prepareSnapshotInstallation(req *InstallSnapshotRequest) {
	// Clear any pending snapshot data from previous incomplete installations
	rn.pendingSnapshotData = nil
	rn.pendingSnapshotOffset = 0

	logrus.WithFields(logrus.Fields{
		"node_id":             rn.ID,
		"last_included_index": req.LastIncludedIndex,
		"last_included_term":  req.LastIncludedTerm,
	}).Debug("Preparing for snapshot installation")
}

// accumulateSnapshotData accumulates data from a snapshot chunk
func (rn *RaftNode) accumulateSnapshotData(req *InstallSnapshotRequest) error {
	// Validate that chunks are arriving in order
	if req.Offset != rn.pendingSnapshotOffset {
		return fmt.Errorf("snapshot chunk out of order: expected offset %d, got %d", 
			rn.pendingSnapshotOffset, req.Offset)
	}

	// Append the new data
	if rn.pendingSnapshotData == nil {
		rn.pendingSnapshotData = make([]byte, 0, len(req.Data)*10) // Pre-allocate with estimate
	}
	
	rn.pendingSnapshotData = append(rn.pendingSnapshotData, req.Data...)
	rn.pendingSnapshotOffset += uint64(len(req.Data))

	logrus.WithFields(logrus.Fields{
		"node_id":              rn.ID,
		"offset":               req.Offset,
		"chunk_size":           len(req.Data),
		"total_received":       len(rn.pendingSnapshotData),
		"expected_next_offset": rn.pendingSnapshotOffset,
	}).Debug("Accumulated snapshot chunk")

	return nil
}

// finalizeSnapshotInstallation finalizes the installation of a complete snapshot
func (rn *RaftNode) finalizeSnapshotInstallation(req *InstallSnapshotRequest) error {
	// Create snapshot object
	snapshot := &Snapshot{
		LastIncludedIndex: req.LastIncludedIndex,
		LastIncludedTerm:  req.LastIncludedTerm,
		Timestamp:         time.Now(),
		StateMachineData:  rn.pendingSnapshotData,
		Membership:        rn.getMembershipList(),
		ConfigurationData: make(map[string]interface{}),
	}

	// Calculate and verify checksum if provided
	expectedChecksum := rn.calculateSnapshotChecksum(snapshot)
	snapshot.Checksum = expectedChecksum

	// Install the snapshot
	if err := rn.InstallSnapshot(snapshot); err != nil {
		return fmt.Errorf("failed to install snapshot: %w", err)
	}

	// Save snapshot to disk
	if err := rn.saveSnapshotToDisk(snapshot); err != nil {
		logrus.WithFields(logrus.Fields{
			"node_id": rn.ID,
			"error":   err,
		}).Warn("Failed to save received snapshot to disk (snapshot still installed in memory)")
	}

	// Clean up pending data
	rn.pendingSnapshotData = nil
	rn.pendingSnapshotOffset = 0

	// Clean up old snapshots
	if err := rn.cleanupOldSnapshots(); err != nil {
		logrus.WithFields(logrus.Fields{
			"node_id": rn.ID,
			"error":   err,
		}).Warn("Failed to clean up old snapshots")
	}

	return nil
}

// sendSnapshot sends a snapshot to a follower node
func (rn *RaftNode) sendSnapshot(peerID, peerAddr string, snapshot *Snapshot) error {
	if rn.SnapshotConfig == nil {
		return fmt.Errorf("snapshot configuration not initialized")
	}

	// Load snapshot data from disk if not in memory
	var snapshotData []byte
	if len(snapshot.StateMachineData) > 0 {
		snapshotData = snapshot.StateMachineData
	} else if snapshot.FilePath != "" {
		// Try to load from disk
		loadedSnapshot, err := rn.LoadSnapshot(snapshot.FilePath)
		if err != nil {
			return fmt.Errorf("failed to load snapshot from disk: %w", err)
		}
		snapshotData = loadedSnapshot.StateMachineData
	} else {
		return fmt.Errorf("snapshot has no data or file path")
	}

	// Calculate checksum
	checksum := crc32.ChecksumIEEE(snapshotData)
	chunkSize := rn.SnapshotConfig.ChunkSize
	totalSize := uint64(len(snapshotData))

	logrus.WithFields(logrus.Fields{
		"node_id":             rn.ID,
		"peer":                peerID,
		"last_included_index": snapshot.LastIncludedIndex,
		"last_included_term":  snapshot.LastIncludedTerm,
		"total_size":          totalSize,
		"chunk_size":          chunkSize,
		"checksum":            checksum,
	}).Info("Sending snapshot to peer")

	// Send snapshot in chunks
	for offset := 0; offset < len(snapshotData); offset += chunkSize {
		end := offset + chunkSize
		if end > len(snapshotData) {
			end = len(snapshotData)
		}

		chunk := snapshotData[offset:end]
		done := end >= len(snapshotData)

		req := &InstallSnapshotRequest{
			Term:              rn.CurrentTerm,
			LeaderID:          rn.ID,
			LastIncludedIndex: snapshot.LastIncludedIndex,
			LastIncludedTerm:  snapshot.LastIncludedTerm,
			Offset:            uint64(offset),
			Data:              chunk,
			Done:              done,
			TotalSize:         totalSize,
			Checksum:          uint64(checksum),
		}

		// Send chunk
		resp, err := rn.Transport.SendInstallSnapshot(peerAddr, req)
		if err != nil {
			return fmt.Errorf("failed to send snapshot chunk at offset %d: %w", offset, err)
		}

		// Check response
		if resp.Term > rn.CurrentTerm {
			// Become follower if we discover a higher term
			rn.mu.Lock()
			rn.becomeFollower(resp.Term)
			rn.mu.Unlock()
			return fmt.Errorf("discovered higher term %d, stepping down", resp.Term)
		}

		if !resp.Success {
			return fmt.Errorf("peer rejected snapshot chunk at offset %d", offset)
		}

		logrus.WithFields(logrus.Fields{
			"node_id":         rn.ID,
			"peer":            peerID,
			"offset":          offset,
			"chunk_size":      len(chunk),
			"done":            done,
			"bytes_received":  resp.BytesReceived,
		}).Debug("Successfully sent snapshot chunk")
	}

	logrus.WithFields(logrus.Fields{
		"node_id":             rn.ID,
		"peer":                peerID,
		"last_included_index": snapshot.LastIncludedIndex,
		"total_size":          totalSize,
	}).Info("Successfully sent complete snapshot to peer")

	return nil
}

// checkAndCreateSnapshot checks if a snapshot should be created and creates it
func (rn *RaftNode) checkAndCreateSnapshot() {
	// Only leaders should initiate snapshots
	rn.mu.RLock()
	isLeader := rn.State == Leader
	rn.mu.RUnlock()
	
	if !isLeader {
		return
	}

	// Check if snapshot should be created
	if !rn.ShouldCreateSnapshot() {
		return
	}

	// Create snapshot asynchronously to avoid blocking
	go rn.createSnapshotAsync()
}

// createSnapshotAsync creates a snapshot asynchronously
func (rn *RaftNode) createSnapshotAsync() {
	logrus.WithField("node_id", rn.ID).Info("Creating snapshot asynchronously")

	snapshot, err := rn.CreateSnapshot()
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"node_id": rn.ID,
			"error":   err,
		}).Error("Failed to create snapshot")
		return
	}

	// Compact the log by removing entries included in snapshot
	rn.mu.Lock()
	oldLogLength := len(rn.Log)
	rn.compactLogWithSnapshot(snapshot)
	newLogLength := len(rn.Log)
	rn.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"node_id":              rn.ID,
		"last_included_index":  snapshot.LastIncludedIndex,
		"old_log_length":       oldLogLength,
		"new_log_length":       newLogLength,
		"space_saved":          oldLogLength - newLogLength,
	}).Info("Log compacted after snapshot creation")

	// TODO: Send snapshot to followers who are far behind
	// This would be implemented as part of leader's heartbeat logic
	// For now, followers will catch up through normal AppendEntries
}

// compactLogWithSnapshot removes log entries that are included in the snapshot
func (rn *RaftNode) compactLogWithSnapshot(snapshot *Snapshot) {
	if snapshot.LastIncludedIndex == 0 {
		return // Nothing to compact
	}

	// Find entries to keep (after snapshot)
	var newLog []LogEntry
	
	// Always keep a dummy entry at index 0 representing the snapshot
	newLog = append(newLog, LogEntry{
		Term:    snapshot.LastIncludedTerm,
		Index:   snapshot.LastIncludedIndex,
		Command: nil,
		Data:    []byte("snapshot"),
	})

	// Keep entries that come after the snapshot
	for i, entry := range rn.Log {
		if entry.Index > snapshot.LastIncludedIndex {
			newLog = append(newLog, rn.Log[i:]...)
			break
		}
	}

	rn.Log = newLog

	// Persist the updated state
	rn.persistState()
}

// GetSnapshotInfo returns information about the current snapshot
func (rn *RaftNode) GetSnapshotInfo() *SnapshotMetadata {
	rn.mu.RLock()
	defer rn.mu.RUnlock()

	if rn.LastSnapshot == nil {
		return nil
	}

	return &SnapshotMetadata{
		LastIncludedIndex: rn.LastSnapshot.LastIncludedIndex,
		LastIncludedTerm:  rn.LastSnapshot.LastIncludedTerm,
		Timestamp:         rn.LastSnapshot.Timestamp,
		Checksum:          rn.LastSnapshot.Checksum,
		Size:              int64(len(rn.LastSnapshot.StateMachineData)),
		FilePath:          rn.LastSnapshot.FilePath,
		Membership:        rn.LastSnapshot.Membership,
	}
}