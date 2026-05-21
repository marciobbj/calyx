package client

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"connect/bootstrap"
	"connect/server"
)

// getFreeTCPPort returns a free TCP port available on localhost
func getFreeTCPPort(t *testing.T) string {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to bind free port: %v", err)
	}
	defer lis.Close()
	return lis.Addr().String()
}

func TestClientEndToEndPipeline(t *testing.T) {
	// 1. Setup contexts and dynamic ports
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	bootstrapAddr := getFreeTCPPort(t)
	serverAddr := getFreeTCPPort(t)

	// 2. Start Bootstrap Node
	bSrv, err := bootstrap.StartBootstrapServer(bootstrapAddr, &wg)
	if err != nil {
		t.Fatalf("Failed to start Bootstrap server: %v", err)
	}
	defer bSrv.GracefulStop()

	// Short sleep to allow bootstrap server to bind and start serving
	time.Sleep(100 * time.Millisecond)

	// 3. Start Server Node (Layers 1-4)
	// We use a short TTL (1s) to make sure it doesn't leak cache on long runs
	sSrv, err := server.StartServer(ctx, bootstrapAddr, 1, 4, serverAddr, 1*time.Second, &wg)
	if err != nil {
		t.Fatalf("Failed to start Server node: %v", err)
	}
	defer sSrv.GracefulStop()

	// Short sleep to complete server registration with bootstrap
	time.Sleep(100 * time.Millisecond)

	// 4. Run Client Pipeline for layers 1-4 (covered by our registered server node)
	taskID := fmt.Sprintf("test_task_e2e_%d", time.Now().Unix())
	
	// Execute RunClient and assert success
	err = RunClient(bootstrapAddr, 1, 4, taskID)
	if err != nil {
		t.Fatalf("E2E Pipeline execution failed: %v", err)
	}
}
