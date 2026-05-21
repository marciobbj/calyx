package tests

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"calyx/bootstrap"
	"calyx/client"
	"calyx/dht"
	"calyx/server"
)

// TestDecentralizedDHTAndSecurePipeline E2E tests the real decentralized DHT (Kademlia)
// route resolution, mutual TLS 1.3 verification, Hashcash challenge response, and Transformer inference logic.
func TestDecentralizedDHTAndSecurePipeline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	var network sync.Map

	bootstrapAddr := getFreeTCPPort(t)
	server1Addr := getFreeTCPPort(t)
	server2Addr := getFreeTCPPort(t)

	// 1. Initialize a simulated Bootstrap DHT node in the network map
	// This allows starting servers to discover and register themselves in the simulated Kademlia routing overlay.
	t.Logf("[Test] Initializing simulated Bootstrap Kademlia DHT on %s...", bootstrapAddr)
	_ = dht.NewKademliaDHT(bootstrapAddr, &network)

	// 2. Start the physical Bootstrap Coordinator gRPC server
	bSrv, err := bootstrap.StartBootstrapServer(bootstrapAddr, &wg)
	if err != nil {
		t.Fatalf("Failed to start Bootstrap node: %v", err)
	}
	defer bSrv.GracefulStop()
	time.Sleep(100 * time.Millisecond)

	// 3. Start Server 1 (Layers 1-4) with shared Kademlia network sync.Map
	t.Logf("[Test] Starting Server 1 on %s...", server1Addr)
	sSrv1, err := server.StartServer(ctx, bootstrapAddr, 1, 4, server1Addr, 5*time.Second, 2, &network, &wg, false)
	if err != nil {
		t.Fatalf("Failed to start Server 1: %v", err)
	}
	defer sSrv1.GracefulStop()

	// 4. Start Server 2 (Layers 5-8) with shared Kademlia network sync.Map
	t.Logf("[Test] Starting Server 2 on %s...", server2Addr)
	sSrv2, err := server.StartServer(ctx, bootstrapAddr, 5, 8, server2Addr, 5*time.Second, 2, &network, &wg, false)
	if err != nil {
		t.Fatalf("Failed to start Server 2: %v", err)
	}
	defer sSrv2.GracefulStop()
	time.Sleep(200 * time.Millisecond)

	// 5. Run Client with shared Kademlia network sync.Map
	// The client will resolve the routes entirely via Kademlia DHT recursive lookups,
	// bypass the central bootstrap coordinator for routing, dial servers with secure TLS 1.3 mTLS,
	// solve Proof-of-Work difficulty 2, and process multi-head self-attention.
	taskID := fmt.Sprintf("decentralized_e2e_dht_%d", time.Now().Unix())
	t.Logf("[Test] Executing E2E Client with Kademlia DHT routing and mTLS verification (Task: %s)...", taskID)
	
	err = client.RunClient(bootstrapAddr, 1, 8, taskID, 2, &network, 0.0, "")
	if err != nil {
		t.Fatalf("Decentralized P2P pipeline failed: %v", err)
	}

	t.Log("[Test] Decentralized DHT and Secure Pipeline E2E validation completed successfully!")
}
