# Calyx

Calyx is an ultra-lightweight, decentralized P2P network and runtime engine written in Go, inspired by the architecture of the **Petals** P2P framework. It enables low-spec client hardware to execute local Large Language Model (LLM) inference by sharding transformer layer blocks across a collaborative network of consumer-grade servers. By employing **Pipeline Parallelism**, intermediate token activations are streamed node-by-node using high-performance bidirectional gRPC streams, while remote **KV Caches** are dynamically updated and kept resident on supporting servers to bypass redundant computation.
> [!Note]
> We are still in the testing and PoC stage. I do not recommend using it for non-testing environments yet.
---

## Architecture

1. **High-Performance Pipeline Parallelism**: Sequential activation routing using persistent, lazy-initialized bidirectional gRPC streams, minimizing round-trip latency overhead.
2. **Decentralized KV Cache Residency**: Thread-safe task-indexed KV caching distributed across hosting servers, eliminating local VRAM/RAM constraints for deep inference chains.
3. **Dynamic TTL Memory Eviction**: Automated background daemons continuously monitor idle sessions and clean up expired caches, ensuring optimal system resource utilization.
4. **Model Discovery & Routing**: A dynamic Model Directory with a CLI dashboard queries active model capacities and layer slices, resolving the optimal pipeline path via a DHT overlay.
5. **Bi-directional Security & TEE Attestation**: Complete transport confidentiality using dynamic TLS 1.3 with mutual authentication (mTLS) backed by cryptographic TEE Hardware Enclave attestation (simulated or strict physical Intel SGX check).
6. **Network NAT Traversal**: Integrated RFC 5389 UDP STUN client that automatically discovers external public IPs and port mappings, making home-hosting accessible behind routers and firewalls.

---

## Bi-Directional Security Safeguards

Calyx implements a robust, bi-directional security architecture to protect all participants in the untrusted P2P network:

### Server-Side Protections (Against Malicious Clients)
* **IEEE 754 NaN/Infinity Scanners**: The server parses incoming tensors and rejects any inputs containing `NaN` or `Infinity` immediately, preventing numerical overflows or division-by-zero crashes.
* **Shape Invariant Validation**: Asserts that the product of the dimensions in the `Shape` attribute perfectly matches the actual size of the raw float slice (`len(data)`) before allocating memory.
* **Operational Physical Clamping**: Restricts float values to a safe physical boundary of `[-100.0, 100.0]`, guarding servers from numerical explosion exploits.

### Client-Side Protections (Against Malicious/Compromised Servers)
* **Computation Decay & Offset Checks**: Clients register dispatched activations in a thread-safe map and verify that received intermediate weights have not undergone sudden erasures or massive physical changes.
* **Lazy Computation Trapdoor**: Rejects all-zero activation slices (lazy server detections) and identical flat static arrays (indicating compromised nodes returning dummy default vectors).

*For a detailed look at the complete security threat model, mutual TLS designs, and mitigation blueprints, see the [Security Architecture Document](file:///home/io/workspace/connect/security_architecture.md).*

---

## TEE Enclave Security & Enclave Simulation Warning

Calyx supports Trusted Execution Environment (TEE) hardware enclave protection (Intel SGX / AMD SEV) to secure process memory from malicious host administrators.

### IMPORTANT: Enclave Simulation Warning

For development and portability, Calyx includes a **Software Simulation Mode** for enclaves. 
When simulated, the enclave attestation quote is generated using simulated cryptographic keys in standard process memory.

> [!WARNING]
> **SIMULATION MODE IS NOT SECURE FOR PUBLIC DEPLOYMENTS!**
> In simulation mode, process memory is **NOT** encrypted by hardware CPU protection. 
> A malicious host administrator or local attacker can easily dump the process memory and steal sensitive model weights, client activations, or private keys. 
> Never use simulated enclaves in public production networks.

### Enclave Configuration Options

You can control enclave behavior and toggle between simulated and strict physical hardware modes using the following CLI flags:

| Flag | Type | Default | Description |
|---|---|---|---|
| `-tee-enclave` | `bool` | `true` | Enable/Disable secure TEE hardware enclave protection in the application. |
| `-enclave-simulation` | `bool` | `true` | When TEE is enabled, controls whether it runs in **Simulation Mode** (software-based) or **Strict Mode** (demands physical SGX). |

#### Enforcing Strict Hardware Enclaves
To run in **Strict Hardware Mode** (disabling simulated enclaves entirely and enforcing physical Intel SGX check):
```bash
go run main.go -mode=server -addr=localhost:50051 -tee-enclave=true -enclave-simulation=false
```
*Note: In strict mode, the application will audit system device nodes (e.g. `/dev/sgx_enclave`, `/dev/sgx`) and immediately fail startup with a secure error if no physical Intel SGX hardware/driver is present on the host.*

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

### Mode 3: Calyx Hybrid Distributed Sandbox (Docker Compose)

For a fully containerized environment that integrates the Go control plane with a native C++ high-performance data plane (offloading layers dynamically and sharing KV caches for GGUF models in real-time), we provide a dedicated Docker Compose sandbox.

Refer to the [Sandbox README](file:///home/io/workspace/calyx/sandbox/README.md) for more details and complete setup instructions.

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

### Practical Use Cases & Deployment Scenarios

Calyx's lightweight P2P pipeline parallel runtime is designed for developers, researchers, and decentralized application creators who want to democratize AI compute. Some primary deployment scenarios include:

1. **Decentralized AI Agent Swarms**:
   * *Scenario*: Autonomous software agents (e.g., coding assistants, automated interpreters, web research agents) running locally on consumer hardware.
   * *Benefit*: Instead of paying high API subscription fees or requiring local multi-GPU setups to run large model parameters, agents act as lightweight P2P clients that offload transformer computations across a trustless cluster of collaborative peers.

2. **Collaborative Consumer-Grade Hosting**:
   * *Scenario*: A community or enterprise wants to host a customized deep neural network model without relying on monopolized cloud hyperscalers.
   * *Benefit*: Individual members commit fractional compute resources (e.g. sharing 4 to 8 model layers using standard household CPUs or GPUs), aggregating memory bandwidth to run powerful pipelines collaboratively.

3. **Privacy-Preserving Edge Computing**:
   * *Scenario*: Local smart devices or localized enterprise networks that cannot leak private, sensitive input data (like environment variables, configuration files, or proprietary logic) to central cloud authorities.
   * *Benefit*: Combining client-side Differential Privacy (DP noise injection) with fragmented multi-node layer routing ensures that no single server node ever intercepts or reconstructs the full prompt context or model activations.

4. **Zero-Friction Local AI Development (Dev Swarms)**:
   * *Scenario*: A local team of software developers working on codebases wants to share GPU resources on their office network.
   * *Benefit*: Teammates run Calyx servers in the background of their workstations, pooling unused desktop GPU/CPU power into a local cluster that any developer's IDE agent can query instantly via standard gRPC APIs.

--- 

MIT
