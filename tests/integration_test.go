package tests

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"calyx/bootstrap"
	"calyx/client"
	"calyx/server"
)

// getFreeTCPPort returns a dynamically available free TCP port on localhost
func getFreeTCPPort(t *testing.T) string {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Integration setup failed - could not bind free port: %v", err)
	}
	defer lis.Close()
	return lis.Addr().String()
}

// TestMultiNodePipelineSuccess verifies a full P2P cluster processing sequence tokens
func TestMultiNodePipelineSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	bootstrapAddr := getFreeTCPPort(t)
	server1Addr := getFreeTCPPort(t)
	server2Addr := getFreeTCPPort(t)

	// 1. Start Bootstrap Server
	bSrv, err := bootstrap.StartBootstrapServer(bootstrapAddr, &wg)
	if err != nil {
		t.Fatalf("Failed to start Bootstrap node: %v", err)
	}
	defer bSrv.GracefulStop()
	time.Sleep(50 * time.Millisecond)

	// 2. Start Server 1 (Layers 1-4)
	sSrv1, err := server.StartServer(ctx, bootstrapAddr, 1, 4, server1Addr, 5*time.Second, &wg)
	if err != nil {
		t.Fatalf("Failed to start Server 1: %v", err)
	}
	defer sSrv1.GracefulStop()

	// 3. Start Server 2 (Layers 5-8)
	sSrv2, err := server.StartServer(ctx, bootstrapAddr, 5, 8, server2Addr, 5*time.Second, &wg)
	if err != nil {
		t.Fatalf("Failed to start Server 2: %v", err)
	}
	defer sSrv2.GracefulStop()
	time.Sleep(100 * time.Millisecond)

	// 4. Run Client sequence (Layers 1-8)
	taskID := fmt.Sprintf("integration_success_%d", time.Now().Unix())
	err = client.RunClient(bootstrapAddr, 1, 8, taskID)
	if err != nil {
		t.Fatalf("Expected multi-node P2P pipeline to succeed, but got error: %v", err)
	}
}

// TestRoutingFailureNoCoverage verifies route discovery handles gaps in node layers correctly
func TestRoutingFailureNoCoverage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	bootstrapAddr := getFreeTCPPort(t)
	server1Addr := getFreeTCPPort(t)

	// 1. Start Bootstrap Server
	bSrv, err := bootstrap.StartBootstrapServer(bootstrapAddr, &wg)
	if err != nil {
		t.Fatalf("Failed to start Bootstrap: %v", err)
	}
	defer bSrv.GracefulStop()
	time.Sleep(50 * time.Millisecond)

	// 2. Start only Server 1 (Layers 1-4) - leaving Layers 5-8 missing
	sSrv1, err := server.StartServer(ctx, bootstrapAddr, 1, 4, server1Addr, 5*time.Second, &wg)
	if err != nil {
		t.Fatalf("Failed to start Server 1: %v", err)
	}
	defer sSrv1.GracefulStop()
	time.Sleep(100 * time.Millisecond)

	// 3. Run Client requesting layers 1 to 8 (expecting failure since layers 5-8 are uncovered)
	taskID := fmt.Sprintf("integration_fail_coverage_%d", time.Now().Unix())
	err = client.RunClient(bootstrapAddr, 1, 8, taskID)
	if err == nil {
		t.Errorf("Expected Client execution to fail due to incomplete route coverage, but it succeeded")
	} else {
		t.Logf("Successfully caught expected route failure: %v", err)
	}
}

// TestPipelineMidStreamFailure verifies the pipeline responds properly to downstream node crashes
func TestPipelineMidStreamFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	bootstrapAddr := getFreeTCPPort(t)
	server1Addr := getFreeTCPPort(t)
	server2Addr := getFreeTCPPort(t)

	// 1. Start Bootstrap Server
	bSrv, err := bootstrap.StartBootstrapServer(bootstrapAddr, &wg)
	if err != nil {
		t.Fatalf("Failed to start Bootstrap: %v", err)
	}
	defer bSrv.GracefulStop()
	time.Sleep(50 * time.Millisecond)

	// 2. Start Server 1 (Layers 1-4)
	sSrv1, err := server.StartServer(ctx, bootstrapAddr, 1, 4, server1Addr, 5*time.Second, &wg)
	if err != nil {
		t.Fatalf("Failed to start Server 1: %v", err)
	}
	defer sSrv1.GracefulStop()

	// 3. Start Server 2 (Layers 5-8)
	sSrv2, err := server.StartServer(ctx, bootstrapAddr, 5, 8, server2Addr, 5*time.Second, &wg)
	if err != nil {
		t.Fatalf("Failed to start Server 2: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// 4. Kill Server 2 right away before client starts, or mid-stream
	// We stop Server 2 so that when Client streams to Server 1, Server 1 cannot connect downstream
	sSrv2.GracefulStop()
	time.Sleep(100 * time.Millisecond)

	// 5. Execute Client - Server 1 should fail to forward and client should receive a clean error
	taskID := fmt.Sprintf("integration_mid_fail_%d", time.Now().Unix())
	err = client.RunClient(bootstrapAddr, 1, 8, taskID)
	if err == nil {
		t.Errorf("Expected E2E pipeline to fail since Server 2 went offline, but it succeeded")
	} else {
		t.Logf("Successfully caught expected E2E pipeline failure: %v", err)
	}
}
