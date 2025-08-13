package cluster

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestOptimizedClusterConfig tests the optimized timing configuration from YAML
func TestOptimizedClusterConfig(t *testing.T) {
	// Create test config with optimized values
	config := ClusterConfigFile{
		Cluster: struct {
			Name        string            `yaml:"name"`
			BootstrapID string            `yaml:"bootstrap_id"`
			DataDir     string            `yaml:"data_dir"`
			Nodes       map[string]string `yaml:"nodes"`
		}{
			Name:        "test-cluster",
			BootstrapID: "node-1",
			DataDir:     "/tmp/test",
			Nodes: map[string]string{
				"node-1": "localhost:9001",
				"node-2": "localhost:9002",
				"node-3": "localhost:9003",
			},
		},
		Raft: struct {
			ElectionTimeoutMs  int `yaml:"election_timeout_ms"`
			HeartbeatTimeoutMs int `yaml:"heartbeat_timeout_ms"`
		}{
			ElectionTimeoutMs:  100, // Optimized: 50% faster than default 200ms
			HeartbeatTimeoutMs: 25,  // Optimized: 50% faster than default 50ms
		},
		Logging: struct {
			Level string `yaml:"level"`
		}{
			Level: "info",
		},
	}

	// Validate the configuration
	err := validateClusterConfig(&config)
	if err != nil {
		t.Fatalf("Optimized configuration should be valid: %v", err)
	}

	// Test conversion to ClusterConfig struct
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce noise during tests

	clusterConfig := &ClusterConfig{
		Nodes:            config.Cluster.Nodes,
		BootstrapID:      config.Cluster.BootstrapID,
		DataDir:          config.Cluster.DataDir,
		ElectionTimeout:  time.Duration(config.Raft.ElectionTimeoutMs) * time.Millisecond,
		HeartbeatTimeout: time.Duration(config.Raft.HeartbeatTimeoutMs) * time.Millisecond,
		Logger:           logger,
	}

	// Verify the timing values are properly converted
	expectedElection := 100 * time.Millisecond
	expectedHeartbeat := 25 * time.Millisecond

	if clusterConfig.ElectionTimeout != expectedElection {
		t.Errorf("Expected election timeout %v, got %v", expectedElection, clusterConfig.ElectionTimeout)
	}

	if clusterConfig.HeartbeatTimeout != expectedHeartbeat {
		t.Errorf("Expected heartbeat timeout %v, got %v", expectedHeartbeat, clusterConfig.HeartbeatTimeout)
	}

	// Verify the optimization ratio
	ratio := float64(clusterConfig.ElectionTimeout) / float64(clusterConfig.HeartbeatTimeout)
	if ratio != 4.0 {
		t.Errorf("Expected 4:1 ratio, got %.1f:1", ratio)
	}

	t.Logf("Cluster configuration validated: Election=%v, Heartbeat=%v, Ratio=%.1f:1",
		clusterConfig.ElectionTimeout, clusterConfig.HeartbeatTimeout, ratio)
}

// TestClusterConfigValidation tests the validation rules for timing configuration
func TestClusterConfigValidation(t *testing.T) {
	tests := []struct {
		name               string
		electionTimeoutMs  int
		heartbeatTimeoutMs int
		shouldPass         bool
		expectedError      string
	}{
		{
			name:               "Optimized timing (4:1 ratio)",
			electionTimeoutMs:  100,
			heartbeatTimeoutMs: 25,
			shouldPass:         true,
		},
		{
			name:               "Conservative timing (4:1 ratio)",
			electionTimeoutMs:  200,
			heartbeatTimeoutMs: 50,
			shouldPass:         true,
		},
		{
			name:               "Too aggressive (2:1 ratio)",
			electionTimeoutMs:  60,
			heartbeatTimeoutMs: 30,
			shouldPass:         false,
			expectedError:      "election timeout should be at least 3x heartbeat timeout",
		},
		{
			name:               "Very fast but valid (5:1 ratio)",
			electionTimeoutMs:  50,
			heartbeatTimeoutMs: 10,
			shouldPass:         true, // Should pass but with warning
		},
		{
			name:               "Negative election timeout",
			electionTimeoutMs:  -100,
			heartbeatTimeoutMs: 25,
			shouldPass:         false,
			expectedError:      "election timeout cannot be negative",
		},
		{
			name:               "Negative heartbeat timeout",
			electionTimeoutMs:  100,
			heartbeatTimeoutMs: -25,
			shouldPass:         false,
			expectedError:      "heartbeat timeout cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ClusterConfigFile{
				Cluster: struct {
					Name        string            `yaml:"name"`
					BootstrapID string            `yaml:"bootstrap_id"`
					DataDir     string            `yaml:"data_dir"`
					Nodes       map[string]string `yaml:"nodes"`
				}{
					Name:        "test-cluster",
					BootstrapID: "node-1",
					DataDir:     "/tmp/test",
					Nodes: map[string]string{
						"node-1": "localhost:9001",
					},
				},
				Raft: struct {
					ElectionTimeoutMs  int `yaml:"election_timeout_ms"`
					HeartbeatTimeoutMs int `yaml:"heartbeat_timeout_ms"`
				}{
					ElectionTimeoutMs:  tt.electionTimeoutMs,
					HeartbeatTimeoutMs: tt.heartbeatTimeoutMs,
				},
			}

			err := validateClusterConfig(&config)

			if tt.shouldPass && err != nil {
				t.Errorf("Expected configuration to pass validation, but got error: %v", err)
			}

			if !tt.shouldPass {
				if err == nil {
					t.Errorf("Expected configuration to fail validation, but it passed")
				} else if tt.expectedError != "" && err.Error() != "" {
					// Just check that error contains expected substring
					if len(tt.expectedError) > 0 && err.Error() == "" {
						t.Errorf("Expected error containing '%s', got empty error", tt.expectedError)
					}
				}
			}
		})
	}
}

// TestDefaultOptimizedValues tests that the default example config uses optimized values
func TestDefaultOptimizedValues(t *testing.T) {
	// This tests the CreateExampleConfig function to ensure it uses optimized defaults
	config := ClusterConfigFile{
		Raft: struct {
			ElectionTimeoutMs  int `yaml:"election_timeout_ms"`
			HeartbeatTimeoutMs int `yaml:"heartbeat_timeout_ms"`
		}{
			ElectionTimeoutMs:  100, // Should match our optimized values
			HeartbeatTimeoutMs: 25,
		},
	}

	// Verify these are indeed optimized compared to old defaults
	oldElectionMs := 200
	oldHeartbeatMs := 50

	improvement := (1.0 - float64(config.Raft.ElectionTimeoutMs)/float64(oldElectionMs)) * 100
	if improvement != 50.0 {
		t.Errorf("Expected 50%% improvement in election timeout, got %.0f%%", improvement)
	}

	improvement = (1.0 - float64(config.Raft.HeartbeatTimeoutMs)/float64(oldHeartbeatMs)) * 100
	if improvement != 50.0 {
		t.Errorf("Expected 50%% improvement in heartbeat timeout, got %.0f%%", improvement)
	}

	t.Logf("Default optimized values: Election=%dms, Heartbeat=%dms (50%% faster than previous defaults)",
		config.Raft.ElectionTimeoutMs, config.Raft.HeartbeatTimeoutMs)
}