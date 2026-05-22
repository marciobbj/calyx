# Stage 1: Build the Calyx Go binary
FROM golang:1.26 AS builder-go
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o connect main.go

# Stage 2: Build llama.cpp with RPC support
FROM ubuntu:22.04 AS builder-cpp

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y \
    cmake \
    git \
    g++ \
    make \
    libcurl4-openssl-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
# Clone only the latest commit to speed up download dramatically
RUN git clone --depth 1 https://github.com/ggerganov/llama.cpp.git .

# Enable RPC compilation
RUN cmake -B build -DGGML_RPC=ON -DCMAKE_BUILD_TYPE=Release \
    && cmake --build build --config Release -j$(nproc)

# Stage 3: Minimal, clean execution runtime
FROM ubuntu:22.04
WORKDIR /app

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y \
    libgomp1 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy binaries from the builder stages
COPY --from=builder-go /app/connect /app/connect
COPY --from=builder-cpp /app/build/bin/ /app/

ENV LD_LIBRARY_PATH=/app

# Copy and set permissions on entrypoint script
COPY sandbox/server/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Ports
# 50051/50052: Calyx Go Node gRPC
# 8001/8002: llama.cpp RPC
EXPOSE 50051 8001

ENTRYPOINT ["/app/entrypoint.sh"]
