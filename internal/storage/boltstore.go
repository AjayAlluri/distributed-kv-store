package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.etcd.io/bbolt"
)

const defaultBucket = "kv_store"

type BoltStore struct {
	db   *bbolt.DB
	path string
}

func NewBoltStore(dataDir string) (*BoltStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "kv.db")
	
	db, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open BoltDB: %w", err)
	}

	store := &BoltStore{
		db:   db,
		path: dbPath,
	}

	// Create the default bucket if it doesn't exist
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(defaultBucket))
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	return store, nil
}

func (bs *BoltStore) Get(key string) (string, error) {
	if key == "" {
		return "", errors.New("key cannot be empty")
	}

	var value string
	err := bs.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return errors.New("bucket not found")
		}

		v := bucket.Get([]byte(key))
		if v == nil {
			return errors.New("key not found")
		}

		value = string(v)
		return nil
	})

	if err != nil {
		return "", err
	}

	return value, nil
}

func (bs *BoltStore) Put(key, value string) error {
	if key == "" {
		return errors.New("key cannot be empty")
	}

	return bs.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return errors.New("bucket not found")
		}

		return bucket.Put([]byte(key), []byte(value))
	})
}

func (bs *BoltStore) Delete(key string) error {
	if key == "" {
		return errors.New("key cannot be empty")
	}

	return bs.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return errors.New("bucket not found")
		}

		// Check if key exists before deleting
		if bucket.Get([]byte(key)) == nil {
			return errors.New("key not found")
		}

		return bucket.Delete([]byte(key))
	})
}

func (bs *BoltStore) Close() error {
	return bs.db.Close()
}

func (bs *BoltStore) Path() string {
	return bs.path
}

// GetAll returns all key-value pairs (useful for debugging/status)
func (bs *BoltStore) GetAll() (map[string]string, error) {
	result := make(map[string]string)

	err := bs.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return errors.New("bucket not found")
		}

		return bucket.ForEach(func(k, v []byte) error {
			result[string(k)] = string(v)
			return nil
		})
	})

	return result, err
}

// Stats returns basic statistics about the database
func (bs *BoltStore) Stats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	err := bs.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return errors.New("bucket not found")
		}

		bucketStats := bucket.Stats()
		stats["key_count"] = bucketStats.KeyN
		stats["bucket_stats"] = bucketStats
		stats["db_path"] = bs.path

		return nil
	})

	if err != nil {
		return nil, err
	}

	return stats, nil
}