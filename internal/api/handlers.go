package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

type KVStore interface {
	Get(key string) (string, error)
	Put(key, value string) error
	Delete(key string) error
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
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	value, err := s.store.Get(key)
	if err != nil {
		if err.Error() == "key not found" {
			http.Error(w, "Key not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Internal server error: %v", err), http.StatusInternalServerError)
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
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	var request struct {
		Value string `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if request.Value == "" {
		http.Error(w, "Value is required", http.StatusBadRequest)
		return
	}

	if err := s.store.Put(key, request.Value); err != nil {
		http.Error(w, fmt.Sprintf("Internal server error: %v", err), http.StatusInternalServerError)
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

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]
	
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	if err := s.store.Delete(key); err != nil {
		if err.Error() == "key not found" {
			http.Error(w, "Key not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Internal server error: %v", err), http.StatusInternalServerError)
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