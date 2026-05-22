# Stage 1: Build the Calyx Go binary
FROM golang:1.26 AS builder-go
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o connect main.go

# Stage 2: Build llama.cpp with RPC support to get llama-server and llama-cli
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
RUN git clone --depth 1 https://github.com/ggerganov/llama.cpp.git .

RUN cmake -B build -DGGML_RPC=ON -DCMAKE_BUILD_TYPE=Release \
    && cmake --build build --config Release -j$(nproc)

# Stage 3: Python runtime with compiled Go & C++ binaries
FROM ubuntu:22.04
WORKDIR /app

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y \
    python3 \
    python3-pip \
    libgomp1 \
    curl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy binaries from the builder stages
COPY --from=builder-go /app/connect /app/connect
COPY --from=builder-cpp /app/build/bin/ /app/

ENV LD_LIBRARY_PATH=/app

# Install python dependencies for a beautiful premium client dashboard
RUN pip3 install --no-cache-dir requests rich

# Copy python client and entrypoint scripts
COPY sandbox/client/client.py /app/client.py
COPY sandbox/client/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["/app/entrypoint.sh"]
