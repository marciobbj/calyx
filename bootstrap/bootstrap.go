package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"

	"calyx/crypto"
	pb "calyx/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// NodeInfo stores registration information for a server node
type NodeInfo struct {
	Address    string
	StartLayer int32
	EndLayer   int32
	ModelID    string
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

// RegisterNode registers a new server node with its layer capacity and model ID
func (s *Server) RegisterNode(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	if req.Address == "" {
		return &pb.RegisterResponse{Success: false, Message: "Address cannot be empty"}, nil
	}
	if req.StartLayer > req.EndLayer {
		return &pb.RegisterResponse{Success: false, Message: "Start layer cannot be greater than end layer"}, nil
	}

	// Extract model-id from metadata context
	modelID := "google/gemma-2b" // default fallback
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("model-id"); len(vals) > 0 {
			modelID = vals[0]
		}
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
				ModelID:    modelID,
			}
			log.Printf("[Bootstrap] Updated: %s (Layers %d-%d) for model %s", req.Address, req.StartLayer, req.EndLayer, modelID)
			return &pb.RegisterResponse{Success: true, Message: "Node registration updated"}, nil
		}
	}

	// Register new node
	s.nodes = append(s.nodes, NodeInfo{
		Address:    req.Address,
		StartLayer: req.StartLayer,
		EndLayer:   req.EndLayer,
		ModelID:    modelID,
	})
	log.Printf("[Bootstrap] Registered: %s (Layers %d-%d) for model %s", req.Address, req.StartLayer, req.EndLayer, modelID)

	return &pb.RegisterResponse{Success: true, Message: "Node registered successfully"}, nil
}

// FindRoute finds a sequential path of nodes that covers the layer range [StartLayer, EndLayer] for a specific model ID
func (s *Server) FindRoute(ctx context.Context, req *pb.RouteRequest) (*pb.RouteResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Extract metadata fields
	modelID := "google/gemma-2b" // default
	listModels := "false"
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("model-id"); len(vals) > 0 {
			modelID = vals[0]
		}
		if vals := md.Get("list-models"); len(vals) > 0 {
			listModels = vals[0]
		}
	}

	// If list-models is set, pack active directory listing and return
	if listModels == "true" {
		modelMap := make(map[string][]NodeInfo)
		for _, node := range s.nodes {
			modelMap[node.ModelID] = append(modelMap[node.ModelID], node)
		}

		var activeCatalog []string
		for mID, nodesList := range modelMap {
			activeCatalog = append(activeCatalog, fmt.Sprintf("MODEL:%s", mID))
			for _, n := range nodesList {
				activeCatalog = append(activeCatalog, fmt.Sprintf("NODE:%s|LAYERS:%d-%d", n.Address, n.StartLayer, n.EndLayer))
			}
		}
		return &pb.RouteResponse{Addresses: activeCatalog}, nil
	}

	log.Printf("[Bootstrap] Route search for Model '%s' Layers %d-%d", modelID, req.StartLayer, req.EndLayer)

	var route []string
	currentLayer := req.StartLayer

	// Greedy range coverage search filtered by ModelID
	for currentLayer <= req.EndLayer {
		found := false
		for _, node := range s.nodes {
			// Check if this node matches the model and covers the current layer
			if node.ModelID == modelID && node.StartLayer <= currentLayer && node.EndLayer >= currentLayer {
				route = append(route, node.Address)
				currentLayer = node.EndLayer + 1
				found = true
				break
			}
		}

		// If no node covers the next required layer, route is broken
		if !found {
			log.Printf("[Bootstrap] Broken route: no node covers layer %d for model %s", currentLayer, modelID)
			return nil, fmt.Errorf("could not find a node to cover layer %d of model %s in the range %d-%d", currentLayer, modelID, req.StartLayer, req.EndLayer)
		}
	}

	log.Printf("[Bootstrap] Route found for model %s: %v", modelID, route)
	return &pb.RouteResponse{Addresses: route}, nil
}

// StartBootstrapServer starts the Bootstrap gRPC server on the specified address using secure mTLS
func StartBootstrapServer(addr string, wg *sync.WaitGroup) (*grpc.Server, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	// Generate key pair and dynamic mTLS certificate
	cert, err := crypto.GenerateKeyPairAndCert()
	if err != nil {
		lis.Close()
		return nil, fmt.Errorf("failed to generate TLS certificate: %w", err)
	}

	// Configure server TLS 1.3 settings
	tlsCfg := crypto.GetServerTLSConfig(cert)
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsCfg)))

	bootstrapServer := NewServer()
	pb.RegisterBootstrapServiceServer(grpcServer, bootstrapServer)

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("[Bootstrap] Secure gRPC mTLS server started on %s", addr)
		if err := grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("[Bootstrap] Server error: %v", err)
		}
	}()

	return grpcServer, nil
}
