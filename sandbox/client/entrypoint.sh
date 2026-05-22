#!/bin/bash
set -e

BOOTSTRAP_ADDR=${BOOTSTRAP_ADDR:-"bootstrap:50050"}
MODEL_ID=${MODEL_ID:-"microsoft/Phi-4-mini-instruct"}

echo "================================================================================"
echo " Starting Calyx Client Node & Orchestration Daemon"
echo " - Bootstrap Coordinator: ${BOOTSTRAP_ADDR}"
echo " - Target Model ID: ${MODEL_ID}"
echo "================================================================================"

# 1. Wait for Calyx bootstrap node to be reachable
echo "==> Waiting for Calyx Bootstrap coordinator to bind..."
for i in {1..30}; do
    if curl -s http://bootstrap:50050/health >/dev/null 2>&1 || nc -z bootstrap 50050 >/dev/null 2>&1; then
        echo "Bootstrap is reachable!"
        break
    fi
    sleep 1
done

# 2. Wait for Calyx Servers to register in the network
echo "==> Querying Calyx decentralized routing directory..."
for i in {1..40}; do
    echo "Querying Calyx Bootstrap registry (Attempt $i)..."
    OUTPUT=$(/app/connect -mode=list-models -bootstrap=${BOOTSTRAP_ADDR} 2>/dev/null || true)
    
    if echo "$OUTPUT" | grep -q "server1" && echo "$OUTPUT" | grep -q "server2"; then
        echo "Success! Both Calyx nodes are registered in the global directory!"
        echo "--------------------------------------------------------------------------------"
        echo "$OUTPUT"
        echo "--------------------------------------------------------------------------------"
        break
    fi
    sleep 3
done

# 3. Check if the GGUF model exists on disk before proceeding
# We dynamically check if GGUF_PATH is set; otherwise, we search for any .gguf file in the /models directory.
if [ -z "$GGUF_PATH" ]; then
    FOUND_GGUF=$(find /models -maxdepth 1 -name "*.gguf" | head -n 1)
    if [ -n "$FOUND_GGUF" ]; then
        GGUF_PATH="$FOUND_GGUF"
    else
        # Default fallback
        GGUF_PATH="/models/model.gguf"
    fi
fi

if [ ! -f "$GGUF_PATH" ]; then
    echo "################################################################################"
    echo " ERROR: GGUF Model weights file not found!"
    echo " Expected location: Place any GGUF model file inside sandbox/models/ directory"
    echo " Please download a GGUF model of your choice and place it there."
    echo "################################################################################"
    # We exit gracefully to avoid container looping but leave a clear error in logs
    exit 1
fi

echo "==> Using GGUF model file: $GGUF_PATH"

# 4. Start the llama-server master connecting to the two discovered RPC backends
echo "==> Launching llama-server master orchestrator..."
echo "==> Offloading layers dynamically across server1:8001 and server2:8002..."
/app/llama-server \
    -m "$GGUF_PATH" \
    --host 0.0.0.0 \
    --port 8080 \
    --rpc "server1:8001,server2:8002" \
    -ngl 99 \
    --ctx-size 2048 \
    --threads 4 \
    --log-disable \
    > /app/llama-server.log 2>&1 &

MASTER_PID=$!

cleanup() {
    echo "Terminating llama-server master..."
    kill -TERM "$MASTER_PID" 2>/dev/null || true
}
trap cleanup EXIT

# 5. Wait for the master server to be fully ready
echo "==> Waiting for master llama-server to initialize (doing RPC layer offload)..."
for i in {1..60}; do
    if curl -s http://localhost:8080/health | grep -q '"status":'; then
        echo "Master llama-server is ready and listening on port 8080!"
        break
    fi
    sleep 2
done

# 6. Run the premium python terminal client dashboard!
echo "==> Launching Rich Client Dashboard..."
python3 /app/client.py
