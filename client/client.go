package client

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	pb "connect/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// RunClient runs the Client node scenario, querying the bootstrap and processing the pipeline
func RunClient(bootstrapAddr string, startLayer, endLayer int32, taskID string) error {
	log.Printf("[Client] Initializing client node (weak machine)...")

	// 1. Connect to Bootstrap Node
	bootstrapConn, err := grpc.NewClient(bootstrapAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to bootstrap: %w", err)
	}
	defer bootstrapConn.Close()

	bootstrapClient := pb.NewBootstrapServiceClient(bootstrapConn)

	// 2. Discover Route
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("[Client] Querying Bootstrap for route to process Layers %d-%d...", startLayer, endLayer)
	routeResp, err := bootstrapClient.FindRoute(ctx, &pb.RouteRequest{
		StartLayer: startLayer,
		EndLayer:   endLayer,
	})
	if err != nil {
		return fmt.Errorf("failed to discover route: %w", err)
	}

	route := routeResp.Addresses
	if len(route) == 0 {
		return fmt.Errorf("discovered route is empty")
	}

	log.Printf("[Client] Processing route planned by Bootstrap: [Client] -> %s -> [Client]", formatRouteChain(route))

	// 3. Connect to the first server node in the route chain
	firstNodeAddr := route[0]
	log.Printf("[Client] Opening connection to the first node of the pipeline: %s", firstNodeAddr)
	serverConn, err := grpc.NewClient(firstNodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to server %s: %w", firstNodeAddr, err)
	}
	defer serverConn.Close()

	p2pClient := pb.NewP2PServiceClient(serverConn)
	p2pStream, err := p2pClient.Forward(context.Background())
	if err != nil {
		return fmt.Errorf("failed to open P2P forward stream: %w", err)
	}

	// Channel to signal when all responses are received
	doneChan := make(chan struct{})
	var streamErr error

	// 4. Start receiving thread
	go func() {
		defer close(doneChan)
		for {
			resp, err := p2pStream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				streamErr = fmt.Errorf("error reading from server stream: %w", err)
				return
			}

			log.Printf("[Client (Success)] <- Received processed response: Tensor ID: %s, Values (first 4): %.4f",
				resp.Tensor.Id, resp.Tensor.Data[:4])
		}
	}()

	// 5. Send sequence of activations (simulating Embedding step of a long context)
	// We send 3 activation slices sequentially to showcase KV cache growth on the remote servers
	log.Printf("[Client] Simulating Embedding of dummy long context (3 successive tokens)...")
	for t := 1; t <= 3; t++ {
		// Mock token embeddings representation (each token is a 4-dimensional vector)
		data := []float64{float64(t) * 1.5, float64(t) * -0.5, float64(t) * 2.0, float64(t) * 0.8}
		
		tensor := &pb.Tensor{
			Id:     fmt.Sprintf("token_%d", t),
			TaskId: taskID,
			Data:   data,
			Shape:  []int64{1, 4},
		}

		req := &pb.ForwardRequest{
			Tensor:            tensor,
			Route:             route,
			CurrentRouteIndex: 0, // Starts at index 0 of route
		}

		log.Printf("[Client] -> Dispatching Tensor '%s' [Values: %.1f] to the pipeline...", tensor.Id, data)
		if err := p2pStream.Send(req); err != nil {
			return fmt.Errorf("failed to send token %d to stream: %w", t, err)
		}

		// Brief sleep between tokens to simulate time-spaced token generation and let log sequencing print nicely
		time.Sleep(800 * time.Millisecond)
	}

	// 6. Gracefully close client sending stream and wait for all pipeline outputs
	p2pStream.CloseSend()
	<-doneChan

	if streamErr != nil {
		return streamErr
	}

	log.Printf("[Client] Pipeline Parallelism completed successfully for task %s!", taskID)
	return nil
}

func formatRouteChain(route []string) string {
	res := ""
	for i, addr := range route {
		if i > 0 {
			res += " -> "
		}
		res += fmt.Sprintf("[%s]", addr)
	}
	return res
}
