package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ajayalluri/distributed-kv-store/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgreSQLStore struct {
	pool *pgxpool.Pool
	ctx  context.Context
}

// NewPostgreSQLStore creates a new PostgreSQL-backed key-value store
func NewPostgreSQLStore(pgConfig config.PostgreSQLConfig) (*PostgreSQLStore, error) {
	// Build connection string
	connStr := buildConnectionString(pgConfig)
	
	// Parse connection pool configuration
	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Set connection pool settings
	if pgConfig.MaxConns > 0 {
		poolConfig.MaxConns = int32(pgConfig.MaxConns)
	} else {
		poolConfig.MaxConns = 10 // Default
	}

	if pgConfig.MinConns > 0 {
		poolConfig.MinConns = int32(pgConfig.MinConns)
	} else {
		poolConfig.MinConns = 2 // Default
	}

	if pgConfig.MaxConnTime != "" {
		if maxLifetime, err := time.ParseDuration(pgConfig.MaxConnTime); err == nil {
			poolConfig.MaxConnLifetime = maxLifetime
		}
	}

	if pgConfig.MaxIdleTime != "" {
		if maxIdleTime, err := time.ParseDuration(pgConfig.MaxIdleTime); err == nil {
			poolConfig.MaxConnIdleTime = maxIdleTime
		}
	}

	ctx := context.Background()

	// Create connection pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	store := &PostgreSQLStore{
		pool: pool,
		ctx:  ctx,
	}

	// Test connection and create table
	if err := store.initialize(); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	return store, nil
}

func buildConnectionString(pgConfig config.PostgreSQLConfig) string {
	sslMode := pgConfig.SSLMode
	if sslMode == "" {
		sslMode = "prefer"
	}

	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		pgConfig.Host,
		pgConfig.Port,
		pgConfig.Database,
		pgConfig.Username,
		pgConfig.Password,
		sslMode,
	)
}

func (ps *PostgreSQLStore) initialize() error {
	// Test connection
	if err := ps.pool.Ping(ps.ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Create table if not exists
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS kv_store (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);
		
		CREATE INDEX IF NOT EXISTS idx_kv_store_updated_at ON kv_store(updated_at);
	`

	_, err := ps.pool.Exec(ps.ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	return nil
}

// Get retrieves a value by key
func (ps *PostgreSQLStore) Get(key string) (string, error) {
	if key == "" {
		return "", errors.New("key cannot be empty")
	}

	var value string
	query := "SELECT value FROM kv_store WHERE key = $1"
	
	err := ps.pool.QueryRow(ps.ctx, query, key).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errors.New("key not found")
		}
		return "", fmt.Errorf("failed to get value: %w", err)
	}

	return value, nil
}

// Put stores a key-value pair
func (ps *PostgreSQLStore) Put(key, value string) error {
	if key == "" {
		return errors.New("key cannot be empty")
	}

	query := `
		INSERT INTO kv_store (key, value, updated_at) 
		VALUES ($1, $2, NOW()) 
		ON CONFLICT (key) 
		DO UPDATE SET 
			value = EXCLUDED.value,
			updated_at = NOW()
	`

	_, err := ps.pool.Exec(ps.ctx, query, key, value)
	if err != nil {
		return fmt.Errorf("failed to store key-value pair: %w", err)
	}

	return nil
}

// Delete removes a key-value pair
func (ps *PostgreSQLStore) Delete(key string) error {
	if key == "" {
		return errors.New("key cannot be empty")
	}

	query := "DELETE FROM kv_store WHERE key = $1"
	
	result, err := ps.pool.Exec(ps.ctx, query, key)
	if err != nil {
		return fmt.Errorf("failed to delete key: %w", err)
	}

	if result.RowsAffected() == 0 {
		return errors.New("key not found")
	}

	return nil
}

// GetAll returns all key-value pairs (useful for debugging/status)
func (ps *PostgreSQLStore) GetAll() (map[string]string, error) {
	query := "SELECT key, value FROM kv_store ORDER BY key"
	
	rows, err := ps.pool.Query(ps.ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all keys: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		result[key] = value
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return result, nil
}

// Stats returns database statistics
func (ps *PostgreSQLStore) Stats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get row count
	var count int64
	err := ps.pool.QueryRow(ps.ctx, "SELECT COUNT(*) FROM kv_store").Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("failed to get row count: %w", err)
	}

	// Get connection pool stats
	poolStats := ps.pool.Stat()

	stats["key_count"] = count
	stats["pool_total_conns"] = poolStats.TotalConns()
	stats["pool_idle_conns"] = poolStats.IdleConns()
	stats["pool_acquired_conns"] = poolStats.AcquiredConns()
	stats["pool_constructing_conns"] = poolStats.ConstructingConns()
	stats["pool_acquire_count"] = poolStats.AcquireCount()
	stats["pool_acquire_duration"] = poolStats.AcquireDuration().String()
	stats["pool_empty_acquire_count"] = poolStats.EmptyAcquireCount()
	stats["pool_canceled_acquire_count"] = poolStats.CanceledAcquireCount()

	return stats, nil
}

// Close closes the database connection pool
func (ps *PostgreSQLStore) Close() error {
	ps.pool.Close()
	return nil
}

// Ping tests the database connection
func (ps *PostgreSQLStore) Ping() error {
	return ps.pool.Ping(ps.ctx)
}

// GetConnectionCount returns current connection count
func (ps *PostgreSQLStore) GetConnectionCount() int32 {
	return ps.pool.Stat().TotalConns()
}