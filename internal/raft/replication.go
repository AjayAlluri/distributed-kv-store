package raft

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// sendHeartbeats sends periodic heartbeats to all followers
func (rn *RaftNode) sendHeartbeats() {
	ticker := time.NewTicker(rn.HeartbeatTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-rn.ShutdownCh:
			return
		case <-ticker.C:
			rn.mu.RLock()
			if rn.State != Leader {
				rn.mu.RUnlock()
				return
			}
			term := rn.CurrentTerm
			rn.mu.RUnlock()

			// Send heartbeats to all peers
			var wg sync.WaitGroup
			for peerID, peerAddr := range rn.Peers {
				if peerID == rn.ID {
					continue
				}

				wg.Add(1)
				go func(peer string, addr string) {
					defer wg.Done()
					rn.sendAppendEntries(peer, addr, term)
				}(peerID, peerAddr)
			}
			wg.Wait()
		}
	}
}

// sendAppendEntries sends append entries RPC to a peer
func (rn *RaftNode) sendAppendEntries(peerID, peerAddr string, term uint64) {
	rn.mu.RLock()
	
	// Double-check we're still leader in the same term
	if rn.State != Leader || rn.CurrentTerm != term {
		rn.mu.RUnlock()
		return
	}

	nextIndex := rn.NextIndex[peerID]
	prevLogIndex := nextIndex - 1
	var prevLogTerm uint64
	
	// Get previous log term
	if prevLogIndex > 0 && prevLogIndex < uint64(len(rn.Log)) {
		prevLogTerm = rn.Log[prevLogIndex].Term
	}

	// Prepare entries to send
	var entries []LogEntry
	if nextIndex < uint64(len(rn.Log)) {
		// Send new entries
		entries = make([]LogEntry, 0, len(rn.Log)-int(nextIndex))
		for i := nextIndex; i < uint64(len(rn.Log)); i++ {
			entries = append(entries, rn.Log[i])
		}
	}

	req := &AppendEntriesRequest{
		Term:         rn.CurrentTerm,
		LeaderID:     rn.ID,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: rn.CommitIndex,
	}

	rn.mu.RUnlock()

	logrus.WithFields(logrus.Fields{
		"node_id":        rn.ID,
		"peer":           peerID,
		"term":           req.Term,
		"prev_log_index": req.PrevLogIndex,
		"prev_log_term":  req.PrevLogTerm,
		"entries":        len(req.Entries),
		"leader_commit":  req.LeaderCommit,
	}).Debug("Sending append entries")

	// Use the transport to send the request
	resp, err := rn.Transport.SendAppendEntries(peerAddr, req)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"node_id": rn.ID,
			"peer":    peerID,
			"peer_addr": peerAddr,
			"error":   err,
		}).Error("Failed to send append entries")
		return
	}
	
	// Handle response
	if resp != nil {
		rn.handleAppendEntriesResponse(resp)
	}
}


// HandleAppendEntries processes an incoming append entries request synchronously (implements RPCHandler)
func (rn *RaftNode) HandleAppendEntries(req *AppendEntriesRequest) *AppendEntriesResponse {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"node_id":        rn.ID,
		"from":           req.LeaderID,
		"term":           req.Term,
		"current_term":   rn.CurrentTerm,
		"prev_log_index": req.PrevLogIndex,
		"entries":        len(req.Entries),
	}).Debug("Received append entries")

	resp := &AppendEntriesResponse{
		Term:    rn.CurrentTerm,
		Success: false,
	}

	// If leader's term is older, reject
	if req.Term < rn.CurrentTerm {
		logrus.WithField("node_id", rn.ID).Debug("Rejecting append entries: stale term")
		return resp
	}

	// If leader's term is newer, become follower
	if req.Term > rn.CurrentTerm {
		rn.becomeFollower(req.Term)
	}

	// Reset election timer since we heard from leader
	rn.resetElectionTimer()
	rn.LastHeartbeat = time.Now()

	resp.Term = rn.CurrentTerm

	// Check if log contains an entry at prevLogIndex with matching term
	if req.PrevLogIndex > 0 {
		if req.PrevLogIndex >= uint64(len(rn.Log)) {
			// Log doesn't contain entry at prevLogIndex
			logrus.WithFields(logrus.Fields{
				"node_id":        rn.ID,
				"prev_log_index": req.PrevLogIndex,
				"log_length":     len(rn.Log),
			}).Debug("Rejecting append entries: missing previous entry")
			return resp
		}

		if rn.Log[req.PrevLogIndex].Term != req.PrevLogTerm {
			// Log contains entry at prevLogIndex but term doesn't match
			logrus.WithFields(logrus.Fields{
				"node_id":       rn.ID,
				"prev_log_term": req.PrevLogTerm,
				"actual_term":   rn.Log[req.PrevLogIndex].Term,
			}).Debug("Rejecting append entries: term mismatch")
			return resp
		}
	}

	// If we get here, the append entries can succeed
	resp.Success = true

	// If there are new entries to append
	if len(req.Entries) > 0 {
		// Delete any conflicting entries and append new ones
		insertIndex := req.PrevLogIndex + 1
		
		// Truncate log if necessary
		if insertIndex < uint64(len(rn.Log)) {
			rn.Log = rn.Log[:insertIndex]
		}

		// Append new entries
		rn.Log = append(rn.Log, req.Entries...)
		
		// Persist the updated log
		rn.persistState()

		logrus.WithFields(logrus.Fields{
			"node_id":      rn.ID,
			"new_entries":  len(req.Entries),
			"log_length":   len(rn.Log),
		}).Debug("Appended new entries")
	}

	// Update commit index
	if req.LeaderCommit > rn.CommitIndex {
		oldCommitIndex := rn.CommitIndex
		rn.CommitIndex = min(req.LeaderCommit, uint64(len(rn.Log)-1))
		
		if rn.CommitIndex > oldCommitIndex {
			logrus.WithFields(logrus.Fields{
				"node_id":         rn.ID,
				"old_commit":      oldCommitIndex,
				"new_commit":      rn.CommitIndex,
			}).Debug("Updated commit index")
			
			// Apply newly committed entries
			go rn.applyCommittedEntries()
		}
	}

	return resp
}

// handleAppendEntries handles an incoming append entries request via channel (for backward compatibility)
func (rn *RaftNode) handleAppendEntries(req *AppendEntriesRequest) {
	// Just call the synchronous handler - response will be ignored in channel-based flow
	rn.HandleAppendEntries(req)
}

// HandleInstallSnapshot processes an incoming install snapshot request synchronously (implements RPCHandler)
func (rn *RaftNode) HandleInstallSnapshot(req *InstallSnapshotRequest) *InstallSnapshotResponse {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"node_id": rn.ID,
		"from":    req.LeaderID,
		"term":    req.Term,
	}).Debug("Received install snapshot request")

	resp := &InstallSnapshotResponse{
		Term: rn.CurrentTerm,
	}

	// If leader's term is older, reject
	if req.Term < rn.CurrentTerm {
		return resp
	}

	// If leader's term is newer, become follower
	if req.Term > rn.CurrentTerm {
		rn.becomeFollower(req.Term)
		resp.Term = rn.CurrentTerm
	}

	// Reset election timer since we heard from leader
	rn.resetElectionTimer()

	// TODO: Implement actual snapshot installation
	// For now, just acknowledge receipt
	return resp
}

// handleAppendEntriesResponse handles a response to append entries
func (rn *RaftNode) handleAppendEntriesResponse(resp *AppendEntriesResponse) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	// If response term is newer, become follower
	if resp.Term > rn.CurrentTerm {
		rn.becomeFollower(resp.Term)
		return
	}

	// Only process if we're still leader
	if rn.State != Leader || resp.Term != rn.CurrentTerm {
		return
	}

	// This is a simplified implementation
	// In a real implementation, we'd track which peer this response came from
	// and update NextIndex/MatchIndex accordingly
	
	if resp.Success {
		logrus.WithField("node_id", rn.ID).Debug("Append entries succeeded")
		// Update NextIndex and MatchIndex for the peer
		// Check if we can advance commit index
		rn.updateCommitIndex()
	} else {
		logrus.WithField("node_id", rn.ID).Debug("Append entries failed")
		// Decrement NextIndex for the peer and retry
	}
}

// updateCommitIndex updates the commit index based on majority replication
func (rn *RaftNode) updateCommitIndex() {
	// This is a simplified implementation
	// In a real implementation, we'd check if a majority of servers
	// have replicated entries up to a certain index
	
	oldCommitIndex := rn.CommitIndex
	
	// For now, just advance commit index to the last log entry
	// In a real implementation, this would be more sophisticated
	if uint64(len(rn.Log)-1) > rn.CommitIndex {
		rn.CommitIndex = uint64(len(rn.Log) - 1)
		
		if rn.CommitIndex > oldCommitIndex {
			logrus.WithFields(logrus.Fields{
				"node_id":     rn.ID,
				"old_commit":  oldCommitIndex,
				"new_commit":  rn.CommitIndex,
			}).Debug("Advanced commit index")
			
			// Apply newly committed entries
			go rn.applyCommittedEntries()
		}
	}
}

// applyCommittedEntries applies committed log entries to the state machine
func (rn *RaftNode) applyCommittedEntries() {
	rn.mu.Lock()
	lastApplied := rn.LastApplied
	commitIndex := rn.CommitIndex
	entries := make([]LogEntry, 0)
	
	// Get entries to apply
	for i := lastApplied + 1; i <= commitIndex && i < uint64(len(rn.Log)); i++ {
		entries = append(entries, rn.Log[i])
	}
	rn.mu.Unlock()

	// Apply entries to state machine
	for _, entry := range entries {
		if entry.Command != nil {
			applyMsg := ApplyMsg{
				CommandValid: true,
				Command:      entry.Command,
				CommandIndex: entry.Index,
			}

			select {
			case rn.ApplyCh <- applyMsg:
				logrus.WithFields(logrus.Fields{
					"node_id": rn.ID,
					"index":   entry.Index,
					"term":    entry.Term,
				}).Debug("Applied log entry to state machine")
			case <-rn.ShutdownCh:
				return
			}
		}

		// Update last applied
		rn.mu.Lock()
		rn.LastApplied = entry.Index
		rn.mu.Unlock()
	}
}

// min returns the minimum of two uint64 values
func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}