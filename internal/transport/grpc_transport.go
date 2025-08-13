package transport

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ajayalluri/distributed-kv-store/internal/raft"
	pb "github.com/ajayalluri/distributed-kv-store/internal/transport/proto"
)

// GRPCTransport implements the RPCTransport interface using gRPC
type GRPCTransport struct {
	// Embed the unimplemented server for forward compatibility
	pb.UnimplementedRaftServiceServer
	
	mu        sync.RWMutex
	localAddr string
	server    *grpc.Server
	listener  net.Listener
	
	// Client connections to other nodes
	clients map[string]pb.RaftServiceClient
	conns   map[string]*grpc.ClientConn
	
	// Timeouts
	requestTimeout time.Duration
	
	// Channels for incoming RPCs
	voteRequestCh    chan *raft.VoteRequest
	appendEntriesCh  chan *raft.AppendEntriesRequest
	
	logger *logrus.Logger
}

// NewGRPCTransport creates a new gRPC-based transport
func NewGRPCTransport(localAddr string, logger *logrus.Logger) *GRPCTransport {
	return &GRPCTransport{
		localAddr:       localAddr,
		clients:         make(map[string]pb.RaftServiceClient),
		conns:           make(map[string]*grpc.ClientConn),
		requestTimeout:  5 * time.Second,
		voteRequestCh:   make(chan *raft.VoteRequest, 100),
		appendEntriesCh: make(chan *raft.AppendEntriesRequest, 100),
		logger:          logger,
	}
}

// Start starts the gRPC server and begins listening for connections
func (gt *GRPCTransport) Start() error {
	listener, err := net.Listen("tcp", gt.localAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", gt.localAddr, err)
	}
	
	gt.listener = listener
	gt.server = grpc.NewServer()
	
	// Register the Raft service
	pb.RegisterRaftServiceServer(gt.server, gt)
	
	gt.logger.WithField("address", gt.localAddr).Info("Starting gRPC transport server")
	
	// Start serving in a goroutine
	go func() {
		if err := gt.server.Serve(listener); err != nil {
			gt.logger.WithError(err).Error("gRPC server failed")
		}
	}()
	
	return nil
}

// Stop stops the gRPC server and closes all connections
func (gt *GRPCTransport) Stop() error {
	gt.mu.Lock()
	defer gt.mu.Unlock()
	
	gt.logger.Info("Stopping gRPC transport")
	
	// Close all client connections
	for addr, conn := range gt.conns {
		conn.Close()
		delete(gt.conns, addr)
		delete(gt.clients, addr)
	}
	
	// Stop the server
	if gt.server != nil {
		gt.server.GracefulStop()
	}
	
	return nil
}

// LocalAddr returns the local address of this transport
func (gt *GRPCTransport) LocalAddr() string {
	return gt.localAddr
}

// getClient returns a gRPC client for the specified node
func (gt *GRPCTransport) getClient(nodeAddr string) (pb.RaftServiceClient, error) {
	gt.mu.Lock()
	defer gt.mu.Unlock()
	
	// Return existing client if available
	if client, exists := gt.clients[nodeAddr]; exists {
		return client, nil
	}
	
	// Create new connection
	ctx, cancel := context.WithTimeout(context.Background(), gt.requestTimeout)
	defer cancel()
	
	conn, err := grpc.DialContext(ctx, nodeAddr, 
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", nodeAddr, err)
	}
	
	client := pb.NewRaftServiceClient(conn)
	gt.conns[nodeAddr] = conn
	gt.clients[nodeAddr] = client
	
	gt.logger.WithField("node_addr", nodeAddr).Debug("Created new gRPC client connection")
	
	return client, nil
}

// SendVoteRequest sends a vote request to the specified node
func (gt *GRPCTransport) SendVoteRequest(nodeAddr string, req *raft.VoteRequest) (*raft.VoteResponse, error) {
	client, err := gt.getClient(nodeAddr)
	if err != nil {
		return nil, err
	}
	
	// Convert internal request to protobuf
	pbReq := &pb.VoteRequest{
		Term:         req.Term,
		CandidateId:  req.CandidateID,
		LastLogIndex: req.LastLogIndex,
		LastLogTerm:  req.LastLogTerm,
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), gt.requestTimeout)
	defer cancel()
	
	pbResp, err := client.RequestVote(ctx, pbReq)
	if err != nil {
		gt.logger.WithFields(logrus.Fields{
			"node_addr": nodeAddr,
			"error":     err,
		}).Error("RequestVote RPC failed")
		return nil, err
	}
	
	// Convert protobuf response to internal type
	return &raft.VoteResponse{
		Term:        pbResp.Term,
		VoteGranted: pbResp.VoteGranted,
	}, nil
}

// SendAppendEntries sends an append entries request to the specified node
func (gt *GRPCTransport) SendAppendEntries(nodeAddr string, req *raft.AppendEntriesRequest) (*raft.AppendEntriesResponse, error) {
	client, err := gt.getClient(nodeAddr)
	if err != nil {
		return nil, err
	}
	
	// Convert internal request to protobuf
	pbEntries := make([]*pb.LogEntry, len(req.Entries))
	for i, entry := range req.Entries {
		pbEntries[i] = &pb.LogEntry{
			Term:  entry.Term,
			Index: entry.Index,
			Data:  entry.Data,
		}
	}
	
	pbReq := &pb.AppendEntriesRequest{
		Term:         req.Term,
		LeaderId:     req.LeaderID,
		PrevLogIndex: req.PrevLogIndex,
		PrevLogTerm:  req.PrevLogTerm,
		Entries:      pbEntries,
		LeaderCommit: req.LeaderCommit,
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), gt.requestTimeout)
	defer cancel()
	
	pbResp, err := client.AppendEntries(ctx, pbReq)
	if err != nil {
		gt.logger.WithFields(logrus.Fields{
			"node_addr": nodeAddr,
			"error":     err,
		}).Error("AppendEntries RPC failed")
		return nil, err
	}
	
	// Convert protobuf response to internal type
	return &raft.AppendEntriesResponse{
		Term:          pbResp.Term,
		Success:       pbResp.Success,
		ConflictTerm:  pbResp.ConflictTerm,
		ConflictIndex: pbResp.ConflictIndex,
	}, nil
}

// SendInstallSnapshot sends a snapshot to the specified node
func (gt *GRPCTransport) SendInstallSnapshot(nodeAddr string, req *raft.InstallSnapshotRequest) (*raft.InstallSnapshotResponse, error) {
	client, err := gt.getClient(nodeAddr)
	if err != nil {
		return nil, err
	}
	
	// Convert internal request to protobuf
	pbReq := &pb.InstallSnapshotRequest{
		Term:             req.Term,
		LeaderId:         req.LeaderID,
		LastIncludedIndex: req.LastIncludedIndex,
		LastIncludedTerm:  req.LastIncludedTerm,
		Offset:           req.Offset,
		Data:             req.Data,
		Done:             req.Done,
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), gt.requestTimeout)
	defer cancel()
	
	pbResp, err := client.InstallSnapshot(ctx, pbReq)
	if err != nil {
		gt.logger.WithFields(logrus.Fields{
			"node_addr": nodeAddr,
			"error":     err,
		}).Error("InstallSnapshot RPC failed")
		return nil, err
	}
	
	// Convert protobuf response to internal type
	return &raft.InstallSnapshotResponse{
		Term: pbResp.Term,
	}, nil
}

// gRPC service implementation

// RequestVote handles incoming vote requests
func (gt *GRPCTransport) RequestVote(ctx context.Context, req *pb.VoteRequest) (*pb.VoteResponse, error) {
	// Convert protobuf request to internal type
	internalReq := &raft.VoteRequest{
		Term:         req.Term,
		CandidateID:  req.CandidateId,
		LastLogIndex: req.LastLogIndex,
		LastLogTerm:  req.LastLogTerm,
	}
	
	// Send to Raft node via channel (non-blocking)
	select {
	case gt.voteRequestCh <- internalReq:
	default:
		gt.logger.Warn("Vote request channel full, dropping request")
		return &pb.VoteResponse{
			Term:        0,
			VoteGranted: false,
		}, nil
	}
	
	// For now, return a placeholder response
	// In a real implementation, we'd need a way to get the actual response
	// from the Raft node, possibly through a response channel
	return &pb.VoteResponse{
		Term:        req.Term,
		VoteGranted: false,
	}, nil
}

// AppendEntries handles incoming append entries requests
func (gt *GRPCTransport) AppendEntries(ctx context.Context, req *pb.AppendEntriesRequest) (*pb.AppendEntriesResponse, error) {
	// Convert protobuf request to internal type
	entries := make([]raft.LogEntry, len(req.Entries))
	for i, pbEntry := range req.Entries {
		entries[i] = raft.LogEntry{
			Term:  pbEntry.Term,
			Index: pbEntry.Index,
			Data:  pbEntry.Data,
		}
	}
	
	internalReq := &raft.AppendEntriesRequest{
		Term:         req.Term,
		LeaderID:     req.LeaderId,
		PrevLogIndex: req.PrevLogIndex,
		PrevLogTerm:  req.PrevLogTerm,
		Entries:      entries,
		LeaderCommit: req.LeaderCommit,
	}
	
	// Send to Raft node via channel (non-blocking)
	select {
	case gt.appendEntriesCh <- internalReq:
	default:
		gt.logger.Warn("Append entries channel full, dropping request")
		return &pb.AppendEntriesResponse{
			Term:    req.Term,
			Success: false,
		}, nil
	}
	
	// For now, return a placeholder response
	// In a real implementation, we'd need a way to get the actual response
	return &pb.AppendEntriesResponse{
		Term:    req.Term,
		Success: true,
	}, nil
}

// InstallSnapshot handles incoming snapshot requests
func (gt *GRPCTransport) InstallSnapshot(ctx context.Context, req *pb.InstallSnapshotRequest) (*pb.InstallSnapshotResponse, error) {
	gt.logger.WithFields(logrus.Fields{
		"term":         req.Term,
		"leader_id":    req.LeaderId,
		"last_included_index": req.LastIncludedIndex,
		"data_size":    len(req.Data),
	}).Debug("Received install snapshot request")
	
	// For now, just return success
	// In a real implementation, we'd handle the snapshot installation
	return &pb.InstallSnapshotResponse{
		Term: req.Term,
	}, nil
}

// GetVoteRequestChan returns the channel for incoming vote requests
func (gt *GRPCTransport) GetVoteRequestChan() <-chan *raft.VoteRequest {
	return gt.voteRequestCh
}

// GetAppendEntriesChan returns the channel for incoming append entries requests
func (gt *GRPCTransport) GetAppendEntriesChan() <-chan *raft.AppendEntriesRequest {
	return gt.appendEntriesCh
}