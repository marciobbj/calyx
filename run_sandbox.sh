#!/bin/bash
set -e

# Calyx Hybrid Distributed Sandbox Runner
# This script ensures a GGUF model is present and spins up the containerized P2P network.

MODELS_DIR="sandbox/models"
DEFAULT_MODEL_URL="https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/resolve/main/qwen2.5-0.5b-instruct-q4_k_m.gguf"
DEFAULT_MODEL_NAME="qwen2.5-0.5b-instruct-q4_k_m.gguf"

echo "================================================================================"
echo "                   Calyx Hybrid Distributed Sandbox Runner"
echo "================================================================================"

# 1. Create models directory if it doesn't exist
if [ ! -d "$MODELS_DIR" ]; then
    echo "==> Creating models directory under $MODELS_DIR..."
    mkdir -p "$MODELS_DIR"
fi

# 2. Check for existing GGUF models
GGUF_COUNT=$(find "$MODELS_DIR" -maxdepth 1 -name "*.gguf" | wc -l)

if [ "$GGUF_COUNT" -eq 0 ]; then
    echo "==> No GGUF model files found in $MODELS_DIR/"
    echo "To run the sandbox, a GGUF model is required."
    echo ""
    read -p "Would you like to automatically download a lightweight 0.5B model (~398MB) to test the sandbox? (y/N): " -r RESPONSE
    if [[ "$RESPONSE" =~ ^([yY][eE][sS]|[yY])$ ]]; then
        echo "==> Downloading model from Hugging Face..."
        if command -v wget >/dev/null 2>&1; then
            wget -O "$MODELS_DIR/$DEFAULT_MODEL_NAME" "$DEFAULT_MODEL_URL"
        elif command -v curl >/dev/null 2>&1; then
            curl -L -o "$MODELS_DIR/$DEFAULT_MODEL_NAME" "$DEFAULT_MODEL_URL"
        else
            echo "ERROR: Neither wget nor curl found. Please download a GGUF model manually"
            echo "and place it under '$MODELS_DIR/'."
            exit 1
        fi
        echo "==> Model successfully downloaded!"
    else
        echo "Please place any compatible GGUF model file in '$MODELS_DIR/' and re-run this script."
        exit 0
    fi
else
    FOUND_MODEL=$(find "$MODELS_DIR" -maxdepth 1 -name "*.gguf" | head -n 1)
    echo "==> Found existing GGUF model: $FOUND_MODEL"
fi

# 3. Spin up the docker-compose cluster
echo ""
echo "==> Launching Calyx Hybrid Sandbox via Docker Compose..."
echo "Press Ctrl+C to terminate the cluster when finished."
echo "--------------------------------------------------------------------------------"
docker compose -f sandbox/docker-compose.yml up --build
