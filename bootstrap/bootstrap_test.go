package bootstrap

import (
	"context"
	"testing"

	pb "connect/proto"
)

func TestRegisterNode(t *testing.T) {
	server := NewServer()

	// 1. Test registering a valid node
	req1 := &pb.RegisterRequest{
		Address:    "localhost:50051",
		StartLayer: 1,
		EndLayer:   4,
	}
	resp1, err := server.RegisterNode(context.Background(), req1)
	if err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}
	if !resp1.Success {
		t.Errorf("Registration failed: %s", resp1.Message)
	}

	// Verify node exists
	if len(server.nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(server.nodes))
	}
	if server.nodes[0].Address != "localhost:50051" || server.nodes[0].StartLayer != 1 || server.nodes[0].EndLayer != 4 {
		t.Errorf("Registered node data mismatch: %+v", server.nodes[0])
	}

	// 2. Test updating registration for same address
	req2 := &pb.RegisterRequest{
		Address:    "localhost:50051",
		StartLayer: 1,
		EndLayer:   6,
	}
	resp2, err := server.RegisterNode(context.Background(), req2)
	if err != nil {
		t.Fatalf("Failed to update node registration: %v", err)
	}
	if !resp2.Success {
		t.Errorf("Update failed: %s", resp2.Message)
	}
	if len(server.nodes) != 1 {
		t.Errorf("Expected 1 node after update, got %d", len(server.nodes))
	}
	if server.nodes[0].EndLayer != 6 {
		t.Errorf("Expected end layer to be updated to 6, got %d", server.nodes[0].EndLayer)
	}

	// 3. Test invalid range
	req3 := &pb.RegisterRequest{
		Address:    "localhost:50052",
		StartLayer: 10,
		EndLayer:   5,
	}
	resp3, err := server.RegisterNode(context.Background(), req3)
	if err != nil {
		t.Fatalf("Expected no error from register call, got: %v", err)
	}
	if resp3.Success {
		t.Errorf("Expected registration to fail due to start > end layer")
	}

	// 4. Test empty address
	req4 := &pb.RegisterRequest{
		Address:    "",
		StartLayer: 1,
		EndLayer:   5,
	}
	resp4, err := server.RegisterNode(context.Background(), req4)
	if err != nil {
		t.Fatalf("Expected no error from register call, got: %v", err)
	}
	if resp4.Success {
		t.Errorf("Expected registration to fail due to empty address")
	}
}

func TestFindRoute(t *testing.T) {
	server := NewServer()

	// Register test nodes
	nodesToRegister := []*pb.RegisterRequest{
		{Address: "node1", StartLayer: 1, EndLayer: 4},
		{Address: "node2", StartLayer: 5, EndLayer: 8},
		{Address: "node3", StartLayer: 9, EndLayer: 12},
	}

	for _, node := range nodesToRegister {
		_, err := server.RegisterNode(context.Background(), node)
		if err != nil {
			t.Fatalf("Failed to pre-register test nodes: %v", err)
		}
	}

	// 1. Test finding a perfect continuous route
	routeReq1 := &pb.RouteRequest{StartLayer: 1, EndLayer: 8}
	routeResp1, err := server.FindRoute(context.Background(), routeReq1)
	if err != nil {
		t.Fatalf("Failed to find route: %v", err)
	}
	expectedRoute1 := []string{"node1", "node2"}
	if len(routeResp1.Addresses) != len(expectedRoute1) {
		t.Fatalf("Expected route length %d, got %d", len(expectedRoute1), len(routeResp1.Addresses))
	}
	for i, addr := range routeResp1.Addresses {
		if addr != expectedRoute1[i] {
			t.Errorf("Expected route index %d to be %s, got %s", i, expectedRoute1[i], addr)
		}
	}

	// 2. Test sub-route query
	routeReq2 := &pb.RouteRequest{StartLayer: 5, EndLayer: 12}
	routeResp2, err := server.FindRoute(context.Background(), routeReq2)
	if err != nil {
		t.Fatalf("Failed to find sub-route: %v", err)
	}
	expectedRoute2 := []string{"node2", "node3"}
	if len(routeResp2.Addresses) != len(expectedRoute2) {
		t.Fatalf("Expected route length %d, got %d", len(expectedRoute2), len(routeResp2.Addresses))
	}

	// 3. Test broken route due to missing layers (gap)
	// Clear nodes and register gap: 1-4 and 6-8 (missing layer 5)
	server.nodes = []NodeInfo{}
	server.RegisterNode(context.Background(), &pb.RegisterRequest{Address: "node1", StartLayer: 1, EndLayer: 4})
	server.RegisterNode(context.Background(), &pb.RegisterRequest{Address: "node2", StartLayer: 6, EndLayer: 8})

	routeReq3 := &pb.RouteRequest{StartLayer: 1, EndLayer: 8}
	_, err = server.FindRoute(context.Background(), routeReq3)
	if err == nil {
		t.Errorf("Expected route finding to fail due to layer coverage gap, but it succeeded")
	}
}
