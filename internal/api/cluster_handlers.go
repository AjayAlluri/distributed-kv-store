package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"

	"github.com/ajayalluri/distributed-kv-store/internal/cluster"
)

// ClusterServer extends the basic server with cluster-aware functionality
type ClusterServer struct {
	*Server
	manager *cluster.Manager
	logger  *logrus.Logger
	
	// HTTP client for forwarding requests
	httpClient *http.Client
}

// NewClusterServer creates a new cluster-aware server
func NewClusterServer(manager *cluster.Manager, logger *logrus.Logger) *ClusterServer {
	kvStore := manager.GetKVStore()
	basicServer := NewServer(kvStore)
	
	cs := &ClusterServer{
		Server:  basicServer,
		manager: manager,
		logger:  logger,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
	
	// Override routes with cluster-aware handlers
	cs.setupClusterRoutes()
	
	return cs
}

// setupClusterRoutes sets up cluster-aware routes
func (cs *ClusterServer) setupClusterRoutes() {
	// Clear existing routes and set up new ones
	cs.router = mux.NewRouter()
	
	// KV operations with leader forwarding
	cs.router.HandleFunc("/kv/{key}", cs.handleGetWithForwarding).Methods("GET")
	cs.router.HandleFunc("/kv/{key}", cs.handlePutWithForwarding).Methods("PUT")
	cs.router.HandleFunc("/kv/{key}", cs.handleDeleteWithForwarding).Methods("DELETE")
	
	// Cluster management endpoints
	cs.router.HandleFunc("/cluster/status", cs.handleClusterStatus).Methods("GET")
	cs.router.HandleFunc("/cluster/leader", cs.handleGetLeader).Methods("GET")
	cs.router.HandleFunc("/cluster/nodes", cs.handleGetNodes).Methods("GET")
	
	// Health check
	cs.router.HandleFunc("/health", cs.handleHealth).Methods("GET")
	
	// Root endpoint with cluster info
	cs.router.HandleFunc("/", cs.handleRoot).Methods("GET")
}

// handleGetWithForwarding handles GET requests with optional leader forwarding
func (cs *ClusterServer) handleGetWithForwarding(w http.ResponseWriter, r *http.Request) {
	// GET operations can be served from any node (read from local state)
	cs.Server.handleGet(w, r)
}

// handlePutWithForwarding handles PUT requests with leader forwarding
func (cs *ClusterServer) handlePutWithForwarding(w http.ResponseWriter, r *http.Request) {
	// Check if we're the leader
	if cs.manager.IsLeader() {
		cs.Server.handlePut(w, r)
		return
	}
	
	// Forward to leader
	cs.forwardToLeader(w, r)
}

// handleDeleteWithForwarding handles DELETE requests with leader forwarding
func (cs *ClusterServer) handleDeleteWithForwarding(w http.ResponseWriter, r *http.Request) {
	// Check if we're the leader
	if cs.manager.IsLeader() {
		cs.Server.handleDelete(w, r)
		return
	}
	
	// Forward to leader
	cs.forwardToLeader(w, r)
}

// forwardToLeader forwards a request to the current leader
func (cs *ClusterServer) forwardToLeader(w http.ResponseWriter, r *http.Request) {
	leaderAddress := cs.manager.GetLeaderAddress()
	if leaderAddress == "" {
		cs.logger.Error("No leader available for forwarding")
		http.Error(w, `{"error": "no leader available", "code": "NO_LEADER"}`, http.StatusServiceUnavailable)
		return
	}
	
	// Build the target URL
	targetURL := fmt.Sprintf("http://%s%s", leaderAddress, r.URL.Path)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}
	
	cs.logger.WithFields(logrus.Fields{
		"method":        r.Method,
		"path":          r.URL.Path,
		"leader":        leaderAddress,
		"target_url":    targetURL,
	}).Debug("Forwarding request to leader")
	
	// Copy request body
	var body io.Reader
	if r.Body != nil {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			cs.logger.WithError(err).Error("Failed to read request body")
			http.Error(w, `{"error": "failed to read request body"}`, http.StatusInternalServerError)
			return
		}
		body = bytes.NewReader(bodyBytes)
	}
	
	// Create forwarded request
	req, err := http.NewRequest(r.Method, targetURL, body)
	if err != nil {
		cs.logger.WithError(err).Error("Failed to create forwarded request")
		http.Error(w, `{"error": "failed to create forwarded request"}`, http.StatusInternalServerError)
		return
	}
	
	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	
	// Add forwarding header
	req.Header.Set("X-Forwarded-By", cs.manager.GetLocalNode().ID)
	
	// Send request
	resp, err := cs.httpClient.Do(req)
	if err != nil {
		cs.logger.WithError(err).Error("Failed to forward request to leader")
		http.Error(w, `{"error": "failed to forward request to leader", "code": "FORWARD_FAILED"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	
	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	
	// Set forwarded header
	w.Header().Set("X-Forwarded-From", cs.manager.GetLocalNode().ID)
	
	// Copy status code
	w.WriteHeader(resp.StatusCode)
	
	// Copy response body
	if _, err := io.Copy(w, resp.Body); err != nil {
		cs.logger.WithError(err).Error("Failed to copy forwarded response")
	}
	
	cs.logger.WithFields(logrus.Fields{
		"method":     r.Method,
		"path":       r.URL.Path,
		"leader":     leaderAddress,
		"status":     resp.StatusCode,
	}).Debug("Successfully forwarded request to leader")
}

// handleClusterStatus returns the current cluster status
func (cs *ClusterServer) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	clusterInfo := cs.manager.GetClusterInfo()
	localNode := cs.manager.GetLocalNode()
	
	status := map[string]interface{}{
		"local_node_id": localNode.ID,
		"is_leader":     cs.manager.IsLeader(),
		"cluster_size":  len(clusterInfo),
		"nodes":         clusterInfo,
		"timestamp":     time.Now().UTC(),
	}
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		cs.logger.WithError(err).Error("Failed to encode cluster status")
		http.Error(w, `{"error": "failed to encode cluster status"}`, http.StatusInternalServerError)
	}
}

// handleGetLeader returns information about the current leader
func (cs *ClusterServer) handleGetLeader(w http.ResponseWriter, r *http.Request) {
	leader := cs.manager.GetLeader()
	
	response := map[string]interface{}{
		"leader":    leader,
		"timestamp": time.Now().UTC(),
	}
	
	if leader == nil {
		response["error"] = "no leader elected"
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		cs.logger.WithError(err).Error("Failed to encode leader info")
		http.Error(w, `{"error": "failed to encode leader info"}`, http.StatusInternalServerError)
	}
}

// handleGetNodes returns information about all nodes in the cluster
func (cs *ClusterServer) handleGetNodes(w http.ResponseWriter, r *http.Request) {
	nodes := cs.manager.GetClusterInfo()
	
	response := map[string]interface{}{
		"nodes":     nodes,
		"count":     len(nodes),
		"timestamp": time.Now().UTC(),
	}
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		cs.logger.WithError(err).Error("Failed to encode nodes info")
		http.Error(w, `{"error": "failed to encode nodes info"}`, http.StatusInternalServerError)
	}
}

// handleHealth returns the health status of the local node
func (cs *ClusterServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	localNode := cs.manager.GetLocalNode()
	term, isLeader := localNode.GetState()
	
	health := map[string]interface{}{
		"status":     "healthy",
		"node_id":    localNode.ID,
		"is_leader":  isLeader,
		"term":       term,
		"state":      localNode.State.String(),
		"timestamp":  time.Now().UTC(),
	}
	
	// Add KV store stats if available
	if kvStore := cs.manager.GetKVStore(); kvStore != nil {
		health["kv_stats"] = kvStore.Stats()
	}
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(health); err != nil {
		cs.logger.WithError(err).Error("Failed to encode health status")
		http.Error(w, `{"error": "failed to encode health status"}`, http.StatusInternalServerError)
	}
}

// handleRoot returns basic information about the cluster
func (cs *ClusterServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	localNode := cs.manager.GetLocalNode()
	clusterInfo := cs.manager.GetClusterInfo()
	
	info := map[string]interface{}{
		"service":       "distributed-kv-store",
		"version":       "1.0.0",
		"node_id":       localNode.ID,
		"is_leader":     cs.manager.IsLeader(),
		"cluster_size":  len(clusterInfo),
		"endpoints": map[string]string{
			"kv_operations":   "/kv/{key}",
			"cluster_status":  "/cluster/status",
			"cluster_leader":  "/cluster/leader",
			"cluster_nodes":   "/cluster/nodes",
			"health":          "/health",
		},
		"timestamp": time.Now().UTC(),
	}
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(info); err != nil {
		cs.logger.WithError(err).Error("Failed to encode root info")
		http.Error(w, `{"error": "failed to encode root info"}`, http.StatusInternalServerError)
	}
}

// isWriteOperation checks if the HTTP method is a write operation
func isWriteOperation(method string) bool {
	return strings.ToUpper(method) == "PUT" || 
		   strings.ToUpper(method) == "POST" || 
		   strings.ToUpper(method) == "DELETE"
}