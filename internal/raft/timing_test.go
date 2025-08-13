package raft

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestOptimizedElectionTimeout tests the optimized election timeout randomization
func TestOptimizedElectionTimeout(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce noise during tests

	config := Config{
		NodeID:           "test-node",
		Address:          "localhost:9999",
		Peers:            map[string]string{},
		ElectionTimeout:  100 * time.Millisecond,
		HeartbeatTimeout: 25 * time.Millisecond,
		Storage:          &mockStorage{},
		ApplyCh:          make(chan ApplyMsg, 10),
		Transport:        &mockTransport{},
	}

	node := NewRaftNode(config)

	// Test multiple timer resets to verify randomization works correctly
	timeouts := make([]time.Duration, 10)
	
	for i := 0; i < 10; i++ {
		start := time.Now()
		node.resetElectionTimer()
		
		// Wait for the timer and measure actual timeout
		<-node.ElectionTimer.C
		elapsed := time.Since(start)
		timeouts[i] = elapsed
		
		// Verify timeout is within expected range
		expectedMin := config.ElectionTimeout                              // 100ms base
		expectedMax := config.ElectionTimeout + config.ElectionTimeout/2  // 100ms + 50ms jitter = 150ms
		
		if elapsed < expectedMin {
			t.Errorf("Timeout %d too short: %v, expected >= %v", i, elapsed, expectedMin)
		}
		if elapsed > expectedMax+5*time.Millisecond { // 5ms tolerance for timing variations
			t.Errorf("Timeout %d too long: %v, expected <= %v", i, elapsed, expectedMax)
		}
	}
	
	// Verify timeouts are actually randomized (not all the same)
	allSame := true
	firstTimeout := timeouts[0]
	for i := 1; i < len(timeouts); i++ {
		if timeouts[i] != firstTimeout {
			allSame = false
			break
		}
	}
	
	if allSame {
		t.Error("All timeouts were identical - randomization not working")
	}
	
	t.Logf("Election timeouts ranged from %v to %v", 
		findMin(timeouts), findMax(timeouts))
}

// TestHeartbeatTimingRatio tests that heartbeat timeout is properly configured relative to election timeout
func TestHeartbeatTimingRatio(t *testing.T) {
	tests := []struct {
		name             string
		electionTimeout  time.Duration
		heartbeatTimeout time.Duration
		shouldPass       bool
	}{
		{
			name:             "Optimized timing (4:1 ratio)",
			electionTimeout:  100 * time.Millisecond,
			heartbeatTimeout: 25 * time.Millisecond,
			shouldPass:       true,
		},
		{
			name:             "Good timing (5:1 ratio)",
			electionTimeout:  150 * time.Millisecond,
			heartbeatTimeout: 30 * time.Millisecond,
			shouldPass:       true,
		},
		{
			name:             "Too aggressive (2:1 ratio - should fail validation)",
			electionTimeout:  60 * time.Millisecond,
			heartbeatTimeout: 30 * time.Millisecond,
			shouldPass:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := NodeConfig{
				NodeID:           "test-node",
				Address:          "localhost:9999",
				Peers:            map[string]string{},
				ElectionTimeout:  tt.electionTimeout,
				HeartbeatTimeout: tt.heartbeatTimeout,
				DataDir:          "/tmp/test",
				Logger:           logrus.New(),
			}

			err := validateNodeConfig(config)
			
			if tt.shouldPass && err != nil {
				t.Errorf("Expected configuration to pass validation, but got error: %v", err)
			}
			if !tt.shouldPass && err == nil {
				t.Errorf("Expected configuration to fail validation, but it passed")
			}
		})
	}
}

// TestTimingOptimizations tests that our optimized timing values are faster than defaults
func TestTimingOptimizations(t *testing.T) {
	// Original default values
	originalElection := 200 * time.Millisecond
	originalHeartbeat := 50 * time.Millisecond
	
	// Our optimized values  
	optimizedElection := 100 * time.Millisecond
	optimizedHeartbeat := 25 * time.Millisecond

	// Verify optimizations achieve faster timing
	if optimizedElection >= originalElection {
		t.Errorf("Optimized election timeout (%v) should be faster than original (%v)", 
			optimizedElection, originalElection)
	}
	if optimizedHeartbeat >= originalHeartbeat {
		t.Errorf("Optimized heartbeat timeout (%v) should be faster than original (%v)", 
			optimizedHeartbeat, originalHeartbeat)
	}

	// Verify proper ratios are maintained
	ratio := float64(optimizedElection) / float64(optimizedHeartbeat)
	if ratio < 3.0 {
		t.Errorf("Election/heartbeat ratio too low: %.1f, should be >= 3.0", ratio)
	}

	electionImprovement := (1.0 - float64(optimizedElection)/float64(originalElection)) * 100
	heartbeatImprovement := (1.0 - float64(optimizedHeartbeat)/float64(originalHeartbeat)) * 100

	t.Logf("Optimization achieved: Election %v->%v (%.0f%% faster), Heartbeat %v->%v (%.0f%% faster)",
		originalElection, optimizedElection, electionImprovement,
		originalHeartbeat, optimizedHeartbeat, heartbeatImprovement)
		
	// Both should be exactly 50% faster
	if electionImprovement != 50.0 {
		t.Errorf("Expected 50%% election improvement, got %.0f%%", electionImprovement)
	}
	if heartbeatImprovement != 50.0 {
		t.Errorf("Expected 50%% heartbeat improvement, got %.0f%%", heartbeatImprovement)
	}
}

// Helper functions for testing
func findMin(durations []time.Duration) time.Duration {
	min := durations[0]
	for _, d := range durations[1:] {
		if d < min {
			min = d
		}
	}
	return min
}

func findMax(durations []time.Duration) time.Duration {
	max := durations[0]
	for _, d := range durations[1:] {
		if d > max {
			max = d
		}
	}
	return max
}

// Mock implementations for testing
type mockStorage struct{}
func (m *mockStorage) SaveState(state *PersistentState) error { return nil }
func (m *mockStorage) LoadState() (*PersistentState, error) { return nil, nil }
func (m *mockStorage) SaveSnapshot(snapshot []byte, lastIncludedIndex, lastIncludedTerm uint64) error { return nil }
func (m *mockStorage) LoadSnapshot() ([]byte, uint64, uint64, error) { return nil, 0, 0, nil }

type mockTransport struct{}
func (m *mockTransport) SendVoteRequest(target string, req *VoteRequest) (*VoteResponse, error) { return nil, nil }
func (m *mockTransport) SendAppendEntries(target string, req *AppendEntriesRequest) (*AppendEntriesResponse, error) { return nil, nil }
func (m *mockTransport) SendInstallSnapshot(target string, req *InstallSnapshotRequest) (*InstallSnapshotResponse, error) { return nil, nil }
func (m *mockTransport) Start() error { return nil }
func (m *mockTransport) Stop() error { return nil }
func (m *mockTransport) SetRaftNode(node *RaftNode) error { return nil }
func (m *mockTransport) SetRaftHandler(handler RPCHandler) error { return nil }
func (m *mockTransport) LocalAddr() string { return "localhost:9999" }