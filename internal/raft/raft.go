package raft

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// NewRaftNode creates a new Raft node with the given configuration
func NewRaftNode(config Config) *RaftNode {
	node := &RaftNode{
		ID:               config.NodeID,
		Peers:            config.Peers,
		State:            Follower,
		CurrentTerm:      0,
		VotedFor:         "",
		Log:              make([]LogEntry, 0),
		CommitIndex:      0,
		LastApplied:      0,
		NextIndex:        make(map[string]uint64),
		MatchIndex:       make(map[string]uint64),
		ElectionTimeout:  config.ElectionTimeout,
		HeartbeatTimeout: config.HeartbeatTimeout,
		LastHeartbeat:    time.Now(),
		VoteRequestCh:    make(chan *VoteRequest, 100),
		VoteResponseCh:   make(chan *VoteResponse, 100),
		AppendEntriesCh:  make(chan *AppendEntriesRequest, 100),
		AppendRespCh:     make(chan *AppendEntriesResponse, 100),
		ShutdownCh:       make(chan struct{}),
		ApplyCh:          config.ApplyCh,
		Storage:          config.Storage,
	}

	// Initialize log with a dummy entry at index 0
	node.Log = append(node.Log, LogEntry{
		Term:    0,
		Index:   0,
		Command: nil,
		Data:    nil,
	})

	// Load persistent state if available
	if state, err := node.Storage.LoadState(); err == nil && state != nil {
		node.CurrentTerm = state.CurrentTerm
		node.VotedFor = state.VotedFor
		node.Log = state.Log
	}

	return node
}

// Start starts the Raft node and begins the main event loop
func (rn *RaftNode) Start() {
	logrus.WithFields(logrus.Fields{
		"node_id": rn.ID,
		"peers":   rn.Peers,
	}).Info("Starting Raft node")

	// Start the main event loop
	go rn.eventLoop()

	// Start election timer
	rn.resetElectionTimer()
}

// Stop gracefully stops the Raft node
func (rn *RaftNode) Stop() {
	logrus.WithField("node_id", rn.ID).Info("Stopping Raft node")
	close(rn.ShutdownCh)
}

// eventLoop is the main event loop for the Raft node
func (rn *RaftNode) eventLoop() {
	for {
		select {
		case <-rn.ShutdownCh:
			return

		case req := <-rn.VoteRequestCh:
			rn.handleVoteRequest(req)

		case resp := <-rn.VoteResponseCh:
			rn.handleVoteResponse(resp)

		case req := <-rn.AppendEntriesCh:
			rn.handleAppendEntries(req)

		case resp := <-rn.AppendRespCh:
			rn.handleAppendEntriesResponse(resp)

		case <-rn.ElectionTimer.C:
			rn.handleElectionTimeout()
		}
	}
}

// GetState returns the current state of the node (thread-safe)
func (rn *RaftNode) GetState() (term uint64, isLeader bool) {
	rn.mu.RLock()
	defer rn.mu.RUnlock()
	return rn.CurrentTerm, rn.State == Leader
}

// GetLeader returns the current leader ID (thread-safe)
func (rn *RaftNode) GetLeader() string {
	rn.mu.RLock()
	defer rn.mu.RUnlock()
	
	if rn.State == Leader {
		return rn.ID
	}
	
	// In a real implementation, we'd track the current leader
	// For now, return empty string if this node is not the leader
	return ""
}

// Submit submits a command to the Raft cluster
func (rn *RaftNode) Submit(command interface{}) (uint64, uint64, bool) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	// Only leaders can accept client requests
	if rn.State != Leader {
		return 0, rn.CurrentTerm, false
	}

	// Create new log entry
	index := uint64(len(rn.Log))
	entry := LogEntry{
		Term:    rn.CurrentTerm,
		Index:   index,
		Command: command,
		Data:    nil, // Will be serialized later
	}

	// Append to local log
	rn.Log = append(rn.Log, entry)

	// Persist the log entry
	rn.persistState()

	logrus.WithFields(logrus.Fields{
		"node_id": rn.ID,
		"term":    rn.CurrentTerm,
		"index":   index,
	}).Debug("Appended log entry")

	return index, rn.CurrentTerm, true
}

// persistState saves the current persistent state
func (rn *RaftNode) persistState() {
	state := &PersistentState{
		CurrentTerm: rn.CurrentTerm,
		VotedFor:    rn.VotedFor,
		Log:         rn.Log,
	}

	if err := rn.Storage.SaveState(state); err != nil {
		logrus.WithFields(logrus.Fields{
			"node_id": rn.ID,
			"error":   err,
		}).Error("Failed to persist state")
	}
}

// resetElectionTimer resets the election timeout with a random duration
func (rn *RaftNode) resetElectionTimer() {
	if rn.ElectionTimer != nil {
		rn.ElectionTimer.Stop()
	}

	// Random timeout to avoid split votes
	timeout := rn.ElectionTimeout + time.Duration(rand.Int63n(int64(rn.ElectionTimeout)))
	rn.ElectionTimer = time.NewTimer(timeout)
}

// becomeFollower transitions the node to follower state
func (rn *RaftNode) becomeFollower(term uint64) {
	logrus.WithFields(logrus.Fields{
		"node_id":  rn.ID,
		"old_term": rn.CurrentTerm,
		"new_term": term,
		"old_state": rn.State.String(),
	}).Info("Becoming follower")

	rn.State = Follower
	rn.CurrentTerm = term
	rn.VotedFor = ""
	rn.resetElectionTimer()
	rn.persistState()
}

// becomeCandidate transitions the node to candidate state and starts election
func (rn *RaftNode) becomeCandidate() {
	logrus.WithFields(logrus.Fields{
		"node_id":  rn.ID,
		"term":     rn.CurrentTerm + 1,
		"old_state": rn.State.String(),
	}).Info("Becoming candidate")

	rn.State = Candidate
	rn.CurrentTerm++
	rn.VotedFor = rn.ID
	rn.resetElectionTimer()
	rn.persistState()

	// Start election
	rn.startElection()
}

// becomeLeader transitions the node to leader state
func (rn *RaftNode) becomeLeader() {
	logrus.WithFields(logrus.Fields{
		"node_id": rn.ID,
		"term":    rn.CurrentTerm,
		"old_state": rn.State.String(),
	}).Info("Becoming leader")

	rn.State = Leader

	// Initialize leader state
	lastLogIndex := uint64(len(rn.Log) - 1)
	for _, peerID := range rn.Peers {
		rn.NextIndex[peerID] = lastLogIndex + 1
		rn.MatchIndex[peerID] = 0
	}

	// Start sending heartbeats
	go rn.sendHeartbeats()
}

// lastLogInfo returns the index and term of the last log entry
func (rn *RaftNode) lastLogInfo() (uint64, uint64) {
	if len(rn.Log) == 0 {
		return 0, 0
	}
	lastEntry := rn.Log[len(rn.Log)-1]
	return lastEntry.Index, lastEntry.Term
}

// isLogUpToDate checks if the candidate's log is at least as up-to-date as this node's log
func (rn *RaftNode) isLogUpToDate(candidateLastLogIndex, candidateLastLogTerm uint64) bool {
	lastLogIndex, lastLogTerm := rn.lastLogInfo()
	
	// If terms are different, the one with higher term is more up-to-date
	if candidateLastLogTerm != lastLogTerm {
		return candidateLastLogTerm > lastLogTerm
	}
	
	// If terms are same, the one with higher index is more up-to-date
	return candidateLastLogIndex >= lastLogIndex
}