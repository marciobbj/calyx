package tests

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"calyx/bootstrap"
	"calyx/client"
	"calyx/crypto"
	pb "calyx/proto"
	"calyx/server"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// MaliciousServer simulates a compromised or lazy node in the network
type MaliciousServer struct {
	pb.UnimplementedP2PServiceServer
	mode string // "zeros", "static", "explosive"
}

// Forward implements the stream processing loop returning faulty data
func (m *MaliciousServer) Forward(stream pb.P2PService_ForwardServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		var poisonedData []float64
		switch m.mode {
		case "zeros":
			poisonedData = make([]float64, len(req.Tensor.Data)) // filled with 0.0
		case "static":
			poisonedData = make([]float64, len(req.Tensor.Data))
			for i := range poisonedData {
				poisonedData[i] = 42.0 // identical flat static activations
			}
		case "explosive":
			poisonedData = make([]float64, len(req.Tensor.Data))
			for i, val := range req.Tensor.Data {
				poisonedData[i] = val + 100.0 // huge illegal offset that triggers delta check
			}
		default:
			poisonedData = req.Tensor.Data
		}

		resp := &pb.ForwardResponse{
			Tensor: &pb.Tensor{
				Id:     fmt.Sprintf("%s_processed_1-8", req.Tensor.Id),
				TaskId: req.Tensor.TaskId,
				Data:   poisonedData,
				Shape:  req.Tensor.Shape,
			},
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

// startMaliciousServer spins up a MaliciousServer node and registers it with the Bootstrap
func startMaliciousServer(bootstrapAddr, addr string, mode string, startLayer, endLayer int32, wg *sync.WaitGroup) (*grpc.Server, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	cert, err := crypto.GenerateKeyPairAndCert()
	if err != nil {
		lis.Close()
		return nil, err
	}

	tlsCfg := crypto.GetServerTLSConfig(cert)
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsCfg)))
	srv := &MaliciousServer{mode: mode}
	pb.RegisterP2PServiceServer(grpcServer, srv)

	// Connect to bootstrap and register the node using secure mTLS
	clientTLS := crypto.GetClientTLSConfig(cert)
	conn, err := grpc.NewClient(bootstrapAddr, grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		grpcServer.Stop()
		lis.Close()
		return nil, err
	}
	defer conn.Close()

	bootstrapClient := pb.NewBootstrapServiceClient(conn)
	resp, err := bootstrapClient.RegisterNode(context.Background(), &pb.RegisterRequest{
		Address:    addr,
		StartLayer: startLayer,
		EndLayer:   endLayer,
	})
	if err != nil {
		grpcServer.Stop()
		lis.Close()
		return nil, err
	}
	if !resp.Success {
		grpcServer.Stop()
		lis.Close()
		return nil, fmt.Errorf("failed to register mock node: %s", resp.Message)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = grpcServer.Serve(lis)
	}()

	return grpcServer, nil
}

// TestServerRejectsNaNTensor verifies that a server rejects stream packages with toxic NaN/Infinity float values.
func TestServerRejectsNaNTensor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	bootstrapAddr := getFreeTCPPort(t)
	serverAddr := getFreeTCPPort(t)

	// 1. Start Bootstrap Server
	bSrv, err := bootstrap.StartBootstrapServer(bootstrapAddr, &wg)
	if err != nil {
		t.Fatalf("Failed to start Bootstrap: %v", err)
	}
	defer bSrv.GracefulStop()
	time.Sleep(50 * time.Millisecond)

	// 2. Start a normal Server node (uses the server package that has sanitizeTensor implemented)
	sSrv, err := startNormalServer(ctx, bootstrapAddr, serverAddr, &wg)
	if err != nil {
		t.Fatalf("Failed to start Server: %v", err)
	}
	defer sSrv.GracefulStop()
	time.Sleep(100 * time.Millisecond)

	// 3. Dial the server directly using mTLS
	cert, err := crypto.GenerateKeyPairAndCert()
	if err != nil {
		t.Fatalf("Failed to generate client TLS cert: %v", err)
	}
	clientTLS := crypto.GetClientTLSConfig(cert)
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	// 4. Solve Proof of Work challenge (difficulty = 2)
	taskID := "nan_test"
	nonce := crypto.Solve(taskID, 2)

	client := pb.NewP2PServiceClient(conn)
	md := metadata.Pairs("pow-nonce", nonce, "task-id", taskID)
	streamCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := client.Forward(streamCtx)
	if err != nil {
		t.Fatalf("Failed to open forward stream: %v", err)
	}

	req := &pb.ForwardRequest{
		Tensor: &pb.Tensor{
			Id:     "nan_token",
			TaskId: taskID,
			Data:   []float64{1.0, math.NaN(), 3.0, 4.0},
			Shape:  []int64{1, 4},
		},
		Route:             []string{serverAddr},
		CurrentRouteIndex: 0,
	}

	if err := stream.Send(req); err != nil {
		t.Fatalf("Failed to send bad tensor: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Error("Expected server to reject NaN tensor with a gRPC error, but received no error")
	} else {
		t.Logf("Successfully caught expected server rejection: %v", err)
	}
}

// TestServerRejectsInvalidShapeInvariant verifies that a server rejects stream packages where shape does not match actual length.
func TestServerRejectsInvalidShapeInvariant(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	bootstrapAddr := getFreeTCPPort(t)
	serverAddr := getFreeTCPPort(t)

	// 1. Start Bootstrap Server
	bSrv, err := bootstrap.StartBootstrapServer(bootstrapAddr, &wg)
	if err != nil {
		t.Fatalf("Failed to start Bootstrap: %v", err)
	}
	defer bSrv.GracefulStop()
	time.Sleep(50 * time.Millisecond)

	// 2. Start a normal Server node
	sSrv, err := startNormalServer(ctx, bootstrapAddr, serverAddr, &wg)
	if err != nil {
		t.Fatalf("Failed to start Server: %v", err)
	}
	defer sSrv.GracefulStop()
	time.Sleep(100 * time.Millisecond)

	// 3. Dial the server directly using mTLS
	cert, err := crypto.GenerateKeyPairAndCert()
	if err != nil {
		t.Fatalf("Failed to generate client TLS cert: %v", err)
	}
	clientTLS := crypto.GetClientTLSConfig(cert)
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	// 4. Solve Proof of Work challenge (difficulty = 2)
	taskID := "shape_test"
	nonce := crypto.Solve(taskID, 2)

	client := pb.NewP2PServiceClient(conn)
	md := metadata.Pairs("pow-nonce", nonce, "task-id", taskID)
	streamCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := client.Forward(streamCtx)
	if err != nil {
		t.Fatalf("Failed to open forward stream: %v", err)
	}

	req := &pb.ForwardRequest{
		Tensor: &pb.Tensor{
			Id:     "shape_token",
			TaskId: taskID,
			Data:   []float64{1.0, 2.0, 3.0}, // only 3 elements
			Shape:  []int64{1, 4},            // claims 4 elements expected
		},
		Route:             []string{serverAddr},
		CurrentRouteIndex: 0,
	}

	if err := stream.Send(req); err != nil {
		t.Fatalf("Failed to send bad tensor: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Error("Expected server to reject invalid shape invariant, but received no error")
	} else {
		t.Logf("Successfully caught expected server rejection: %v", err)
	}
}

// TestClientDetectsPoisonedServerComputation verifies client detects lazy or malicious computation
func TestClientDetectsPoisonedServerComputation(t *testing.T) {
	modes := []string{"zeros", "static", "explosive"}

	for _, mode := range modes {
		t.Run(fmt.Sprintf("mode_%s", mode), func(t *testing.T) {
			var wg sync.WaitGroup
			bootstrapAddr := getFreeTCPPort(t)
			serverAddr := getFreeTCPPort(t)

			// 1. Start Bootstrap Server
			bSrv, err := bootstrap.StartBootstrapServer(bootstrapAddr, &wg)
			if err != nil {
				t.Fatalf("Failed to start Bootstrap: %v", err)
			}
			defer bSrv.GracefulStop()
			time.Sleep(50 * time.Millisecond)

			// 2. Start Malicious Server
			mSrv, err := startMaliciousServer(bootstrapAddr, serverAddr, mode, 1, 8, &wg)
			if err != nil {
				t.Fatalf("Failed to start Malicious Server: %v", err)
			}
			defer mSrv.GracefulStop()
			time.Sleep(100 * time.Millisecond)

			// 3. Run Client requesting layers 1 to 8
			taskID := fmt.Sprintf("malicious_client_test_%s_%d", mode, time.Now().Unix())
			err = client.RunClient(bootstrapAddr, 1, 8, taskID, 2, nil)
			if err == nil {
				t.Errorf("Expected Client to detect and reject malicious server (%s) computation, but it succeeded", mode)
			} else {
				t.Logf("Successfully caught security failure for mode %s: %v", mode, err)
			}
		})
	}
}

// startNormalServer is a helper that starts a normal server node
func startNormalServer(ctx context.Context, bootstrapAddr, addr string, wg *sync.WaitGroup) (*grpc.Server, error) {
	return server.StartServer(ctx, bootstrapAddr, 1, 4, addr, 5*time.Second, 2, nil, wg)
}
