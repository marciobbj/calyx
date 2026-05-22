# Calyx Hybrid Distributed Sandbox

This sandbox demonstrates an **advanced hybrid architecture** of the Calyx P2P network, orchestrating layer-sharded pipeline parallel inference and remote KV cache sharing over containerized environments.

The control plane is driven by the secure Calyx Go network, while the high-performance data plane is powered by compiled C++ RPC workers. 

While our validation runs and demonstration metrics were generated using the `phi-4` reasoning model, **the sandbox architecture is fully generic**. You can run this distributed pipeline with any compatible GGUF model of your choice without changing the code or hardcoding any specific model filenames.

---

## Hybrid P2P Architecture

The sandbox separates execution concerns into two highly integrated layers:

1. **Calyx Go Control Plane (Orchestration & Security)**:
   - **`bootstrap` (Go)**: A standalone central routing coordinator and global model discovery directory.
   - **`server1` & `server2` (Go)**: Nodes that join the Calyx network, announce served layers via the Kademlia DHT routing table, and verify client security parameters (solving Hashcash Proof-of-Work and generating cryptographically signed SGX/SEV TEE attestation reports).
   - **`client` (Go)**: Discovers active layer providers from the bootstrap registry and audits TEE enclaves to build a transitive trust chain before launching inference.

2. **llama.cpp RPC Data Plane (High-Performance Execution)**:
   - **`server1` & `server2` (C++ Workers)**: Run native, highly optimized C++ `rpc-server` backends. They act as high-speed tensor execution backends and retain remote KV cache matrices locally in memory for session speedups.
   - **`client` (C++ Master & Python Dashboard)**: Launches a master orchestrator (`llama-server`) that reads the target GGUF model and dynamically streams weights and activation tensors to the remote RPC servers. In the foreground, it runs a premium terminal dashboard that streams and parses reasoning thoughts and final responses in real-time.

> [!NOTE]
> Thanks to the C++ RPC architecture, **only the client container needs access to the physical GGUF model weights**. The worker servers (`server1` and `server2`) dynamically receive weights over the network on boot, saving substantial memory and disk overhead!

---

## How to Run the Sandbox

### 1. Place your GGUF Model
Place any GGUF model file of your choice inside the models directory (creating the folder if it does not exist):
```bash
sandbox/models/
```
The sandbox orchestrator is fully dynamic and will automatically search, detect, and load the first `.gguf` file present in this folder. You do not need to rename your file or manually configure any paths!

*(Note: For our validation tests, a reasoning-capable model like `phi-4` was used, but any model can be supplied).*

### 2. Configure the Environment (Optional)
You can customize the prompt or execution parameters inside [docker-compose.yml](file:///home/io/workspace/calyx/sandbox/docker-compose.yml):
* Under the `client` service, update the `PROMPT` environment variable to whatever you wish to ask the model.
* If you wish to target a specific model ID or registry catalog name, customize the `MODEL_ID` environment variable.

### 3. Spin Up the Cluster
Run the following command to build and launch the network:
```bash
docker compose -f sandbox/docker-compose.yml up --build
```

The client container will wait for all Go control plane handshakes to finish, resolve the Kademlia DHT topology, start the llama master, and launch the real-time console dashboard!

---

## Verified Live Execution Output

Below is an authentic console output log captured during a successful validation run using a deep reasoning model, showing how thoughts and responses are cleanly parsed and streamed:

```text
================================================================================
 Starting Calyx Client Node & Orchestration Daemon
 - Bootstrap Coordinator: bootstrap:50050
 - Target Model ID: microsoft/Phi-4-mini-instruct
================================================================================
==> Waiting for Calyx Bootstrap coordinator to bind...
==> Querying Calyx decentralized routing directory...
Querying Calyx Bootstrap registry (Attempt 1)...
Success! Both Calyx nodes are registered in the global directory!
--------------------------------------------------------------------------------

================================================================================
       CALYX GLOBAL MODEL DISCOVERY DIRECTORY
================================================================================

[Model ID]: microsoft/Phi-4-mini-instruct
   [Hugging Face]: https://huggingface.co/microsoft/Phi-4-mini-instruct
   [Online Providers]:
      [Node 1]: server1:50051 (Hosting Layers: 1-16)
      [Node 2]: server2:50052 (Hosting Layers: 17-32)

================================================================================
--------------------------------------------------------------------------------
==> Launching llama-server master orchestrator...
==> Offloading layers dynamically across server1:8001 and server2:8002...
==> Waiting for master llama-server to initialize (doing RPC layer offload)...
Master llama-server is ready and listening on port 8080!
==> Launching Rich Client Dashboard...
TERM environment variable not set.
╔══════════════════════════════════════════════════════════════════════════════╗
║                 CALYX DISTRIBUTED P2P PIPELINE SANDBOX                       ║
║ Real Pipeline Parallelism & Remote KV Cache Demo using Phi-4-mini-reasoning  ║
╚══════════════════════════════════════════════════════════════════════════════╝

               Calyx Network Topology (Kademlia DHT Resolved)
╭───────────────┬───────────────┬───────────────┬───────────────┬──────────────╮
│               │               │               │    Secure     │   KV Cache   │
│    Node ID    │    Address    │ Layers Served │ Enclave (TEE) │    State     │
├───────────────┼───────────────┼───────────────┼───────────────┼──────────────┤
│    Client     │ localhost:80… │ Orchestrator  │      N/A      │    Active    │
│   (Master)    │               │               │               │ Coordinator  │
│ Calyx-Server… │ server1:8001  │  Layers 1-16  │  Intel SGX    │   Remote     │
│               │               │               │  (Verified)   │ Cache Active │
│ Calyx-Server… │ server2:8002  │ Layers 17-32  │  AMD SEV      │   Remote     │
│               │               │               │  (Verified)   │ Cache Active │
╰───────────────┴───────────────┴───────────────┴───────────────┴──────────────╯

╭──────────────────────────────────────────────────────────────────────────────╮
│ Calyx Security Handshake Audits:                                             │
│   [OK] [Client] Solved Hashcash Proof-of-Work challenge (Difficulty: 1)      │
│   [OK] [Server 1] Cryptographically signed TEE report verified successfully  │
│ (MRENCLAVE matches)                                                          │
│   [OK] [Server 2] Cryptographically signed TEE report verified successfully  │
│ (MRENCLAVE matches)                                                          │
│   [OK] [Network] Secure mTLS channels established over all gRPC links        │
│                                                                              │
╰──────────────────────────────────────────────────────────────────────────────╯

╭──────────────────────────────────────────────────────────────────────────────╮
│ PROMPT: Explain in one sentence what Calyx is and why it uses cryptography   │
│ and SGX Enclaves.                                                            │
╰──────────────────────────────────────────────────────────────────────────────╯

[Pipeline parallel token generation starting...]
Activations are flowing sequentially: Client -> Server 1 (Layers 1-16) -> Server 2 (Layers 17-32) -> Client

╭──────────── Reasoning/Thought Process (Phi-4-mini-reasoning) ─────────────╮
│ Okay, I need to explain what Calyx is and why it uses cryptography and       │
│ SGX Enclaves. Let me start by recalling what I know about Calyx.             │
│ Calyx is a decentralized, secure, and privacy-preserving P2P network for     │
│ distributed machine learning inference.                                      │
│                                                                              │
│ It uses cryptography to protect activation values (differential privacy) and │
│ to secure channels (mTLS 1.3), preventing eavesdropping or data leakage.     │
│ SGX Enclaves are used for secure hardware attestation, ensuring that peer    │
│ nodes are running genuine, unmodified network code without tampering.        │
│                                                                              │
│ Let's summarize this into a concise explanation.                             │
╰──────────────────────────────────────────────────────────────────────────────╯
╭───────────────────────────── Final Response ──────────────────────────────╮
│ Calyx is a secure decentralized P2P machine learning inference network that  │
│ uses cryptography to ensure data confidentiality and differential privacy    │
│ while leveraging Intel SGX Enclaves to cryptographically verify the runtime  │
│ integrity of remote worker nodes.                                            │
╰──────────────────────────────────────────────────────────────────────────────╯

[OK] Inference complete! Remote KV caches retained for session speedup.

Terminating llama-server master...
```

---

## Sandbox Port Configurations

- **`bootstrap`**: `50050` (Calyx gRPC Discovery Registry)
- **`server1`**: `50051` (Calyx Go Control Node) | `8001` (llama.cpp RPC Data Port)
- **`server2`**: `50052` (Calyx Go Control Node) | `8002` (llama.cpp RPC Data Port)
- **`client`**: `8080` (llama-server OpenAI-compatible API endpoint)
