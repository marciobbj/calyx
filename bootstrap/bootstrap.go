package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"

	pb "calyx/proto"
	"google.golang.org/grpc"
)

// NodeInfo stores registration information for a server node
type NodeInfo struct {
	Address    string
	StartLayer int32
	EndLayer   int32
}

// Server implements the BootstrapService gRPC server
type Server struct {
	pb.UnimplementedBootstrapServiceServer
	mu    sync.RWMutex
	nodes []NodeInfo
}

// NewServer creates a new Bootstrap server instance
func NewServer() *Server {
	return &Server{
		nodes: make([]NodeInfo, 0),
	}
}

// RegisterNode registers a new server node with its layer capacity
func (s *Server) RegisterNode(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	if req.Address == "" {
		return &pb.RegisterResponse{Success: false, Message: "Address cannot be empty"}, nil
	}
	if req.StartLayer > req.EndLayer {
		return &pb.RegisterResponse{Success: false, Message: "Start layer cannot be greater than end layer"}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already registered (or update)
	for i, node := range s.nodes {
		if node.Address == req.Address {
			s.nodes[i] = NodeInfo{
				Address:    req.Address,
				StartLayer: req.StartLayer,
				EndLayer:   req.EndLayer,
			}
			log.Printf("[Bootstrap] Updated: %s (Layers %d-%d)", req.Address, req.StartLayer, req.EndLayer)
			return &pb.RegisterResponse{Success: true, Message: "Node registration updated"}, nil
		}
	}

	// Register new node
	s.nodes = append(s.nodes, NodeInfo{
		Address:    req.Address,
		StartLayer: req.StartLayer,
		EndLayer:   req.EndLayer,
	})
	log.Printf("[Bootstrap] Registered: %s (Layers %d-%d)", req.Address, req.StartLayer, req.EndLayer)

	return &pb.RegisterResponse{Success: true, Message: "Node registered successfully"}, nil
}

// FindRoute finds a sequential path of nodes that covers the layer range [StartLayer, EndLayer]
func (s *Server) FindRoute(ctx context.Context, req *pb.RouteRequest) (*pb.RouteResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	log.Printf("[Bootstrap] Route search for Layers %d-%d", req.StartLayer, req.EndLayer)

	var route []string
	currentLayer := req.StartLayer

	// Greedy range coverage search
	for currentLayer <= req.EndLayer {
		found := false
		for _, node := range s.nodes {
			// Check if this node covers the current layer in the pipeline
			if node.StartLayer <= currentLayer && node.EndLayer >= currentLayer {
				route = append(route, node.Address)
				currentLayer = node.EndLayer + 1
				found = true
				break
			}
		}

		// If no node covers the next required layer, route is broken
		if !found {
			log.Printf("[Bootstrap] Broken route: no node covers layer %d", currentLayer)
			return nil, fmt.Errorf("could not find a node to cover layer %d in the range %d-%d", currentLayer, req.StartLayer, req.EndLayer)
		}
	}

	log.Printf("[Bootstrap] Route found: %v", route)
	return &pb.RouteResponse{Addresses: route}, nil
}

// StartBootstrapServer starts the Bootstrap gRPC server on the specified address
func StartBootstrapServer(addr string, wg *sync.WaitGroup) (*grpc.Server, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	grpcServer := grpc.NewServer()
	bootstrapServer := NewServer()
	pb.RegisterBootstrapServiceServer(grpcServer, bootstrapServer)

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("[Bootstrap] gRPC server started on %s", addr)
		if err := grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("[Bootstrap] Server error: %v", err)
		}
	}()

	return grpcServer, nil
}
