package raft

// VoteRequest represents the RequestVote RPC request
type VoteRequest struct {
	Term         uint64 `json:"term"`          // Candidate's term
	CandidateID  string `json:"candidate_id"`  // Candidate requesting vote
	LastLogIndex uint64 `json:"last_log_index"` // Index of candidate's last log entry
	LastLogTerm  uint64 `json:"last_log_term"`  // Term of candidate's last log entry
}

// VoteResponse represents the RequestVote RPC response
type VoteResponse struct {
	Term        uint64 `json:"term"`         // CurrentTerm, for candidate to update itself
	VoteGranted bool   `json:"vote_granted"` // True means candidate received vote
}

// AppendEntriesRequest represents the AppendEntries RPC request
type AppendEntriesRequest struct {
	Term         uint64     `json:"term"`          // Leader's term
	LeaderID     string     `json:"leader_id"`     // So follower can redirect clients
	PrevLogIndex uint64     `json:"prev_log_index"` // Index of log entry immediately preceding new ones
	PrevLogTerm  uint64     `json:"prev_log_term"`  // Term of prevLogIndex entry
	Entries      []LogEntry `json:"entries"`       // Log entries to store (empty for heartbeat)
	LeaderCommit uint64     `json:"leader_commit"` // Leader's commitIndex
}

// AppendEntriesResponse represents the AppendEntries RPC response
type AppendEntriesResponse struct {
	Term    uint64 `json:"term"`    // CurrentTerm, for leader to update itself
	Success bool   `json:"success"` // True if follower contained entry matching prevLogIndex and prevLogTerm
	
	// Optimization fields for faster log replication
	ConflictTerm  uint64 `json:"conflict_term,omitempty"`  // Term of conflicting entry
	ConflictIndex uint64 `json:"conflict_index,omitempty"` // First index with ConflictTerm
}

// InstallSnapshotRequest represents the InstallSnapshot RPC request (for future implementation)
type InstallSnapshotRequest struct {
	Term              uint64 `json:"term"`               // Leader's term
	LeaderID          string `json:"leader_id"`          // So follower can redirect clients
	LastIncludedIndex uint64 `json:"last_included_index"` // The snapshot replaces all entries up through this index
	LastIncludedTerm  uint64 `json:"last_included_term"`  // Term of lastIncludedIndex
	Offset            uint64 `json:"offset"`             // Byte offset where chunk is positioned in snapshot file
	Data              []byte `json:"data"`               // Raw bytes of snapshot chunk
	Done              bool   `json:"done"`               // True if this is the last chunk
}

// InstallSnapshotResponse represents the InstallSnapshot RPC response
type InstallSnapshotResponse struct {
	Term uint64 `json:"term"` // CurrentTerm, for leader to update itself
}

// ClientRequest represents a client request to the leader
type ClientRequest struct {
	Command interface{} `json:"command"` // The command to execute
	ID      string      `json:"id"`      // Unique request ID for deduplication
}

// ClientResponse represents the response to a client request
type ClientResponse struct {
	Success     bool        `json:"success"`      // Whether the command was successfully applied
	Response    interface{} `json:"response"`     // The result of applying the command
	LeaderHint  string      `json:"leader_hint"`  // Hint about who the leader might be
	Error       string      `json:"error"`        // Error message if any
}

// RPCTransport interface for sending Raft RPCs between nodes
type RPCTransport interface {
	// SendVoteRequest sends a vote request to the specified node
	SendVoteRequest(nodeAddr string, req *VoteRequest) (*VoteResponse, error)
	
	// SendAppendEntries sends an append entries request to the specified node
	SendAppendEntries(nodeAddr string, req *AppendEntriesRequest) (*AppendEntriesResponse, error)
	
	// SendInstallSnapshot sends a snapshot to the specified node
	SendInstallSnapshot(nodeAddr string, req *InstallSnapshotRequest) (*InstallSnapshotResponse, error)
	
	// Start starts the transport layer
	Start() error
	
	// Stop stops the transport layer
	Stop() error
	
	// LocalAddr returns the local address of this transport
	LocalAddr() string
	
	// SetRaftNode connects the transport to a Raft node for handling incoming requests
	SetRaftNode(node *RaftNode) error
}

// StateMachine interface for applying committed log entries
type StateMachine interface {
	// Apply applies a command to the state machine and returns the result
	Apply(command interface{}) (interface{}, error)
	
	// Snapshot creates a snapshot of the current state
	Snapshot() ([]byte, error)
	
	// Restore restores the state machine from a snapshot
	Restore(snapshot []byte) error
}