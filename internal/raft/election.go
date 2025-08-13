package raft

import (
	"sync"
	"sync/atomic"

	"github.com/sirupsen/logrus"
)

// handleElectionTimeout handles the election timeout event
func (rn *RaftNode) handleElectionTimeout() {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	// Only followers and candidates can start elections
	if rn.State == Leader {
		return
	}

	logrus.WithFields(logrus.Fields{
		"node_id": rn.ID,
		"term":    rn.CurrentTerm,
		"state":   rn.State.String(),
	}).Info("Election timeout occurred")

	rn.becomeCandidate()
}

// startElection initiates a new election
func (rn *RaftNode) startElection() {
	lastLogIndex, lastLogTerm := rn.lastLogInfo()

	// Create vote request
	req := &VoteRequest{
		Term:         rn.CurrentTerm,
		CandidateID:  rn.ID,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}

	logrus.WithFields(logrus.Fields{
		"node_id":        rn.ID,
		"term":           rn.CurrentTerm,
		"last_log_index": lastLogIndex,
		"last_log_term":  lastLogTerm,
		"peers":          len(rn.Peers),
	}).Info("Starting election")

	// Vote for ourselves
	votes := int32(1)
	needed := int32(len(rn.Peers)/2 + 1)

	// Send vote requests to all peers
	var wg sync.WaitGroup
	for peerID, peerAddr := range rn.Peers {
		if peerID == rn.ID {
			continue
		}

		wg.Add(1)
		go func(peer string, addr string) {
			defer wg.Done()
			rn.sendVoteRequest(peer, addr, req, &votes, needed)
		}(peerID, peerAddr)
	}

	// Wait for responses or timeout
	go func() {
		wg.Wait()
		// Check if we won the election
		rn.mu.Lock()
		if rn.State == Candidate && atomic.LoadInt32(&votes) >= needed {
			rn.becomeLeader()
		}
		rn.mu.Unlock()
	}()
}

// sendVoteRequest sends a vote request to a peer
func (rn *RaftNode) sendVoteRequest(peerID, peerAddr string, req *VoteRequest, votes *int32, needed int32) {
	logrus.WithFields(logrus.Fields{
		"node_id": rn.ID,
		"peer":    peerID,
		"peer_addr": peerAddr,
		"term":    req.Term,
	}).Debug("Sending vote request")

	// Use the transport to send the request
	resp, err := rn.Transport.SendVoteRequest(peerAddr, req)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"node_id": rn.ID,
			"peer":    peerID,
			"peer_addr": peerAddr,
			"error":   err,
		}).Error("Failed to send vote request")
		return
	}

	// Handle the response
	if resp != nil {
		logrus.WithFields(logrus.Fields{
			"node_id":      rn.ID,
			"peer":         peerID,
			"vote_granted": resp.VoteGranted,
			"term":         resp.Term,
		}).Debug("Received vote response")

		rn.mu.Lock()
		defer rn.mu.Unlock()

		// Check if we're still a candidate in the same term
		if rn.State != Candidate || rn.CurrentTerm != req.Term {
			return
		}

		// Handle the response
		if resp.Term > rn.CurrentTerm {
			rn.becomeFollower(resp.Term)
			return
		}

		if resp.VoteGranted {
			newVotes := atomic.AddInt32(votes, 1)
			logrus.WithFields(logrus.Fields{
				"node_id": rn.ID,
				"peer":    peerID,
				"votes":   newVotes,
				"needed":  needed,
			}).Debug("Received vote")

			if newVotes >= needed {
				logrus.WithFields(logrus.Fields{
					"node_id": rn.ID,
					"term":    rn.CurrentTerm,
					"votes":   newVotes,
				}).Info("Won election")
				rn.becomeLeader()
			}
		}
	}
}

// HandleVoteRequest processes an incoming vote request synchronously (implements RPCHandler)
func (rn *RaftNode) HandleVoteRequest(req *VoteRequest) *VoteResponse {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"node_id":          rn.ID,
		"from":             req.CandidateID,
		"term":             req.Term,
		"current_term":     rn.CurrentTerm,
		"voted_for":        rn.VotedFor,
		"candidate_log_idx": req.LastLogIndex,
		"candidate_log_term": req.LastLogTerm,
	}).Debug("Received vote request")

	resp := &VoteResponse{
		Term:        rn.CurrentTerm,
		VoteGranted: false,
	}

	// If candidate's term is older, reject
	if req.Term < rn.CurrentTerm {
		logrus.WithFields(logrus.Fields{
			"node_id": rn.ID,
			"reason":  "stale_term",
		}).Debug("Rejecting vote: stale term")
		return resp
	}

	// If candidate's term is newer, become follower
	if req.Term > rn.CurrentTerm {
		rn.becomeFollower(req.Term)
	}

	resp.Term = rn.CurrentTerm

	// Grant vote if:
	// 1. Haven't voted for anyone else in this term
	// 2. Candidate's log is at least as up-to-date as ours
	if (rn.VotedFor == "" || rn.VotedFor == req.CandidateID) &&
		rn.isLogUpToDate(req.LastLogIndex, req.LastLogTerm) {
		
		resp.VoteGranted = true
		rn.VotedFor = req.CandidateID
		rn.resetElectionTimer()
		rn.persistState()

		logrus.WithFields(logrus.Fields{
			"node_id":   rn.ID,
			"candidate": req.CandidateID,
			"term":      req.Term,
		}).Info("Granted vote")
	} else {
		logrus.WithFields(logrus.Fields{
			"node_id":      rn.ID,
			"candidate":    req.CandidateID,
			"voted_for":    rn.VotedFor,
			"log_up_to_date": rn.isLogUpToDate(req.LastLogIndex, req.LastLogTerm),
		}).Debug("Rejected vote")
	}

	return resp
}

// handleVoteRequest handles an incoming vote request via channel (for backward compatibility)
func (rn *RaftNode) handleVoteRequest(req *VoteRequest) {
	// Just call the synchronous handler - response will be ignored in channel-based flow
	rn.HandleVoteRequest(req)
}

// handleVoteResponse handles a vote response
func (rn *RaftNode) handleVoteResponse(resp *VoteResponse) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"node_id":     rn.ID,
		"term":        resp.Term,
		"vote_granted": resp.VoteGranted,
		"current_term": rn.CurrentTerm,
		"state":       rn.State.String(),
	}).Debug("Received vote response")

	// If response term is newer, become follower
	if resp.Term > rn.CurrentTerm {
		rn.becomeFollower(resp.Term)
		return
	}

	// Only process if we're still a candidate
	if rn.State != Candidate || resp.Term != rn.CurrentTerm {
		return
	}

	// Vote responses are handled in sendVoteRequest for simplicity
	// In a real implementation, this would aggregate votes
}