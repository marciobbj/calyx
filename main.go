package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"connect/bootstrap"
	"connect/client"
	"connect/server"
)

func main() {
	// 1. Define CLI flags for flexible multi-process execution
	modeFlag := flag.String("mode", "demo", "Mode to run: 'bootstrap', 'server', 'client', or 'demo'")
	addrFlag := flag.String("addr", "", "Address to bind or connect to")
	bootstrapAddrFlag := flag.String("bootstrap", "localhost:50050", "Address of the bootstrap node")
	startLayerFlag := flag.Int("start", 1, "Starting layer (for server mode)")
	endLayerFlag := flag.Int("end", 8, "Ending layer (for server mode)")
	ttlFlag := flag.Duration("ttl", 10*time.Minute, "KV Cache TTL (e.g. 10m, 5s)")
	taskIDFlag := flag.String("task", "task_petals_go", "Unique task identifier")

	flag.Parse()

	// Configure standard log layout to make visual logging clean and neat
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	switch *modeFlag {
	case "bootstrap":
		runBootstrapMode(*addrFlag, *bootstrapAddrFlag)
	case "server":
		runServerMode(*addrFlag, *bootstrapAddrFlag, int32(*startLayerFlag), int32(*endLayerFlag), *ttlFlag)
	case "client":
		runClientMode(*bootstrapAddrFlag, int32(*startLayerFlag), int32(*endLayerFlag), *taskIDFlag)
	case "demo":
		runAutomatedDemo(*ttlFlag)
	default:
		fmt.Printf("Unknown mode: %s. Use -mode with 'bootstrap', 'server', 'client', or 'demo'\n", *modeFlag)
		os.Exit(1)
	}
}

func runBootstrapMode(addr, bootstrapAddr string) {
	if addr == "" {
		addr = bootstrapAddr
	}
	log.Printf("[Main] Starting Bootstrap Node in standalone mode...")
	var wg sync.WaitGroup
	srv, err := bootstrap.StartBootstrapServer(addr, &wg)
	if err != nil {
		log.Fatalf("[Main] Failed to start Bootstrap: %v", err)
	}

	handleShutdown(func() {
		srv.GracefulStop()
	}, &wg)
}

func runServerMode(addr, bootstrapAddr string, startLayer, endLayer int32, ttl time.Duration) {
	if addr == "" {
		log.Fatal("[Main] Error: -addr parameter is required in 'server' mode (e.g. -addr=localhost:50051)")
	}
	log.Printf("[Main] Starting standalone Server Node (Layers %d-%d) on %s...", startLayer, endLayer, addr)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	srv, err := server.StartServer(ctx, bootstrapAddr, startLayer, endLayer, addr, ttl, &wg)
	if err != nil {
		cancel()
		log.Fatalf("[Main] Failed to start Server: %v", err)
	}

	handleShutdown(func() {
		cancel()
		srv.GracefulStop()
	}, &wg)
}

func runClientMode(bootstrapAddr string, startLayer, endLayer int32, taskID string) {
	log.Printf("[Main] Starting standalone Client Node...")
	err := client.RunClient(bootstrapAddr, startLayer, endLayer, taskID)
	if err != nil {
		log.Fatalf("[Main] Error running Client: %v", err)
	}
}

func runAutomatedDemo(customTTL time.Duration) {
	log.Println("================================================================================")
	log.Println("   PETALS DECENTRALIZED P2P ARCHITECTURE - AUTOMATED DEMO IN GO")
	log.Println("================================================================================")
	
	// For the demo, if no custom TTL was passed, we set it to 4 seconds so the user
	// can see the TTL cache cleanup happening visually in real-time without waiting 10 minutes.
	demoTTL := customTTL
	if demoTTL == 10*time.Minute { // Default value
		demoTTL = 4 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	bootstrapAddr := "localhost:50050"
	server1Addr := "localhost:50051"
	server2Addr := "localhost:50052"

	// 1. Start Bootstrap Server
	log.Printf("[Main] 1. Initializing Coordinator / DHT (Bootstrap) on %s...", bootstrapAddr)
	bSrv, err := bootstrap.StartBootstrapServer(bootstrapAddr, &wg)
	if err != nil {
		log.Fatalf("[Main] Failed to start Bootstrap server for Demo: %v", err)
	}
	time.Sleep(300 * time.Millisecond) // Give standard TCP listener time to bind

	// 2. Start Server Node 1 (Layers 1-4)
	log.Printf("[Main] 2. Initializing Server 1 (Layers 1-4) on %s...", server1Addr)
	sSrv1, err := server.StartServer(ctx, bootstrapAddr, 1, 4, server1Addr, demoTTL, &wg)
	if err != nil {
		log.Fatalf("[Main] Failed to start Server 1 for Demo: %v", err)
	}

	// 3. Start Server Node 2 (Layers 5-8)
	log.Printf("[Main] 3. Initializing Server 2 (Layers 5-8) on %s...", server2Addr)
	sSrv2, err := server.StartServer(ctx, bootstrapAddr, 5, 8, server2Addr, demoTTL, &wg)
	if err != nil {
		log.Fatalf("[Main] Failed to start Server 2 for Demo: %v", err)
	}
	time.Sleep(500 * time.Millisecond) // Wait for registration to complete fully

	// 4. Start Client Pipeline execution
	taskID := fmt.Sprintf("task_demo_%d", time.Now().Unix())
	log.Printf("[Main] 4. Running Client Node to process layers 1 to 8 with TaskID '%s'", taskID)
	
	err = client.RunClient(bootstrapAddr, 1, 8, taskID)
	if err != nil {
		log.Printf("[Main] Error running Client in Demo: %v", err)
	}

	// 5. Keep services alive to demonstrate the TTL KV Cache cleanup in action
	log.Printf("[Main] 5. Waiting for TTL expiration (%s) to view KV Cache eviction...", demoTTL)
	
	// Sleep slightly longer than the TTL to ensure the background checker triggers the deletion
	time.Sleep(demoTTL + 1500*time.Millisecond)

	// Graceful shutdown of servers
	log.Println("[Main] Demo completed! Shutting down servers gracefully...")
	cancel() // Stops server background TTL daemons
	bSrv.GracefulStop()
	sSrv1.GracefulStop()
	sSrv2.GracefulStop()

	wg.Wait()
	log.Println("================================================================================")
	log.Println("   END OF DEMONSTRATION - PETALS P2P GO COMPLETED SUCCESSFULLY!")
	log.Println("================================================================================")
}

func handleShutdown(stopFunc func(), wg *sync.WaitGroup) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("\n[Main] Shutdown signal received. Stopping servers...")
	stopFunc()
	wg.Wait()
	log.Println("[Main] Shutdown completed successfully.")
}
