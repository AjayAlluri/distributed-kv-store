#!/bin/bash

# Script to start a 3-node Raft cluster for testing

set -e

echo "Starting distributed key-value store cluster..."

# Build the cluster binary
echo "Building cluster binary..."
go build -o bin/cluster cmd/cluster/main.go

# Create data directories
mkdir -p data/cluster/node-1
mkdir -p data/cluster/node-2  
mkdir -p data/cluster/node-3

# Start node-1 (bootstrap node)
echo "Starting node-1 (bootstrap)..."
RAFT_NODE_ID=node-1 ./bin/cluster -config=config/multi-node-cluster.yaml -port=8081 &
NODE1_PID=$!

# Wait a bit for node-1 to start
sleep 2

# Start node-2
echo "Starting node-2..."
RAFT_NODE_ID=node-2 ./bin/cluster -config=config/multi-node-cluster.yaml -port=8082 &
NODE2_PID=$!

# Wait a bit
sleep 2

# Start node-3
echo "Starting node-3..."
RAFT_NODE_ID=node-3 ./bin/cluster -config=config/multi-node-cluster.yaml -port=8083 &
NODE3_PID=$!

# Function to cleanup on exit
cleanup() {
    echo ""
    echo "Shutting down cluster..."
    kill $NODE1_PID $NODE2_PID $NODE3_PID 2>/dev/null || true
    wait $NODE1_PID $NODE2_PID $NODE3_PID 2>/dev/null || true
    echo "Cluster shutdown complete"
}

# Set trap to cleanup on script exit
trap cleanup EXIT

echo ""
echo "Cluster started successfully!"
echo "Node 1: http://localhost:8081 (Raft: localhost:9001)"
echo "Node 2: http://localhost:8082 (Raft: localhost:9002)"  
echo "Node 3: http://localhost:8083 (Raft: localhost:9003)"
echo ""
echo "Try these commands:"
echo "  curl http://localhost:8081/cluster/status"
echo "  curl -X PUT http://localhost:8081/kv/test -d 'hello world'"
echo "  curl http://localhost:8082/kv/test"
echo ""
echo "Press Ctrl+C to stop the cluster"

# Wait for user interrupt
wait