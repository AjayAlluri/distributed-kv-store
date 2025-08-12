# Phase 1: Foundation Complete ✅

## Project Overview

Built a **production-quality distributed key-value store foundation** with enterprise-grade PostgreSQL backend, demonstrating advanced Go development and distributed systems architecture. This phase establishes the groundwork for implementing Raft consensus in Phase 2.

## 🏗️ Architecture Implemented

### **Core Components**
- **HTTP REST API**: Clean endpoints for GET/PUT/DELETE operations with proper status codes
- **Dual Storage Backends**: PostgreSQL (production) + BoltDB (development/fallback)
- **Configuration Management**: YAML files with environment variable overrides
- **Structured Logging**: JSON logs with configurable levels and file output
- **Connection Pooling**: Enterprise PostgreSQL integration with pgxpool
- **Graceful Shutdown**: Production-ready resource management and signal handling

### **Technology Stack**
- **Language**: Go 1.24+
- **Primary Database**: PostgreSQL with pgx driver (industry standard)
- **Fallback Storage**: BoltDB embedded database (development/testing)
- **HTTP Router**: Gorilla Mux
- **Logging**: Logrus with JSON formatting
- **Configuration**: YAML + environment variable support
- **Development Process**: Professional feature branch workflow

## 🎯 Why Dual Storage Architecture?

### **PostgreSQL (Primary - Production)**
- **Industry Standard**: Used by every major company (FAANG, startups, enterprises)
- **Resume Value**: Shows SQL skills, connection pooling, transaction management
- **Scalability**: Supports multiple nodes, read replicas, advanced querying
- **Enterprise Features**: ACID transactions, SSL/TLS, connection pooling
- **Real-world Architecture**: How production distributed systems actually work

### **BoltDB (Secondary - Development/Fallback)**
- **Development Simplicity**: No external dependencies for local development
- **Testing**: Fast, isolated tests without database setup
- **Embedded Use Cases**: Edge deployments, single-node scenarios
- **Fallback Option**: Graceful degradation when database unavailable
- **Educational Value**: Shows interface-based design and storage abstraction

### **Strategic Decision**
The dual architecture demonstrates:
1. **Professional Design**: Interface-based storage abstraction
2. **Operational Flexibility**: Different backends for different environments
3. **Development Efficiency**: Easy local development without PostgreSQL
4. **Production Readiness**: Enterprise database for production workloads

## 🚀 Complete Setup and Testing Guide

### **Prerequisites**
```bash
# Install required software
brew install go postgresql

# Start PostgreSQL
brew services start postgresql

# Create database
createdb kvstore
```

### **1. Clone and Build**
```bash
git clone https://github.com/AjayAlluri/distributed-kv-store.git
cd distributed-kv-store
go mod download
go build -o bin/kvstore cmd/server/main.go
```

### **2. PostgreSQL Backend (Recommended)**

#### **Start Server:**
```bash
# Option A: Using configuration file
./bin/kvstore --config config/postgres.yaml

# Option B: Using environment variables (12-factor app style)
KV_STORAGE_TYPE=database \
KV_DB_HOST=localhost \
KV_DB_NAME=kvstore \
KV_DB_USER=$(whoami) \
./bin/kvstore
```

#### **Expected Output:**
```json
{"level":"info","msg":"Starting distributed key-value store","version":"1.0.0"}
{"storage_type":"database","db_host":"localhost","msg":"PostgreSQL storage initialized successfully"}
{"address":"0.0.0.0:8080","level":"info","msg":"Starting HTTP server"}
```

### **3. API Testing**

#### **Store Data:**
```bash
curl -X PUT http://localhost:8080/kv/user:123 \
  -H "Content-Type: application/json" \
  -d '{"value": "Alice Johnson"}'
```
**Response:**
```json
{"key":"user:123","value":"Alice Johnson","message":"Key-value pair stored successfully"}
```

#### **Retrieve Data:**
```bash
curl http://localhost:8080/kv/user:123
```
**Response:**
```json
{"key":"user:123","value":"Alice Johnson"}
```

#### **Delete Data:**
```bash
curl -X DELETE http://localhost:8080/kv/user:123
```
**Response:**
```json
{"key":"user:123","message":"Key deleted successfully"}
```

#### **Server Status:**
```bash
curl http://localhost:8080/status
```
**Response:**
```json
{
  "server":"distributed-kv-store",
  "version":"1.0.0",
  "status":"healthy",
  "role":"leader",
  "node_id":"node-1",
  "timestamp":"2025-08-12T21:10:46Z"
}
```

#### **Health Check:**
```bash
curl http://localhost:8080/health
```
**Response:**
```json
{"healthy":true,"timestamp":"2025-08-12T21:10:47Z"}
```

### **4. PostgreSQL Verification**

#### **Check Data in Database:**
```bash
psql -d kvstore -c "SELECT * FROM kv_store ORDER BY created_at;"
```
**Output:**
```
     key     |     value     |          created_at           |          updated_at           
-------------+---------------+-------------------------------+-------------------------------
 user:123    | Alice Johnson | 2025-08-12 16:10:36.055604-05 | 2025-08-12 16:10:36.055604-05
```

#### **Check Connection Pooling:**
```bash
psql -d kvstore -c "SELECT COUNT(*) as active_connections FROM pg_stat_activity WHERE datname = 'kvstore';"
```
**Output:**
```
 active_connections 
--------------------
                  4
```

#### **View Table Schema:**
```bash
psql -d kvstore -c "\\d kv_store"
```
**Output:**
```
                    Table "public.kv_store"
   Column   |           Type           | Modifiers 
------------+--------------------------+-----------
 key        | text                     | not null
 value      | text                     | not null
 created_at | timestamp with time zone | default now()
 updated_at | timestamp with time zone | default now()
Indexes:
    "kv_store_pkey" PRIMARY KEY, btree (key)
    "idx_kv_store_updated_at" btree (updated_at)
```

### **5. File Storage Testing (Alternative)**

#### **Start with BoltDB:**
```bash
./bin/kvstore --config config/server.yaml
```

#### **Expected Output:**
```json
{"level":"info","msg":"Starting distributed key-value store"}
{"storage_type":"file","data_path":"/path/to/data","msg":"File-based storage initialized successfully"}
{"address":"0.0.0.0:8080","level":"info","msg":"Starting HTTP server"}
```

#### **Test File Storage:**
```bash
curl -X PUT http://localhost:8080/kv/file-test \
  -H "Content-Type: application/json" \
  -d '{"value": "BoltDB Storage"}'

curl http://localhost:8080/kv/file-test
```

#### **Verify File Created:**
```bash
ls -la data/
# Shows: kv.db (BoltDB file)
```

## ⚙️ Configuration Options

### **PostgreSQL Configuration** (`config/postgres.yaml`)
```yaml
server:
  host: "0.0.0.0"
  port: 8080

storage:
  type: "database"

database:
  host: "localhost"
  port: 5432
  database: "kvstore"
  username: "your_username"
  password: ""
  max_conns: 10
  min_conns: 2
  ssl_mode: "prefer"

logging:
  level: "info"
  format: "json"
```

### **File Storage Configuration** (`config/server.yaml`)
```yaml
server:
  host: "0.0.0.0"
  port: 8080

storage:
  type: "file"
  data_dir: "./data"

logging:
  level: "info"
  format: "json"
```

### **Environment Variables**
```bash
# Storage Backend
export KV_STORAGE_TYPE=database          # "database" or "file"

# PostgreSQL Settings
export KV_DB_HOST=localhost
export KV_DB_PORT=5432
export KV_DB_NAME=kvstore
export KV_DB_USER=your_username
export KV_DB_PASSWORD=your_password
export KV_DB_SSLMODE=prefer

# Server Settings
export KV_SERVER_HOST=0.0.0.0
export KV_SERVER_PORT=8080

# Logging
export KV_LOG_LEVEL=info                 # debug, info, warn, error

# File Storage (when type=file)
export KV_DATA_DIR=./data
```

## 🏢 Enterprise Features Demonstrated

### **Database Integration**
- **Connection Pooling**: 10 max, 2 min connections with configurable timeouts
- **Prepared Statements**: Optimal query performance and SQL injection prevention
- **ACID Transactions**: Data consistency guarantees
- **SSL/TLS Support**: Encrypted database connections
- **Automatic Schema**: Table and index creation on startup
- **Timestamps**: Created/updated tracking with timezone support

### **Production Operations**
- **Structured Logging**: JSON format for log aggregation systems
- **Graceful Shutdown**: 30-second timeout for clean resource cleanup
- **Health Monitoring**: Dedicated health and status endpoints
- **Configuration Validation**: Startup-time validation prevents runtime issues
- **Error Handling**: Comprehensive HTTP status codes and error responses

### **Development Workflow**
- **Feature Branches**: Professional Git workflow with clean history
- **Environment Parity**: Same code runs in dev (BoltDB) and prod (PostgreSQL)
- **Configuration Management**: Environment-specific configs without code changes
- **Testing Support**: Dual backends enable different testing strategies

## 📊 Testing Results Summary

### ✅ **PostgreSQL Storage Backend**
- Connection successful with connection pooling
- CRUD operations fully functional
- Schema auto-creation working
- Data persistence across server restarts
- Connection pooling visible in database
- ACID transactions verified

### ✅ **Dual Storage Support**
- PostgreSQL mode: `storage.type: "database"` ✓
- File mode: `storage.type: "file"` ✓
- Seamless switching via configuration ✓
- Proper logging for each storage type ✓

### ✅ **Configuration Management**
- YAML configuration files working ✓
- Environment variables override working ✓
- Configuration validation preventing errors ✓
- Default values for production use ✓

### ✅ **API Functionality**
- PUT /kv/{key}: Create/Update with 201/200 responses ✓
- GET /kv/{key}: Read with 200/404 responses ✓
- DELETE /kv/{key}: Delete with 200/404 responses ✓
- GET /status: Server metadata and health ✓
- GET /health: Simple health check ✓

## 💼 Interview Talking Points

### **Technical Depth**
*"I built a distributed key-value store with dual storage backends. The PostgreSQL implementation uses connection pooling with pgx driver, the most performant Go PostgreSQL library. I implemented prepared statements for optimal query performance and ACID transactions for data consistency."*

### **Production Readiness**
*"The system supports both PostgreSQL for production and embedded BoltDB for development. It includes structured JSON logging, graceful shutdown with 30-second timeout, comprehensive error handling, and environment-based configuration following 12-factor app principles."*

### **System Design**
*"I used interface-based design to abstract storage layers, enabling easy switching between backends. The configuration system supports YAML files and environment variables, with validation to prevent runtime issues. The architecture is designed for horizontal scaling with the upcoming Raft consensus layer."*

## 🚀 Ready for Phase 2: Raft Consensus

### **Foundation Provides**
- **HTTP API Layer**: Ready for client requests and leader forwarding
- **Storage Interface**: Abstracted for Raft log persistence
- **Configuration System**: Ready for multi-node cluster configuration
- **Logging Infrastructure**: Structured logs for distributed system debugging
- **Server Framework**: Graceful shutdown and resource management

### **Next Phase Will Add**
- Raft state machine implementation
- Leader election algorithm
- Log replication and consensus
- Multi-node cluster support
- Network partition tolerance
- Client request forwarding

## 🎯 Project Status

**Repository**: https://github.com/AjayAlluri/distributed-kv-store

**Phase 1**: ✅ **COMPLETE**
- Production-quality single-node key-value store
- Enterprise PostgreSQL integration
- Professional development workflow
- Comprehensive testing and documentation
- Interview-ready demonstration

**Phase 2**: 🚧 **READY TO BEGIN**
- Raft consensus algorithm implementation
- Distributed cluster functionality
- Advanced distributed systems concepts

---

**Demo Command**: `./bin/kvstore` → Test API → Show PostgreSQL data → Impress technical interviewers! 💪