#!/bin/bash
# Database inspection utility for distributed key-value store

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
BASE_URL="http://localhost:8081"
LEADER_PORT=""

# Function to print colored output
print_header() {
    echo -e "${BLUE}=== $1 ===${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

# Function to find the leader
find_leader() {
    print_header "Finding Cluster Leader"
    
    for port in 8081 8082 8083; do
        echo "Checking node at port $port..."
        
        # Check if node is responding
        if curl -s "http://localhost:$port/health" > /dev/null 2>&1; then
            # Check if this node is the leader
            status=$(curl -s "http://localhost:$port/cluster/status" 2>/dev/null)
            if echo "$status" | grep -q '"is_leader":true'; then
                LEADER_PORT=$port
                print_success "Leader found at port $port"
                return 0
            else
                echo "  Node at $port is follower"
            fi
        else
            print_warning "Node at port $port is not responding"
        fi
    done
    
    if [ -z "$LEADER_PORT" ]; then
        print_error "No leader found! Cluster may be down."
        return 1
    fi
}

# Function to show cluster status
show_cluster_status() {
    print_header "Cluster Status"
    
    if [ -z "$LEADER_PORT" ]; then
        find_leader || return 1
    fi
    
    curl -s "http://localhost:$LEADER_PORT/cluster/status" | jq '.' 2>/dev/null || {
        print_error "Failed to get cluster status"
        return 1
    }
}

# Function to show all key-value pairs
show_all_data() {
    print_header "All Key-Value Pairs"
    
    if [ -z "$LEADER_PORT" ]; then
        find_leader || return 1
    fi
    
    # Try to get some sample data by testing common keys
    echo "Attempting to retrieve stored data..."
    
    # For now, we'll need to test specific keys since there's no "list all" endpoint
    # Let's try some common test keys
    test_keys=("test" "key1" "hello" "example" "data" "foo" "bar")
    
    found_data=false
    for key in "${test_keys[@]}"; do
        response=$(curl -s "http://localhost:$LEADER_PORT/kv/$key" 2>/dev/null)
        if echo "$response" | jq -e '.key' > /dev/null 2>&1; then
            echo "$response" | jq '.'
            found_data=true
        fi
    done
    
    if [ "$found_data" = false ]; then
        print_warning "No data found with common test keys. Try specific keys or add some data first."
        echo ""
        echo "To add data:"
        echo "  curl -X PUT http://localhost:$LEADER_PORT/kv/mykey -H 'Content-Type: application/json' -d '{\"value\":\"myvalue\"}'"
        echo ""
        echo "To retrieve data:"
        echo "  curl http://localhost:$LEADER_PORT/kv/mykey"
    fi
}

# Function to show Raft state files
show_raft_state() {
    print_header "Raft State Files"
    
    for node in node-1 node-2 node-3; do
        echo "=== $node ==="
        if [ -f "data/cluster/$node/raft_state.json" ]; then
            cat "data/cluster/$node/raft_state.json" | jq '.' 2>/dev/null || cat "data/cluster/$node/raft_state.json"
        else
            print_warning "Raft state file not found for $node"
        fi
        echo ""
    done
}

# Function to show BoltDB contents (if available)
show_boltdb_info() {
    print_header "BoltDB File Information"
    
    if [ -f "data/kv.db" ]; then
        echo "BoltDB file exists: data/kv.db"
        ls -lh data/kv.db
        echo ""
        print_warning "BoltDB is a binary format. Use the HTTP API to view contents."
    else
        print_warning "BoltDB file not found at data/kv.db"
    fi
}

# Function to test basic operations
test_operations() {
    print_header "Testing Basic Operations"
    
    if [ -z "$LEADER_PORT" ]; then
        find_leader || return 1
    fi
    
    test_key="test-$(date +%s)"
    test_value="test-value-$(date +%s)"
    
    echo "1. Storing test data..."
    store_response=$(curl -s -X PUT "http://localhost:$LEADER_PORT/kv/$test_key" \
        -H "Content-Type: application/json" \
        -d "{\"value\":\"$test_value\"}")
    
    if echo "$store_response" | jq -e '.key' > /dev/null 2>&1; then
        print_success "Data stored successfully"
        echo "$store_response" | jq '.'
    else
        print_error "Failed to store data"
        echo "$store_response"
        return 1
    fi
    
    echo ""
    echo "2. Retrieving test data..."
    retrieve_response=$(curl -s "http://localhost:$LEADER_PORT/kv/$test_key")
    
    if echo "$retrieve_response" | jq -e '.key' > /dev/null 2>&1; then
        print_success "Data retrieved successfully"
        echo "$retrieve_response" | jq '.'
    else
        print_error "Failed to retrieve data"
        echo "$retrieve_response"
        return 1
    fi
    
    echo ""
    echo "3. Deleting test data..."
    delete_response=$(curl -s -X DELETE "http://localhost:$LEADER_PORT/kv/$test_key")
    
    if echo "$delete_response" | jq -e '.key' > /dev/null 2>&1; then
        print_success "Data deleted successfully"
        echo "$delete_response" | jq '.'
    else
        print_error "Failed to delete data"
        echo "$delete_response"
        return 1
    fi
}

# Main function
main() {
    echo -e "${BLUE}"
    echo "================================================"
    echo "  Distributed Key-Value Store Database Inspector"
    echo "================================================"
    echo -e "${NC}"
    
    case "${1:-all}" in
        "status")
            show_cluster_status
            ;;
        "data")
            show_all_data
            ;;
        "raft")
            show_raft_state
            ;;
        "bolt")
            show_boltdb_info
            ;;
        "test")
            test_operations
            ;;
        "all")
            find_leader
            echo ""
            show_cluster_status
            echo ""
            show_all_data
            echo ""
            show_raft_state
            echo ""
            show_boltdb_info
            ;;
        "help"|*)
            echo "Usage: $0 [command]"
            echo ""
            echo "Commands:"
            echo "  all     - Show everything (default)"
            echo "  status  - Show cluster status"
            echo "  data    - Show stored key-value data"
            echo "  raft    - Show Raft state files"
            echo "  bolt    - Show BoltDB file info"
            echo "  test    - Test basic operations"
            echo "  help    - Show this help"
            echo ""
            echo "Examples:"
            echo "  $0 status    # Show cluster status"
            echo "  $0 data      # Show stored data"
            echo "  $0 test      # Test store/retrieve/delete"
            ;;
    esac
}

# Check if jq is available
if ! command -v jq &> /dev/null; then
    print_warning "jq not found. Install with: brew install jq (macOS) or apt-get install jq (Ubuntu)"
    echo "Raw JSON output will be shown instead."
fi

# Run main function
main "$@"