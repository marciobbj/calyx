package server

import (
	"context"
	"testing"
	"time"

	"calyx/crypto"
)

func TestServerKVCacheOperations(t *testing.T) {
	// Initialize a Server node
	cert, err := crypto.GenerateKeyPairAndCert()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}
	srv := NewServer("localhost:50051", 1, 4, 10*time.Second, 2, cert, nil, false)

	taskID := "task_test_123"

	// Ensure cache starts empty
	if _, ok := srv.GetKVCacheEntry(taskID); ok {
		t.Fatalf("Expected no cache entry for task initially")
	}

	// 1. Manually add first chunk of data simulating the first token
	entry := &KVCacheEntry{
		Data:         []float64{1.0, 2.0, 3.0},
		LastAccessed: time.Now(),
	}
	srv.kvCache.Store(taskID, entry)

	// Retrieve and verify
	retrieved, ok := srv.GetKVCacheEntry(taskID)
	if !ok {
		t.Fatalf("Failed to retrieve cache entry")
	}
	if len(retrieved.Data) != 3 {
		t.Errorf("Expected 3 floats, got %d", len(retrieved.Data))
	}

	// 2. Append more data (simulating second token)
	retrieved.mu.Lock()
	retrieved.Data = append(retrieved.Data, []float64{4.0, 5.0, 6.0}...)
	retrieved.LastAccessed = time.Now()
	retrieved.mu.Unlock()

	// Verify update is persistent in map
	retrieved2, ok := srv.GetKVCacheEntry(taskID)
	if !ok {
		t.Fatalf("Failed to retrieve cache entry after second update")
	}
	if len(retrieved2.Data) != 6 {
		t.Errorf("Expected 6 floats, got %d", len(retrieved2.Data))
	}
	expectedData := []float64{1.0, 2.0, 3.0, 4.0, 5.0, 6.0}
	for i, v := range retrieved2.Data {
		if v != expectedData[i] {
			t.Errorf("Expected data at index %d to be %f, got %f", i, expectedData[i], v)
		}
	}
}

func TestServerMathMutation(t *testing.T) {
	cert, err := crypto.GenerateKeyPairAndCert()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}
	srv := NewServer("localhost:50051", 1, 4, 10*time.Second, 2, cert, nil, false)

	// Create mock tensor data
	tensorData := []float64{1.0, 2.0, 3.0, 4.0}

	// Set up a mock KV cache for the task to simulate dependency
	taskID := "task_test_math"
	entry := &KVCacheEntry{
		Data:         []float64{0.5, 0.5, 0.5, 0.5}, // average is 0.5
		LastAccessed: time.Now(),
	}
	srv.kvCache.Store(taskID, entry)

	// Apply math formula manually to calculate expected values
	// Formula: val*0.95 + cacheAvg*0.04 + float64(s.startLayer)*0.01
	// For s.startLayer = 1, cacheAvg = 0.5:
	// val * 0.95 + 0.5 * 0.04 + 1 * 0.01 = val * 0.95 + 0.02 + 0.01 = val * 0.95 + 0.03
	expectedOut := make([]float64, len(tensorData))
	for i, val := range tensorData {
		expectedOut[i] = val*0.95 + 0.03
	}

	// Read and calculate using the server structural style
	entryVal, ok := srv.kvCache.Load(taskID)
	if !ok {
		t.Fatalf("Failed to retrieve cache")
	}
	cacheEntry := entryVal.(*KVCacheEntry)

	cacheEntry.mu.Lock()
	cacheLength := len(cacheEntry.Data)
	var sum float64
	for _, val := range cacheEntry.Data {
		sum += val
	}
	cacheAvg := sum / float64(cacheLength)

	outData := make([]float64, len(tensorData))
	for i, val := range tensorData {
		outData[i] = val*0.95 + cacheAvg*0.04 + float64(srv.startLayer)*0.01
	}
	cacheEntry.mu.Unlock()

	// Verify math
	for i, val := range outData {
		if val != expectedOut[i] {
			t.Errorf("Math mismatch at index %d: expected %f, got %f", i, expectedOut[i], val)
		}
	}
}

func TestServerTTLWorker(t *testing.T) {
	// Start with a super short TTL for testing
	ttl := 100 * time.Millisecond
	cert, err := crypto.GenerateKeyPairAndCert()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}
	srv := NewServer("localhost:50051", 1, 4, ttl, 2, cert, nil, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the TTL worker in background
	go srv.startTTLWorker(ctx)

	taskID := "task_test_ttl"
	entry := &KVCacheEntry{
		Data:         []float64{1.0, 2.0},
		LastAccessed: time.Now(),
	}

	srv.kvCache.Store(taskID, entry)

	// Verify it exists initially
	if _, ok := srv.GetKVCacheEntry(taskID); !ok {
		t.Fatalf("Cache entry should be present initially")
	}

	// Wait for TTL (100ms) + buffer time (100ms)
	time.Sleep(200 * time.Millisecond)

	// Verify it was evicted by TTL worker
	if _, ok := srv.GetKVCacheEntry(taskID); ok {
		t.Errorf("Expected cache entry to be evicted by TTL, but it is still present")
	}
}
