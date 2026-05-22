package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"sync"
	"time"

	"calyx/crypto"
	"calyx/dht"
	"calyx/engine"
	pb "calyx/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

var (
	WeightsPath = ""
	ModelID     = "google/gemma-2b"
	StunServer  = ""
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
	startLayer        int32
	endLayer          int32
	address           string
	kvCache           sync.Map // TaskID (string) -> *KVCacheEntry
	ttl               time.Duration
	dht               *dht.KademliaDHT
	powDifficulty     int
	transformer       *engine.TransformerLayer
	tlsCert           tls.Certificate
	teeEnclave        bool
	attestationReport *crypto.AttestationReport
}

// NewServer creates a new Server node
func NewServer(address string, startLayer, endLayer int32, ttl time.Duration, powDifficulty int, cert tls.Certificate, network *sync.Map, teeEnclave bool) *Server {
	var report *crypto.AttestationReport
	if teeEnclave {
		var err error
		report, err = crypto.GenerateAttestationReport(address, crypto.DefaultMRENCLAVE)
		if err != nil {
			log.Printf("[Server Layers %d-%d] Warning: failed to generate TEE attestation report: %v", startLayer, endLayer, err)
		} else {
			log.Printf("[Server Layers %d-%d] TEE: Cryptographically signed secure enclave attestation report generated (MRENCLAVE: %s)", startLayer, endLayer, report.MRENCLAVE)
		}
	}

	transformerLayer := engine.NewTransformerLayer(4) // default hiddenDim is 4
	if WeightsPath != "" {
		if err := engine.EnsureWeightsExist(WeightsPath, 4); err != nil {
			log.Printf("[Server Layers %d-%d] Warning: failed to ensure weights file: %v. Using default identity weights.", startLayer, endLayer, err)
		} else {
			loaded, err := engine.LoadWeights(WeightsPath)
			if err != nil {
				log.Printf("[Server Layers %d-%d] Warning: failed to load physical weights from %s: %v. Using default identity weights.", startLayer, endLayer, WeightsPath, err)
			} else {
				transformerLayer = loaded
				log.Printf("[Server Layers %d-%d] Engine: Successfully loaded physical model weights from %s!", startLayer, endLayer, WeightsPath)
			}
		}
	}

	srv := &Server{
		address:           address,
		startLayer:        startLayer,
		endLayer:          endLayer,
		ttl:               ttl,
		powDifficulty:     powDifficulty,
		transformer:       transformerLayer,
		tlsCert:           cert,
		dht:               dht.NewKademliaDHT(address, network),
		teeEnclave:        teeEnclave,
		attestationReport: report,
	}
	// Announce capacity for each layer in its local DHT
	for l := startLayer; l <= endLayer; l++ {
		srv.dht.Store(l, address)
	}
	return srv
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

	// Retrieve client metadata to verify Hashcash Proof-of-Work
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return errors.New("security violation: missing metadata")
	}
	nonces := md.Get("pow-nonce")
	if len(nonces) == 0 {
		return errors.New("security violation: missing Proof-of-Work nonce")
	}
	nonce := nonces[0]

	taskIDs := md.Get("task-id")
	if len(taskIDs) == 0 {
		return errors.New("security violation: missing task-id in metadata")
	}
	expectedTaskID := taskIDs[0]

	// Verify the solution
	if !crypto.Verify(expectedTaskID, nonce, s.powDifficulty) {
		return fmt.Errorf("security violation: invalid Proof of Work nonce for TaskID %s and difficulty %d", expectedTaskID, s.powDifficulty)
	}

	// Send simulated TEE enclave attestation report if enabled
	if s.teeEnclave && s.attestationReport != nil {
		reportBytes, err := json.Marshal(s.attestationReport)
		if err == nil {
			headerMD := metadata.Pairs("enclave-attestation", string(reportBytes))
			if sErr := stream.SendHeader(headerMD); sErr != nil {
				log.Printf("[Server Layers %d-%d] Warning: failed to send TEE attestation header: %v", s.startLayer, s.endLayer, sErr)
				_ = stream.SendHeader(metadata.MD{})
			} else {
				log.Printf("[Server Layers %d-%d] TEE: Sent secure attestation report to upstream client/peer", s.startLayer, s.endLayer)
			}
		} else {
			log.Printf("[Server Layers %d-%d] Warning: failed to marshal TEE attestation report: %v", s.startLayer, s.endLayer, err)
			_ = stream.SendHeader(metadata.MD{})
		}
	} else {
		// Even if TEE is not enabled, we must send an empty header to prevent the client's Header() call from deadlocking the bidirectional stream
		_ = stream.SendHeader(metadata.MD{})
	}

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

		// Verify Task ID matches authenticated stream Task ID
		if tensor.TaskId != expectedTaskID {
			return fmt.Errorf("security violation: tensor TaskID %s mismatches authenticated stream TaskID %s", tensor.TaskId, expectedTaskID)
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

		// Adjust the transformer layer weights or hidden dimensions to match incoming tensor shape if needed
		if len(tensor.Shape) >= 2 && int(tensor.Shape[1]) != s.transformer.HiddenDim {
			s.transformer = engine.NewTransformerLayer(int(tensor.Shape[1]))
		}

		// Run actual transformer forward pass!
		outData, tErr := s.transformer.Forward(tensor.Data, tensor.Shape, &entry.Data)
		if tErr != nil {
			entry.mu.Unlock()
			return fmt.Errorf("transformer forward error: %w", tErr)
		}
		cacheLength := len(entry.Data)
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
				log.Printf("[Server Layers %d-%d] Pipeline: Connecting to the next node %s via mTLS...", s.startLayer, s.endLayer, nextAddr)
				clientTLS := crypto.GetClientTLSConfig(s.tlsCert)
				nextConn, initErr = grpc.NewClient(nextAddr, grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
				if initErr != nil {
					return
				}

				client := pb.NewP2PServiceClient(nextConn)
				// Propagate outgoing metadata so downstream servers can verify task and pow if needed.
				nextStreamCtx := metadata.NewOutgoingContext(ctx, md)
				nextStream, initErr = client.Forward(nextStreamCtx)
				if initErr != nil {
					return
				}

				// Retrieve and verify downstream server TEE attestation report
				header, hErr := nextStream.Header()
				if hErr != nil {
					initErr = fmt.Errorf("failed to retrieve downstream TEE headers: %w", hErr)
					return
				}
				if attestVals := header.Get("enclave-attestation"); len(attestVals) > 0 {
					var report crypto.AttestationReport
					if jsonErr := json.Unmarshal([]byte(attestVals[0]), &report); jsonErr != nil {
						initErr = fmt.Errorf("failed to unmarshal downstream TEE report: %w", jsonErr)
						return
					}
					if vErr := crypto.VerifyAttestationReport(&report, crypto.DefaultMRENCLAVE); vErr != nil {
						initErr = fmt.Errorf("downstream TEE audit failed: %w", vErr)
						return
					}
					log.Printf("[Server Layers %d-%d] TEE Audit (Success): Downstream node %s verified cryptographically", s.startLayer, s.endLayer, nextAddr)
				} else if s.teeEnclave {
					initErr = fmt.Errorf("security violation: downstream node %s did not provide a TEE attestation report", nextAddr)
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

// Register contacts the Bootstrap server and registers this node using secure mTLS
func (s *Server) Register(bootstrapAddr string) error {
	clientTLS := crypto.GetClientTLSConfig(s.tlsCert)
	conn, err := grpc.NewClient(bootstrapAddr, grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		return fmt.Errorf("failed to connect to bootstrap: %w", err)
	}
	defer conn.Close()

	client := pb.NewBootstrapServiceClient(conn)
	md := metadata.Pairs("model-id", ModelID)
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	resp, err := client.RegisterNode(ctx, &pb.RegisterRequest{
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
func StartServer(ctx context.Context, bootstrapAddr string, startLayer, endLayer int32, addr string, ttl time.Duration, powDifficulty int, network *sync.Map, wg *sync.WaitGroup, teeEnclave bool) (*grpc.Server, error) {
	// Listen on 0.0.0.0:port to be fully robust inside containerized environments,
	// but preserve the original host/IP in srv.address for DHT registration and discovery.
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		port = addr
	}
	listenAddr := "0.0.0.0:" + port
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", listenAddr, err)
	}

	// Dynamically query external IP mapped via STUN for WAN NAT traversal diagnostics
	go func() {
		extAddr, err := crypto.GetExternalIPMappedAddress(StunServer)
		if err != nil {
			log.Printf("[Server Layers %d-%d] NAT Discovery: STUN query skipped/unavailable: %v (operating on local network)", startLayer, endLayer, err)
		} else {
			log.Printf("[Server Layers %d-%d] NAT Discovery (Success): Node public-facing mapped address is %s", startLayer, endLayer, extAddr)
		}
	}()

	// 1. Generate key pair and dynamic mTLS certificate
	cert, err := crypto.GenerateKeyPairAndCert()
	if err != nil {
		lis.Close()
		return nil, fmt.Errorf("failed to generate TLS certificate: %w", err)
	}

	// 2. Configure server TLS 1.3 settings
	tlsCfg := crypto.GetServerTLSConfig(cert)
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsCfg)))

	// 3. Initialize server node with DHT routing
	srv := NewServer(addr, startLayer, endLayer, ttl, powDifficulty, cert, network, teeEnclave)
	pb.RegisterP2PServiceServer(grpcServer, srv)

	// If bootstrap address is provided, add it to DHT peers
	if bootstrapAddr != "" {
		bootstrapID := dht.HashString(bootstrapAddr)
		srv.dht.AddPeer(dht.Peer{
			ID:         bootstrapID,
			Address:    bootstrapAddr,
			StartLayer: 0,
			EndLayer:   0,
		})

		// If in-memory overlay map is active, populate bi-directionally
		if network != nil {
			if val, ok := network.Load(bootstrapID); ok {
				if bootstrapDHT, ok := val.(*dht.KademliaDHT); ok {
					bootstrapDHT.AddPeer(dht.Peer{
						ID:         srv.dht.LocalID,
						Address:    srv.address,
						StartLayer: srv.startLayer,
						EndLayer:   srv.endLayer,
					})
				}
			}
		}
	}

	// Start TTL monitor daemon in background
	go srv.startTTLWorker(ctx)

	// Register with Bootstrap (optional fallback, since DHT is fully decentralized)
	if bootstrapAddr != "" {
		if err := srv.Register(bootstrapAddr); err != nil {
			log.Printf("[Server Layers %d-%d] Warning: failed to register with Bootstrap coordinator (%v). Continuing via DHT routing...", startLayer, endLayer, err)
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("[Server Layers %d-%d] Secure P2P mTLS server started on %s", startLayer, endLayer, addr)
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
