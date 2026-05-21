# Quick Start Guide: Real-World LLM Workflows on Calyx

This guide explains how to leverage the **Calyx P2P Network** to execute real-world large language model workflows (such as Google's Gemma-2B/7B) by splitting model layers across decentralized participants. It also outlines how nodes configure their resource boundaries to prevent overloading.

---

## 1. Architectural Concept: Slicing Gemma

A standard Gemma model (e.g., Gemma-2B containing 18 layers) is split across the network. Instead of a single weak machine running the whole model, three independent nodes host subsets of the layers:

```
[Local Agent / Client] 
      │ 
      ▼  (Tokenize & Embed)
 [Embeddings Tensor]
      │
      ▼  (mTLS gRPC Stream)
 [Server A (Layers 1-6)]   ◄─── Solves Hashcash PoW to access
      │
      ▼  (mTLS Forward)
 [Server B (Layers 7-12)]
      │
      ▼  (mTLS Forward)
 [Server C (Layers 13-18)]
      │
      ▼  (mTLS Return)
 [Local Agent / Client]   ◄─── Receives finished hidden states & decodes
```

---

## 2. Quick Start: Setting Up a Resource Provider (Server Node)

If you have a local GPU/CPU and want to share a segment of the Gemma model (e.g., layers 1 to 6) with the network, you run a Calyx Server node.

### A. Load Real Weights into the Engine
To hook up real Gemma weights, implement a reader to map weights from a standard `.gguf` file or `safetensors` file into the `TransformerLayer` variables in `engine/transformer.go`:

```go
package main

import (
	"calyx/engine"
	"log"
)

func main() {
	// Initialize a transformer layer matching Gemma-2B dimensions (hiddenDim: 2048)
	layer := engine.NewTransformerLayer(2048)
	
	// Load actual weights from a local GGUF model file
	err := loadGemmaWeights(layer, "models/gemma-2b-it.gguf", 1) // Layer Index 1
	if err != nil {
		log.Fatalf("Failed to load Gemma weights: %v", nil)
	}
}
```

### B. Launch Server Node
Launch your node, defining its available port, layer segment capacity, and registration details:
```bash
./bin/connect -mode=server -addr=your-public-ip:50051 -bootstrap=bootstrap-node-ip:50050 -start=1 -end=6 -ttl=5m -difficulty=3
```

---

## 3. Quick Start: Running a Local Agent (Client Node)

To execute a prompt utilizing the shared resources of the cluster:

1. **Local Tokenization**: Your agent parses the prompt text locally into token IDs and extracts the initial activations embedding tensor:
   ```go
   tokens := tokenizer.Encode("Write a python script to reverse a string", true)
   inputTensor := embeddingLayer.Embed(tokens) // Dimension: [1, seq_len, 2048]
   ```
2. **Dynamic Route Finding**: Start the client node. It uses Kademlia DHT recursive lookups (`dht/kademlia.go`) to discover peer nodes providing the required layer sequence:
   ```bash
   ./bin/connect -mode=client -bootstrap=bootstrap-node-ip:50050 -start=1 -end=18 -task=gemma_inference_101 -difficulty=3
   ```
3. **Execution**: The client solves the Hashcash Proof-of-Work puzzle for the target nodes, dials them using mTLS 1.3, streams the embedding tensor down the pipeline, and collects the completed predictions.

---

## 4. Preventing Participant Overload: Resource Configuration

In a decentralized network, there is a risk that a node with limited hardware might be flooded with too many requests, leading to Out-Of-Memory (OOM) crashes or CPU/GPU thread starvation.

Calyx addresses this with **four built-in, configurable limits** that allow participants to control exactly how much resource they dedicate:

### A. Dynamic KV Cache TTL Slicing (Memory Control)
To prevent your machine's RAM from being consumed by inactive sessions, the server executes a background TTL daemon. You configure the cache duration using the `-ttl` flag:
* **High Memory Machine**: `-ttl=30m` (keeps KV Cache active for 30 minutes to facilitate fast follow-up tokens).
* **Low Memory Machine**: `-ttl=10s` (aggressively cleans up memory allocations after 10 seconds of inactivity).

```go
// Background TTL worker (server/server.go)
if time.Since(entry.LastAccessed) > s.ttl {
    s.kvCache.Delete(taskID) // Frees up RAM allocations instantly
}
```

### B. Scalable Rate Limiting via Hashcash (CPU/GPU Protection)
Each server configures a `-difficulty` flag (default `2`). If a server starts experiencing heavy load, it can dynamically increase its difficulty (e.g., to `4` or `5` leading zero hexadecimal characters):
* Increasing this setting forces clients to spend more computational time solving the puzzle *before* the server accepts the gRPC handshake.
* This naturally rate-limits incoming connections, preventing botnets, spam attacks, or Sybil exploits from starving your local CPU/GPU threads.

### C. Concurrency Stream Limits
You can restrict the maximum number of concurrent clients your node will serve simultaneously in `server/server.go` by modifying the gRPC server parameters:
```go
// Configuring max concurrent streams to protect resources
grpcServer := grpc.NewServer(
	grpc.Creds(credentials.NewTLS(tlsCfg)),
	grpc.MaxConcurrentStreams(4), // Limits node to 4 simultaneous client streams
)
```

### D. Layer Segment Slicing
You can choose exactly how many layers to host. Hosting 1 layer requires very little GPU VRAM, whereas hosting 12 layers requires substantial VRAM. You configure this easily on startup:
* **Low resource allocation (1 layer)**: `-start=1 -end=1`
* **High resource allocation (12 layers)**: `-start=1 -end=12`
