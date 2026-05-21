# Calyx

This is a decentralized P2P network developed in Go that replicates the fundamental architectural blocks of the **Petals** decentralized P2P network. The objective is to allow client devices (representing weak consumer hardware) to process giant sequence lengths by delegating specific Transformer layer blocks to support servers, maintaining and updating the **KV Cache** remotely across **gRPC bidirectional streams** (Pipeline Parallelism).

---

## Key Features

1. **True Pipeline Parallelism**: Activations are streamed node-by-node using persistent, lazy-initialized bidirectional gRPC streams to minimize round-trip connection overheads.
2. **Decentralized KV Cache**: Each server node retains keys and values for its assigned subset of layers under a thread-safe task-indexed cache, avoiding recomputations for subsequent tokens.
3. **Dynamic TTL Cache Eviction**: Background workers in each server monitor idle tasks and automatically purge expired KV Caches, freeing up system memory.
4. **Decentralized Route Planning**: A mock DHT (Bootstrap Node) keeps track of active nodes and registers layer capacities, offering a routing API `FindRoute(startLayer, endLayer)` to formulate the optimal execution path.

---

## How to Run

You can run the codebase in two modes:

### Mode 1: Automated Integrated Demo (Recommended)

Spins up the entire network (1 Bootstrap Node, 2 Server Nodes, and 1 Client Node) inside a single process. It processes 3 successive tokens, displays KV Cache growth, and exhibits the automated background TTL cleanup.

```bash
go run main.go
```

**What happens under the hood:**
1. Starts the Bootstrap server on `:50050`.
2. Starts **Server 1 (Layers 1-4)** on `:50051`.
3. Starts **Server 2 (Layers 5-8)** on `:50052`.
4. Both servers register their capacities with the Bootstrap Node.
5. The Client requests a route for layers 1 to 8, plans the chain: `[Client] -> [Server 1] -> [Server 2] -> [Client]`, and connects to the pipeline.
6. The Client dispatches 3 token activations sequentially, updating and growing the remote KV caches.
7. After the client task completes, the servers wait for 4 seconds of idle time and evict the task's KV cache automatically.

---

### Mode 2: Independent Process Deployment (Multi-Terminal)

To simulate a real decentralized environment, run each node in separate terminal windows:

#### 1. Start the Bootstrap / Coordinator Node
```bash
go run main.go -mode=bootstrap -addr=localhost:50050
```

#### 2. Start Server Node 1 (Layers 1 to 4)
```bash
go run main.go -mode=server -addr=localhost:50051 -bootstrap=localhost:50050 -start=1 -end=4 -ttl=10m
```

#### 3. Start Server Node 2 (Layers 5 to 8)
```bash
go run main.go -mode=server -addr=localhost:50052 -bootstrap=localhost:50050 -start=5 -end=8 -ttl=10m
```

#### 4. Run the Client Node to process layers 1 to 8
```bash
go run main.go -mode=client -bootstrap=localhost:50050 -start=1 -end=8 -task=my_custom_task_id
```

---

## Automated Testing & Integration Suite

We have established a comprehensive, automated multi-level testing architecture to ensure high stability and seamless communication between P2P nodes.

### Local Command Orchestration (Makefile)

A [Makefile](file:///home/io/workspace/connect/Makefile) is provided to standardize and automate tasks:

* **Compile the Binary**:
  ```bash
  make build
  ```
* **Run Unit & Package-Level Tests** (spinning up nodes on random, dynamic loopback TCP sockets):
  ```bash
  make test
  ```
* **Run Black-Box CLI Process-Level E2E Tests** (compiling the binary and executing separate background OS processes for nodes, verifying full inter-process streams and cleaning up PIDs cleanly):
  ```bash
  make e2e-test
  ```
* **Run Interactive Demo**:
  ```bash
  make run-demo
  ```
* **Clean Build Artifacts & Logs**:
  ```bash
  make clean
  ```

---

### Process-Level E2E Test Runner (`scripts/run_e2e_tests.sh`)

A dedicated black-box [run_e2e_tests.sh](file:///home/io/workspace/connect/scripts/run_e2e_tests.sh) script handles the lifecycle of standalone node processes:
1. Automatically compiles the latest CLI binary.
2. Spawns a background **Bootstrap Node** (`:50100`), **Server 1** (`:50101`, layers 1-4), and **Server 2** (`:50102`, layers 5-8 with 3s TTL).
3. Verifies that all server processes register correctly with the coordinator.
4. Executes the **Client Node** to process layers 1 to 8, feeding sequence activations sequentially.
5. Performs validation checks on files inside the `test_logs/` directory to assert remote KV Cache growth and successful pipeline execution.
6. Employs a robust `trap` mechanism to terminate background node PIDs cleanly upon completion or interruption.

---

### Continuous Integration (GitHub Actions)

A GitHub Actions workflow is configured under `.github/workflows/go-ci.yml`. It runs automatically on every commit push or pull request to:
* Validate code formatting (`gofmt`).
* Analyze code statically (`go vet`).
* Compile the project binary.
* Execute the package-level unit and integration test suite (`make test`).
* Execute the black-box CLI E2E process suite (`make e2e-test`).
