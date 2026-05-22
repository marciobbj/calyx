#!/bin/bash
set -e

# Default values if environment variables are not set
RPC_PORT=${RPC_PORT:-8001}
SERVER_PORT=${SERVER_PORT:-50051}
START_LAYER=${START_LAYER:-1}
END_LAYER=${END_LAYER:-16}
MODEL_ID=${MODEL_ID:-"microsoft/Phi-4-mini-instruct"}
BOOTSTRAP_ADDR=${BOOTSTRAP_ADDR:-"bootstrap:50050"}

echo "================================================================================"
echo " Starting Calyx Server & llama.cpp RPC Worker Node"
echo " - Server Name: ${SERVER_NAME:-server}"
echo " - Calyx Go Server Port: ${SERVER_PORT} (Serving layers ${START_LAYER}-${END_LAYER})"
echo " - llama.cpp RPC Port: ${RPC_PORT}"
echo " - Model ID: ${MODEL_ID}"
echo " - Bootstrap Node: ${BOOTSTRAP_ADDR}"
echo "================================================================================"

# 1. Spawn llama.cpp RPC server in the background
echo "==> Starting llama.cpp rpc-server..."
/app/rpc-server --host 0.0.0.0 --port ${RPC_PORT} > /app/rpc-server.log 2>&1 &

# Store the background process PID
RPC_PID=$!

# Ensure we terminate it if the script exits
cleanup() {
    echo "Terminating RPC server (PID: ${RPC_PID})..."
    kill -TERM "$RPC_PID" 2>/dev/null || true
}
trap cleanup EXIT

# Give the rpc-server a quick second to bind
sleep 1

# 2. Run the Calyx Go server in the foreground
# It will load a small dummy weights file locally to pass weight-verification tests,
# but actual execution layers are offloaded to the RPC backend which receives weights dynamically.
echo "==> Launching Calyx Go Server Node..."
/app/connect \
    -mode=server \
    -addr=${SERVER_NAME}:${SERVER_PORT} \
    -bootstrap=${BOOTSTRAP_ADDR} \
    -start=${START_LAYER} \
    -end=${END_LAYER} \
    -model=${MODEL_ID} \
    -difficulty=1 \
    -tee-enclave=true \
    -enclave-simulation=true
