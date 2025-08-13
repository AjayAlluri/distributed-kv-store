package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

type KVStore interface {
	Get(key string) (string, error)
	Put(key, value string) error
	Delete(key string) error
	Close() error
}

type Server struct {
	store  KVStore
	router *mux.Router
}

func NewServer(store KVStore) *Server {
	s := &Server{
		store:  store,
		router: mux.NewRouter(),
	}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.router.HandleFunc("/kv/{key}", s.handleGet).Methods("GET")
	s.router.HandleFunc("/kv/{key}", s.handlePut).Methods("PUT")
	s.router.HandleFunc("/kv/{key}", s.handleDelete).Methods("DELETE")
	s.router.HandleFunc("/status", s.handleStatus).Methods("GET")
	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]
	
	if key == "" {
		http.Error(w, `{"error": "Key is required", "code": "MISSING_KEY"}`, http.StatusBadRequest)
		return
	}

	value, err := s.store.Get(key)
	if err != nil {
		if err.Error() == "key not found" {
			errorResponse := map[string]interface{}{
				"error": "Key not found",
				"code":  "KEY_NOT_FOUND",
				"key":   key,
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(errorResponse)
			return
		}
		
		errorResponse := map[string]interface{}{
			"error": "Failed to retrieve key",
			"code":  "GET_ERROR", 
			"details": map[string]interface{}{
				"message": err.Error(),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse)
		return
	}

	response := map[string]interface{}{
		"key":   key,
		"value": value,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]
	
	if key == "" {
		http.Error(w, `{"error": "Key is required", "code": "MISSING_KEY"}`, http.StatusBadRequest)
		return
	}

	// Check Content-Type header
	contentType := r.Header.Get("Content-Type")
	if contentType != "" && !strings.Contains(contentType, "application/json") {
		http.Error(w, `{"error": "Content-Type must be application/json", "code": "INVALID_CONTENT_TYPE"}`, http.StatusBadRequest)
		return
	}

	// Read and validate request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error": "Failed to read request body", "code": "READ_ERROR"}`, http.StatusBadRequest)
		return
	}
	
	if len(body) == 0 {
		http.Error(w, `{"error": "Request body is required", "code": "EMPTY_BODY"}`, http.StatusBadRequest)
		return
	}

	var request struct {
		Value string `json:"value"`
	}

	if err := json.Unmarshal(body, &request); err != nil {
		// Provide detailed error information
		errorResponse := map[string]interface{}{
			"error": "Invalid JSON format",
			"code":  "INVALID_JSON",
			"details": map[string]interface{}{
				"message":     err.Error(),
				"body_length": len(body),
				"body_preview": string(body[:min(len(body), 100)]), // First 100 chars
			},
		}
		
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse)
		return
	}

	if request.Value == "" {
		http.Error(w, `{"error": "Value is required and cannot be empty", "code": "EMPTY_VALUE"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.Put(key, request.Value); err != nil {
		errorResponse := map[string]interface{}{
			"error": "Failed to store key-value pair",
			"code":  "STORE_ERROR",
			"details": map[string]interface{}{
				"message": err.Error(),
			},
		}
		
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse)
		return
	}

	response := map[string]interface{}{
		"key":     key,
		"value":   request.Value,
		"message": "Key-value pair stored successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]
	
	if key == "" {
		http.Error(w, `{"error": "Key is required", "code": "MISSING_KEY"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.Delete(key); err != nil {
		if err.Error() == "key not found" {
			errorResponse := map[string]interface{}{
				"error": "Key not found",
				"code":  "KEY_NOT_FOUND",
				"key":   key,
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(errorResponse)
			return
		}
		
		errorResponse := map[string]interface{}{
			"error": "Failed to delete key",
			"code":  "DELETE_ERROR",
			"details": map[string]interface{}{
				"message": err.Error(),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse)
		return
	}

	response := map[string]interface{}{
		"key":     key,
		"message": "Key deleted successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"server":    "distributed-kv-store",
		"version":   "1.0.0",
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"role":      "leader", // Will be dynamic in Raft implementation
		"node_id":   "node-1", // Will be configurable
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"healthy":   true,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func extractKeyFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) >= 3 && parts[1] == "kv" {
		return parts[2]
	}
	return ""
}