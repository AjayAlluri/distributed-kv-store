package raft

import (
	"fmt"
	"sync"
	"time"
)

// NodeState represents the three possible states of a Raft node
type NodeState int

const (
	Follower NodeState = iota
	Candidate
	Leader
)

func (s NodeState) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	default:
		return "Unknown"
	}
}

// LogEntry represents a single entry in the Raft log
type LogEntry struct {
	Term    uint64      `json:"term"`    // Term when entry was received by leader
	Index   uint64      `json:"index"`   // Position in the log
	Command interface{} `json:"command"` // State machine command (key-value operations)
	Data    []byte      `json:"data"`    // Serialized command data
}

// PersistentState represents the persistent state that must survive restarts
type PersistentState struct {
	CurrentTerm uint64     `json:"current_term"` // Latest term server has seen
	VotedFor    string     `json:"voted_for"`    // CandidateId that received vote in current term
	Log         []LogEntry `json:"log"`          // Log entries
}

// VolatileState represents the volatile state on all servers
type VolatileState struct {
	CommitIndex uint64 // Index of highest log entry known to be committed
	LastApplied uint64 // Index of highest log entry applied to state machine
}

// LeaderState represents the volatile state on leaders (reinitialized after election)
type LeaderState struct {
	NextIndex  map[string]uint64 // For each server, index of next log entry to send
	MatchIndex map[string]uint64 // For each server, index of highest log entry known to be replicated
}

// RaftNode represents a single node in the Raft cluster
type RaftNode struct {
	// Node identification
	ID      string            // Unique identifier for this node
	Address string            // Network address for this node
	Peers   map[string]string // Node ID -> Address mapping

	// Current state
	mu          sync.RWMutex
	State       NodeState
	CurrentTerm uint64
	VotedFor    string
	LeaderID    string     // ID of the current leader (empty if unknown)
	Log         []LogEntry

	// Volatile state
	CommitIndex uint64
	LastApplied uint64

	// Leader state (only valid when State == Leader)
	NextIndex  map[string]uint64
	MatchIndex map[string]uint64

	// Election timing
	ElectionTimeout  time.Duration
	HeartbeatTimeout time.Duration
	LastHeartbeat    time.Time
	ElectionTimer    *time.Timer

	// Channels for communication
	VoteRequestCh  chan *VoteRequest
	VoteResponseCh chan *VoteResponse
	AppendEntriesCh chan *AppendEntriesRequest
	AppendRespCh    chan *AppendEntriesResponse
	ShutdownCh      chan struct{}

	// Apply channel for committed entries
	ApplyCh chan ApplyMsg

	// Storage for persistence
	Storage PersistentStorage
	
	// Transport for network communication
	Transport RPCTransport
}

// ApplyMsg represents a message to apply to the state machine
type ApplyMsg struct {
	CommandValid bool        // True if this is a regular command
	Command      interface{} // The command to apply
	CommandIndex uint64      // The log index of the command
	
	// Snapshot fields (for future implementation)
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  uint64
	SnapshotIndex uint64
}

// PersistentStorage interface for persisting Raft state
type PersistentStorage interface {
	SaveState(state *PersistentState) error
	LoadState() (*PersistentState, error)
	SaveSnapshot(snapshot []byte, lastIncludedIndex, lastIncludedTerm uint64) error
	LoadSnapshot() ([]byte, uint64, uint64, error)
}

// Configuration for Raft node
type Config struct {
	NodeID           string
	Address          string
	Peers            map[string]string // Node ID -> Address mapping
	ElectionTimeout  time.Duration
	HeartbeatTimeout time.Duration
	Storage          PersistentStorage
	ApplyCh          chan ApplyMsg
	Transport        RPCTransport
}

// Default configuration values
const (
	DefaultElectionTimeoutMin  = 150 * time.Millisecond
	DefaultElectionTimeoutMax  = 300 * time.Millisecond
	DefaultHeartbeatTimeout    = 50 * time.Millisecond
	DefaultMaxLogEntriesPerReq = 100
)

// Common errors
var (
	ErrNodeNotFound      = fmt.Errorf("node not found")
	ErrElectionTimeout   = fmt.Errorf("election timeout")
	ErrNotLeader         = fmt.Errorf("not leader")
	ErrInvalidTerm       = fmt.Errorf("invalid term")
)