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

	"calyx/bootstrap"
	"calyx/client"
	"calyx/crypto"
	"calyx/server"
)

func main() {
	// 1. Define CLI flags for flexible multi-process execution
	modeFlag := flag.String("mode", "demo", "Mode to run: 'bootstrap', 'server', 'client', 'list-models', or 'demo'")
	addrFlag := flag.String("addr", "", "Address to bind or connect to")
	bootstrapAddrFlag := flag.String("bootstrap", "localhost:50050", "Address of the bootstrap node")
	startLayerFlag := flag.Int("start", 1, "Starting layer (for server mode)")
	endLayerFlag := flag.Int("end", 8, "Ending layer (for server mode)")
	ttlFlag := flag.Duration("ttl", 10*time.Minute, "KV Cache TTL (e.g. 10m, 5s)")
	taskIDFlag := flag.String("task", "task_calyx_go", "Unique task identifier")
	difficultyFlag := flag.Int("difficulty", 2, "Hashcash Proof-of-Work puzzle difficulty (number of leading zeros)")
	dpNoiseFlag := flag.Float64("dp-noise", 0.001, "Standard deviation of Differential Privacy Gaussian noise (0.0 to disable)")
	teeEnclaveFlag := flag.Bool("tee-enclave", true, "Enable secure hardware enclaves (Intel SGX / AMD SEV)")
	enclaveSimulationFlag := flag.Bool("enclave-simulation", true, "Enable simulated enclave mode (if false, strict physical hardware mode is enforced)")
	weightsFlag := flag.String("weights", "bin/layer_weights.bin", "Path to the binary transformer layer weights file")
	modelFlag := flag.String("model", "google/gemma-2b", "Model ID served or requested by the node")
	stunServerFlag := flag.String("stun-server", "stun.l.google.com:19302", "STUN server address for NAT traversal")

	flag.Parse()

	// Propagate configuration flags to packages
	crypto.EnclaveSimulation = *enclaveSimulationFlag
	server.WeightsPath = *weightsFlag
	server.ModelID = *modelFlag
	server.StunServer = *stunServerFlag
	client.ModelID = *modelFlag

	if *teeEnclaveFlag && *enclaveSimulationFlag {
		log.Println("################################################################################")
		log.Println(" WARNING: TEE ENCLAVE RUNNING IN SIMULATION/DEVELOPMENT MODE!                    ")
		log.Println(" The process memory is NOT protected by hardware CPU encryption (SGX/SEV).      ")
		log.Println(" Sensitive model weights and client activations are vulnerable to memory dumps. ")
		log.Println(" DO NOT USE THIS SIMULATION IN PUBLIC PRODUCTION ENVIRONMENTS!                  ")
		log.Println("################################################################################")
	}

	// Configure standard log layout to make visual logging clean and neat
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	switch *modeFlag {
	case "bootstrap":
		runBootstrapMode(*addrFlag, *bootstrapAddrFlag)
	case "server":
		runServerMode(*addrFlag, *bootstrapAddrFlag, int32(*startLayerFlag), int32(*endLayerFlag), *ttlFlag, *difficultyFlag, *teeEnclaveFlag)
	case "client":
		runClientMode(*bootstrapAddrFlag, int32(*startLayerFlag), int32(*endLayerFlag), *taskIDFlag, *difficultyFlag, *dpNoiseFlag)
	case "list-models":
		runListModelsMode(*bootstrapAddrFlag)
	case "demo":
		runAutomatedDemo(*ttlFlag, *difficultyFlag, *dpNoiseFlag, *teeEnclaveFlag)
	default:
		fmt.Printf("Unknown mode: %s. Use -mode with 'bootstrap', 'server', 'client', 'list-models', or 'demo'\n", *modeFlag)
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

func runServerMode(addr, bootstrapAddr string, startLayer, endLayer int32, ttl time.Duration, powDifficulty int, teeEnclave bool) {
	if addr == "" {
		log.Fatal("[Main] Error: -addr parameter is required in 'server' mode (e.g. -addr=localhost:50051)")
	}
	log.Printf("[Main] Starting standalone Server Node (Layers %d-%d) on %s with Hashcash Difficulty: %d, TEE Enclave: %t...", startLayer, endLayer, addr, powDifficulty, teeEnclave)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	srv, err := server.StartServer(ctx, bootstrapAddr, startLayer, endLayer, addr, ttl, powDifficulty, nil, &wg, teeEnclave)
	if err != nil {
		cancel()
		log.Fatalf("[Main] Failed to start Server: %v", err)
	}

	handleShutdown(func() {
		cancel()
		srv.GracefulStop()
	}, &wg)
}

func runClientMode(bootstrapAddr string, startLayer, endLayer int32, taskID string, powDifficulty int, dpNoise float64) {
	log.Printf("[Main] Starting standalone Client Node with DP noise standard deviation: %f...", dpNoise)
	err := client.RunClient(bootstrapAddr, startLayer, endLayer, taskID, powDifficulty, nil, dpNoise, "")
	if err != nil {
		log.Fatalf("[Main] Error running Client: %v", err)
	}
}

func runAutomatedDemo(customTTL time.Duration, powDifficulty int, dpNoise float64, teeEnclave bool) {
	log.Println("================================================================================")
	log.Println("   CALYX DECENTRALIZED P2P ARCHITECTURE - AUTOMATED DEMO IN GO")
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
	log.Printf("[Main] 2. Initializing Server 1 (Layers 1-4) on %s (TEE: %t)...", server1Addr, teeEnclave)
	sSrv1, err := server.StartServer(ctx, bootstrapAddr, 1, 4, server1Addr, demoTTL, powDifficulty, nil, &wg, teeEnclave)
	if err != nil {
		log.Fatalf("[Main] Failed to start Server 1 for Demo: %v", err)
	}

	// 3. Start Server Node 2 (Layers 5-8)
	log.Printf("[Main] 3. Initializing Server 2 (Layers 5-8) on %s (TEE: %t)...", server2Addr, teeEnclave)
	sSrv2, err := server.StartServer(ctx, bootstrapAddr, 5, 8, server2Addr, demoTTL, powDifficulty, nil, &wg, teeEnclave)
	if err != nil {
		log.Fatalf("[Main] Failed to start Server 2 for Demo: %v", err)
	}
	time.Sleep(500 * time.Millisecond) // Wait for registration to complete fully

	// 4. Start Client Pipeline execution
	taskID := fmt.Sprintf("task_demo_%d", time.Now().Unix())
	log.Printf("[Main] 4. Running Client Node to process layers 1 to 8 with TaskID '%s' (DP Noise: %f)", taskID, dpNoise)

	expectedMRENCLAVE := ""
	if teeEnclave {
		expectedMRENCLAVE = crypto.DefaultMRENCLAVE
	}

	err = client.RunClient(bootstrapAddr, 1, 8, taskID, powDifficulty, nil, dpNoise, expectedMRENCLAVE)
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
	log.Println("   END OF DEMONSTRATION - CALYX P2P GO COMPLETED SUCCESSFULLY!")
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

func runListModelsMode(bootstrapAddr string) {
	err := client.FetchAndListModels(bootstrapAddr)
	if err != nil {
		log.Fatalf("[Main] Error listing models: %v", err)
	}
}
