#!/usr/bin/env bash

# Black-box E2E integration test script for Petals P2P Go CLI binary.
# It spawns a bootstrap node, two servers representing layers 1-4 and 5-8,
# and executes a client connection.

set -euo pipefail

# Define ports and workspace directory
WORKSPACE_DIR="$(pwd)"
LOG_DIR="${WORKSPACE_DIR}/test_logs"
mkdir -p "${LOG_DIR}"

BOOTSTRAP_ADDR="localhost:50100"
SERVER1_ADDR="localhost:50101"
SERVER2_ADDR="localhost:50102"

echo "================================================================"
echo "    STARTING E2E CLI BINARY PROCESS INTEGRATION TEST"
echo "================================================================"
echo "Workspace: ${WORKSPACE_DIR}"
echo "Logs Directory: ${LOG_DIR}"

# Compile and check binary
BINARY="./bin/connect"
if [ ! -f "${BINARY}" ]; then
    echo "ERROR: CLI binary not found at ${BINARY}. Run 'make build' first."
    exit 1
fi

# Track spawned process PIDs for safe cleanup
BOOTSTRAP_PID=""
SERVER1_PID=""
SERVER2_PID=""

# Cleanup function to kill background processes on exit (normal or error)
cleanup() {
    echo "----------------------------------------------------------------"
    echo "Cleaning up background CLI processes..."
    
    if [ -n "${SERVER1_PID}" ]; then
        echo "Stopping Server 1 (PID: ${SERVER1_PID})..."
        kill -9 "${SERVER1_PID}" 2>/dev/null || true
    fi
    
    if [ -n "${SERVER2_PID}" ]; then
        echo "Stopping Server 2 (PID: ${SERVER2_PID})..."
        kill -9 "${SERVER2_PID}" 2>/dev/null || true
    fi
    
    if [ -n "${BOOTSTRAP_PID}" ]; then
        echo "Stopping Bootstrap (PID: ${BOOTSTRAP_PID})..."
        kill -9 "${BOOTSTRAP_PID}" 2>/dev/null || true
    fi
    echo "Cleanup complete."
    echo "================================================================"
}

# Register cleanup trap
trap cleanup EXIT

# 1. Start Bootstrap Node
echo "1. Launching Coordinator (Bootstrap Node) on ${BOOTSTRAP_ADDR}..."
"${BINARY}" -mode=bootstrap -addr="${BOOTSTRAP_ADDR}" > "${LOG_DIR}/bootstrap.log" 2>&1 &
BOOTSTRAP_PID=$!
sleep 0.5

# 2. Start Server 1 (Layers 1-4)
echo "2. Launching Server Node 1 (Layers 1-4) on ${SERVER1_ADDR}..."
"${BINARY}" -mode=server -addr="${SERVER1_ADDR}" -bootstrap="${BOOTSTRAP_ADDR}" -start=1 -end=4 -ttl=3s > "${LOG_DIR}/server1.log" 2>&1 &
SERVER1_PID=$!

# 3. Start Server 2 (Layers 5-8)
echo "3. Launching Server Node 2 (Layers 5-8) on ${SERVER2_ADDR}..."
"${BINARY}" -mode=server -addr="${SERVER2_ADDR}" -bootstrap="${BOOTSTRAP_ADDR}" -start=5 -end=8 -ttl=3s > "${LOG_DIR}/server2.log" 2>&1 &
SERVER2_PID=$!

echo "Waiting 1.5 seconds for server node registration to complete..."
sleep 1.5

# Check if background processes are still running
if ! kill -0 "${BOOTSTRAP_PID}" 2>/dev/null; then
    echo "ERROR: Bootstrap process died prematurely. Check ${LOG_DIR}/bootstrap.log"
    cat "${LOG_DIR}/bootstrap.log"
    exit 1
fi
if ! kill -0 "${SERVER1_PID}" 2>/dev/null; then
    echo "ERROR: Server 1 process died prematurely. Check ${LOG_DIR}/server1.log"
    cat "${LOG_DIR}/server1.log"
    exit 1
fi
if ! kill -0 "${SERVER2_PID}" 2>/dev/null; then
    echo "ERROR: Server 2 process died prematurely. Check ${LOG_DIR}/server2.log"
    cat "${LOG_DIR}/server2.log"
    exit 1
fi

# 4. Run Client Node
TASK_ID="e2e_blackbox_$(date +%s)"
echo "4. Running Client Node for layers 1 to 8 (Task ID: ${TASK_ID})..."

# Run client and capture its output/error code
set +e
"${BINARY}" -mode=client -bootstrap="${BOOTSTRAP_ADDR}" -start=1 -end=8 -task="${TASK_ID}" > "${LOG_DIR}/client.log" 2>&1
CLIENT_EXIT_CODE=$?
set -e

# Analyze Client output
if [ ${CLIENT_EXIT_CODE} -eq 0 ]; then
    echo ">>> SUCCESS: Client pipeline run completed with exit code 0."
else
    echo ">>> FAILURE: Client pipeline run failed with exit code ${CLIENT_EXIT_CODE}."
    echo "--- CLIENT LOG ---"
    cat "${LOG_DIR}/client.log"
    echo "------------------"
    exit ${CLIENT_EXIT_CODE}
fi

# 5. Verify the logs to make sure correct data was processed
echo "5. Verifying log traces for KV Cache propagation..."

if grep -q "KV Cache Part 1: Processed Tensor" "${LOG_DIR}/server1.log"; then
    echo "  [OK] Server 1 successfully processed activations."
else
    echo "  [ERROR] Server 1 log does not show KV Cache processing."
    exit 1
fi

if grep -q "KV Cache Part 2: Processed Tensor" "${LOG_DIR}/server2.log"; then
    echo "  [OK] Server 2 successfully processed activations."
else
    echo "  [ERROR] Server 2 log does not show KV Cache processing."
    exit 1
fi

if grep -q "Pipeline Parallelism completed successfully" "${LOG_DIR}/client.log"; then
    echo "  [OK] Client confirmed E2E Pipeline completion."
else
    echo "  [ERROR] Client log does not show pipeline completion success."
    exit 1
fi

echo "All black-box E2E CLI checks PASSED!"
