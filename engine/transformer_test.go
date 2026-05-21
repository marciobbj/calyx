package engine

import (
	"testing"
)

func TestTransformerLayerAttentionForward(t *testing.T) {
	hiddenDim := 4
	layer := NewTransformerLayer(hiddenDim)

	input := []float64{1.0, -1.0, 0.5, 2.0}
	shape := []int64{1, 4} // seq_len = 1, hidden_dim = 4

	var kvCache []float64

	t.Log("Executing first forward token pass...")
	output, err := layer.Forward(input, shape, &kvCache)
	if err != nil {
		t.Fatalf("Transformer Forward failed: %v", err)
	}

	if len(output) != len(input) {
		t.Errorf("Expected output size %d, got %d", len(input), len(output))
	}

	// KV Cache size: since hiddenDim = 4, first token appends K (size 4) and V (size 4) = 8 floats total
	if len(kvCache) != 8 {
		t.Errorf("Expected KV cache size 8, got %d", len(kvCache))
	}

	t.Log("Executing second forward token pass...")
	output2, err := layer.Forward([]float64{2.0, -2.0, 1.0, 4.0}, shape, &kvCache)
	if err != nil {
		t.Fatalf("Second Transformer Forward failed: %v", err)
	}

	if len(output2) != len(input) {
		t.Errorf("Expected output size %d, got %d", len(input), len(output2))
	}

	// Second token appends another K (size 4) and V (size 4) = 16 floats total
	if len(kvCache) != 16 {
		t.Errorf("Expected KV cache size 16, got %d", len(kvCache))
	}

	t.Logf("Completed transformer self-attention computations!")
}
