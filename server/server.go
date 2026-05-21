package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"sync"
	"time"

	pb "calyx/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// KVCacheEntry represents a thread-safe KV cache entry for a single task session
type KVCacheEntry struct {
	mu           sync.Mutex
	Data         []float64
	LastAccessed time.Time
}

// Server implements the P2PService gRPC server
type Server struct {
	pb.UnimplementedP2PServiceServer
	startLayer int32
	endLayer   int32
	address    string
	kvCache    sync.Map // TaskID (string) -> *KVCacheEntry
	ttl        time.Duration
}

// NewServer creates a new Server node
func NewServer(address string, startLayer, endLayer int32, ttl time.Duration) *Server {
	return &Server{
		address:    address,
		startLayer: startLayer,
		endLayer:   endLayer,
		ttl:        ttl,
	}
}

// Forward implements the gRPC streaming execution pipeline
func (s *Server) Forward(stream pb.P2PService_ForwardServer) error {
	ctx := stream.Context()
	var nextStream pb.P2PService_ForwardClient
	var nextConn *grpc.ClientConn
	var forwardWg sync.WaitGroup
	var onceInit sync.Once
	var initErr error

	defer func() {
		// Clean up downstream connections on exit
		if nextStream != nil {
			nextStream.CloseSend()
		}
		forwardWg.Wait()
		if nextConn != nil {
			nextConn.Close()
		}
	}()

	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("error receiving from upstream: %w", err)
		}

		tensor := req.Tensor
		if tensor == nil {
			return errors.New("received nil tensor")
		}

		// Run security sanitization and bounds checking
		if err := s.sanitizeTensor(tensor); err != nil {
			log.Printf("[Server Layers %d-%d] Security Alert: rejected malicious tensor from client: %v", s.startLayer, s.endLayer, err)
			return fmt.Errorf("security violation: %w", err)
		}

		// 1. Retrieve or update KV Cache
		var entry *KVCacheEntry
		actual, loaded := s.kvCache.Load(tensor.TaskId)
		if !loaded {
			entry = &KVCacheEntry{
				Data:         make([]float64, 0),
				LastAccessed: time.Now(),
			}
			s.kvCache.Store(tensor.TaskId, entry)
			log.Printf("[Server Layers %d-%d] KV Cache: Initialized remote cache for TaskID %s", s.startLayer, s.endLayer, tensor.TaskId)
		} else {
			entry = actual.(*KVCacheEntry)
		}

		entry.mu.Lock()
		entry.LastAccessed = time.Now()
		entry.Data = append(entry.Data, tensor.Data...)
		cacheLength := len(entry.Data)

		// 2. Simulate mathematical calculation depending on local layers and KV Cache state
		// We calculate the average of the accumulated KV cache values to inject sequence dependencies
		var cacheAvg float64
		if cacheLength > 0 {
			var sum float64
			for _, val := range entry.Data {
				sum += val
			}
			cacheAvg = sum / float64(cacheLength)
		}

		outData := make([]float64, len(tensor.Data))
		for i, val := range tensor.Data {
			// Mutation formula: slight decay, cache influence, and small offset relative to layers
			outData[i] = val*0.95 + cacheAvg*0.04 + float64(s.startLayer)*0.01
		}
		entry.mu.Unlock()

		log.Printf("[Server Layers %d-%d] KV Cache Part %d: Processed Tensor %s (Accumulated context size: %d floats)",
			s.startLayer, s.endLayer, s.startLayer/4+1, tensor.Id, cacheLength)

		// 3. Determine next node in routing chain
		nextRouteIndex := req.CurrentRouteIndex + 1
		isLastNode := nextRouteIndex >= int32(len(req.Route))

		if isLastNode {
			// We are the final node in the Pipeline, send response directly back to customer
			resp := &pb.ForwardResponse{
				Tensor: &pb.Tensor{
					Id:     fmt.Sprintf("%s_processed_%d-%d", tensor.Id, s.startLayer, s.endLayer),
					TaskId: tensor.TaskId,
					Data:   outData,
					Shape:  tensor.Shape,
				},
			}
			if err := stream.Send(resp); err != nil {
				return fmt.Errorf("error sending response back to upstream: %w", err)
			}
		} else {
			// Forward activations down the Pipeline to the next node
			nextAddr := req.Route[nextRouteIndex]

			// Initialize connection to next node lazily once
			onceInit.Do(func() {
				log.Printf("[Server Layers %d-%d] Pipeline: Connecting to the next node %s...", s.startLayer, s.endLayer, nextAddr)
				nextConn, initErr = grpc.NewClient(nextAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
				if initErr != nil {
					return
				}

				client := pb.NewP2PServiceClient(nextConn)
				nextStream, initErr = client.Forward(ctx)
				if initErr != nil {
					return
				}

				// Spawn a goroutine to relay downstream responses back upstream
				forwardWg.Add(1)
				go func() {
					defer forwardWg.Done()
					for {
						res, rErr := nextStream.Recv()
						if errors.Is(rErr, io.EOF) {
							return
						}
						if rErr != nil {
							log.Printf("[Server Layers %d-%d] Error receiving from next node: %v", s.startLayer, s.endLayer, rErr)
							return
						}
						if sErr := stream.Send(res); sErr != nil {
							log.Printf("[Server Layers %d-%d] Error sending back upstream: %v", s.startLayer, s.endLayer, sErr)
							return
						}
					}
				}()
			})

			if initErr != nil {
				return fmt.Errorf("failed to lazy-initialize downstream pipeline stream: %w", initErr)
			}

			// Send the processed tensor to the next node
			forwardReq := &pb.ForwardRequest{
				Tensor: &pb.Tensor{
					Id:     fmt.Sprintf("%s_processed_%d-%d", tensor.Id, s.startLayer, s.endLayer),
					TaskId: tensor.TaskId,
					Data:   outData,
					Shape:  tensor.Shape,
				},
				Route:             req.Route,
				CurrentRouteIndex: nextRouteIndex,
			}

			if err := nextStream.Send(forwardReq); err != nil {
				return fmt.Errorf("error sending to next node in pipeline: %w", err)
			}
		}
	}
}

// Register contacts the Bootstrap server and registers this node
func (s *Server) Register(bootstrapAddr string) error {
	conn, err := grpc.NewClient(bootstrapAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to bootstrap: %w", err)
	}
	defer conn.Close()

	client := pb.NewBootstrapServiceClient(conn)
	resp, err := client.RegisterNode(context.Background(), &pb.RegisterRequest{
		Address:    s.address,
		StartLayer: s.startLayer,
		EndLayer:   s.endLayer,
	})
	if err != nil {
		return fmt.Errorf("registration call failed: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("registration rejected: %s", resp.Message)
	}

	log.Printf("[Server Layers %d-%d] Registered on Bootstrap at %s successfully!", s.startLayer, s.endLayer, bootstrapAddr)
	return nil
}

// StartServer runs the gRPC server and starts the TTL daemon
func StartServer(ctx context.Context, bootstrapAddr string, startLayer, endLayer int32, addr string, ttl time.Duration, wg *sync.WaitGroup) (*grpc.Server, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	grpcServer := grpc.NewServer()
	srv := NewServer(addr, startLayer, endLayer, ttl)
	pb.RegisterP2PServiceServer(grpcServer, srv)

	// Start TTL monitor daemon in background
	go srv.startTTLWorker(ctx)

	// Register with Bootstrap
	if err := srv.Register(bootstrapAddr); err != nil {
		grpcServer.Stop()
		return nil, fmt.Errorf("failed to register with Bootstrap: %w", err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("[Server Layers %d-%d] P2P server started on %s", startLayer, endLayer, addr)
		if err := grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("[Server Layers %d-%d] Server error: %v", startLayer, endLayer, err)
		}
	}()

	return grpcServer, nil
}

// GetKVCacheEntry retrieves a cache entry safely (useful for testing)
func (s *Server) GetKVCacheEntry(taskID string) (*KVCacheEntry, bool) {
	val, loaded := s.kvCache.Load(taskID)
	if !loaded {
		return nil, false
	}
	return val.(*KVCacheEntry), true
}

// startTTLWorker cleans up inactive KV Caches periodically
func (s *Server) startTTLWorker(ctx context.Context) {
	// Check every half of TTL interval
	checkInterval := s.ttl / 2
	if checkInterval < 100*time.Millisecond {
		checkInterval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.kvCache.Range(func(key, value interface{}) bool {
				taskID := key.(string)
				entry := value.(*KVCacheEntry)

				entry.mu.Lock()
				lastAccessed := entry.LastAccessed
				entry.mu.Unlock()

				if time.Since(lastAccessed) > s.ttl {
					s.kvCache.Delete(taskID)
					log.Printf("[Server Layers %d-%d] TTL: Cache for TaskID %s expired due to inactivity (%s) and was removed.",
						s.startLayer, s.endLayer, taskID, s.ttl)
				}
				return true
			})
		}
	}
}

// sanitizeTensor validates shape invariants, checks for NaN/Infinity, and clamps floats to a safe boundary
func (s *Server) sanitizeTensor(tensor *pb.Tensor) error {
	if tensor == nil {
		return errors.New("received nil tensor")
	}

	// 1. Verify shape invariants
	expectedSize := int64(1)
	if len(tensor.Shape) == 0 {
		return errors.New("tensor shape cannot be empty")
	}
	for _, dim := range tensor.Shape {
		if dim <= 0 {
			return fmt.Errorf("invalid tensor dimension size: %d", dim)
		}
		expectedSize *= dim
	}
	if expectedSize != int64(len(tensor.Data)) {
		return fmt.Errorf("tensor shape dimensions mismatch actual data length: expected %d, got %d", expectedSize, len(tensor.Data))
	}

	// 2. Scan and clamp float values
	for i, val := range tensor.Data {
		if math.IsNaN(val) {
			return fmt.Errorf("malicious tensor data: detected NaN value at index %d", i)
		}
		if math.IsInf(val, 0) {
			return fmt.Errorf("malicious tensor data: detected Infinity value at index %d", i)
		}
		
		// Clamp to a safe physical boundary to prevent numerical overflow exploits
		if val > 100.0 {
			tensor.Data[i] = 100.0
		} else if val < -100.0 {
			tensor.Data[i] = -100.0
		}
	}

	return nil
}

