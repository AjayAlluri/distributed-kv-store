package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Storage  StorageConfig  `yaml:"storage"`
	Raft     RaftConfig     `yaml:"raft"`
	Logging  LoggingConfig  `yaml:"logging"`
	Cluster  ClusterConfig  `yaml:"cluster"`
}

type ServerConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	ReadTimeout  string `yaml:"read_timeout"`
	WriteTimeout string `yaml:"write_timeout"`
}

type StorageConfig struct {
	DataDir    string `yaml:"data_dir"`
	SyncWrites bool   `yaml:"sync_writes"`
}

type RaftConfig struct {
	NodeID           string `yaml:"node_id"`
	ElectionTimeout  string `yaml:"election_timeout"`
	HeartbeatTimeout string `yaml:"heartbeat_timeout"`
	SnapshotInterval string `yaml:"snapshot_interval"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	File   string `yaml:"file"`
}

type ClusterConfig struct {
	Peers     []PeerConfig `yaml:"peers"`
	Bootstrap bool         `yaml:"bootstrap"`
}

type PeerConfig struct {
	NodeID  string `yaml:"node_id"`
	Address string `yaml:"address"`
	Port    int    `yaml:"port"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:         "0.0.0.0",
			Port:         8080,
			ReadTimeout:  "10s",
			WriteTimeout: "10s",
		},
		Storage: StorageConfig{
			DataDir:    "./data",
			SyncWrites: true,
		},
		Raft: RaftConfig{
			NodeID:           "node-1",
			ElectionTimeout:  "1s",
			HeartbeatTimeout: "500ms",
			SnapshotInterval: "30s",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
			File:   "",
		},
		Cluster: ClusterConfig{
			Peers:     []PeerConfig{},
			Bootstrap: true,
		},
	}
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(configPath string) (*Config, error) {
	config := DefaultConfig()

	if configPath == "" {
		return config, nil
	}

	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", configPath)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate and set defaults
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return config, nil
}

// LoadConfigFromEnv loads configuration from environment variables
func LoadConfigFromEnv() (*Config, error) {
	config := DefaultConfig()

	if host := os.Getenv("KV_SERVER_HOST"); host != "" {
		config.Server.Host = host
	}

	if port := os.Getenv("KV_SERVER_PORT"); port != "" {
		var p int
		if _, err := fmt.Sscanf(port, "%d", &p); err == nil {
			config.Server.Port = p
		}
	}

	if dataDir := os.Getenv("KV_DATA_DIR"); dataDir != "" {
		config.Storage.DataDir = dataDir
	}

	if nodeID := os.Getenv("KV_NODE_ID"); nodeID != "" {
		config.Raft.NodeID = nodeID
	}

	if logLevel := os.Getenv("KV_LOG_LEVEL"); logLevel != "" {
		config.Logging.Level = logLevel
	}

	return config, config.validate()
}

// SaveConfig saves the configuration to a YAML file
func (c *Config) SaveConfig(configPath string) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// validate validates the configuration
func (c *Config) validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Storage.DataDir == "" {
		return fmt.Errorf("data directory cannot be empty")
	}

	if c.Raft.NodeID == "" {
		return fmt.Errorf("node ID cannot be empty")
	}

	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}

	if !validLogLevels[c.Logging.Level] {
		return fmt.Errorf("invalid log level: %s", c.Logging.Level)
	}

	return nil
}

// GetServerAddress returns the server address as host:port
func (c *Config) GetServerAddress() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// GetDataPath returns the absolute path to the data directory
func (c *Config) GetDataPath() (string, error) {
	return filepath.Abs(c.Storage.DataDir)
}