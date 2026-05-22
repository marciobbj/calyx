# Stage 1: Build the Calyx Go binary
FROM golang:1.26 AS builder

WORKDIR /app

# Copy the Go module definitions and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire workspace Go code
COPY . .

# Compile the static Go binary
RUN CGO_ENABLED=0 GOOS=linux go build -o connect main.go

# Stage 2: Clean runtime image
FROM alpine:latest
WORKDIR /app

# Install certificates for secure TLS networking
RUN apk --no-cache add ca-certificates

# Copy the compiled binary
COPY --from=builder /app/connect /app/connect

EXPOSE 50050

ENTRYPOINT ["/app/connect", "-mode=bootstrap", "-addr=0.0.0.0:50050"]
