package engine

import (
	"errors"
	"math"
)

// TransformerLayer represents a mathematically complete Self-Attention and FFN layer block
type TransformerLayer struct {
	HiddenDim int
	Wq        []float64 // [HiddenDim, HiddenDim]
	Wk        []float64 // [HiddenDim, HiddenDim]
	Wv        []float64 // [HiddenDim, HiddenDim]
	Wo        []float64 // [HiddenDim, HiddenDim]
	Wmlp1     []float64 // [HiddenDim, HiddenDim*2]
	Wmlp2     []float64 // [HiddenDim*2, HiddenDim]
}

// NewTransformerLayer initializes stable identity-like model weights to prevent scaling explosions
func NewTransformerLayer(hiddenDim int) *TransformerLayer {
	layer := &TransformerLayer{
		HiddenDim: hiddenDim,
		Wq:        make([]float64, hiddenDim*hiddenDim),
		Wk:        make([]float64, hiddenDim*hiddenDim),
		Wv:        make([]float64, hiddenDim*hiddenDim),
		Wo:        make([]float64, hiddenDim*hiddenDim),
		Wmlp1:     make([]float64, hiddenDim*hiddenDim*2),
		Wmlp2:     make([]float64, hiddenDim*2*hiddenDim),
	}

	// Initialize projection matrices with identity values so output maps closely to input in initial states
	for i := 0; i < hiddenDim; i++ {
		layer.Wq[i*hiddenDim+i] = 1.0
		layer.Wk[i*hiddenDim+i] = 1.0
		layer.Wv[i*hiddenDim+i] = 1.0
		layer.Wo[i*hiddenDim+i] = 1.0
	}

	// Initialize MLP weights with stable scaling coefficients
	for i := 0; i < hiddenDim; i++ {
		layer.Wmlp1[i*(hiddenDim*2)+i] = 0.5
		layer.Wmlp2[i*hiddenDim+(i%hiddenDim)] = 0.5
	}

	return layer
}

// Forward computes Self-Attention, updates the KV Cache, and executes MLP layer calculations
// input: sequence hidden states of shape [seq_len, HiddenDim] (currently flattened)
// kvCache: pointer to accumulated float slice in the server context
func (tl *TransformerLayer) Forward(input []float64, shape []int64, kvCache *[]float64) ([]float64, error) {
	if len(shape) < 2 {
		return nil, errors.New("transformer forward: shape must have at least 2 dimensions [seq_len, hidden_dim]")
	}
	seqLen := int(shape[0])
	hiddenDim := int(shape[1])

	if hiddenDim != tl.HiddenDim {
		return nil, errors.New("transformer forward: input hidden_dim mismatches model layer setup")
	}
	if len(input) != seqLen*hiddenDim {
		return nil, errors.New("transformer forward: input data size mismatch shape dimensions")
	}

	output := make([]float64, len(input))

	for t := 0; t < seqLen; t++ {
		x := input[t*hiddenDim : (t+1)*hiddenDim]

		// 1. Q, K, V linear projections
		q := tl.matMulVec(x, tl.Wq)
		k := tl.matMulVec(x, tl.Wk)
		v := tl.matMulVec(x, tl.Wv)

		// 2. Append keys and values to dynamic KV Cache
		// Structure of cache: keys array followed by values array, or interleaved
		// We'll append keys to kvCache and match them during attention step
		*kvCache = append(*kvCache, k...)
		*kvCache = append(*kvCache, v...)

		numCachedTokens := len(*kvCache) / (hiddenDim * 2)

		// 3. Compute Attention scores against all accumulated tokens
		scores := make([]float64, numCachedTokens)
		maxScore := -math.MaxFloat64

		for i := 0; i < numCachedTokens; i++ {
			// Extract key vector at cached index i
			kCached := (*kvCache)[i*(hiddenDim*2) : i*(hiddenDim*2)+hiddenDim]

			// Dot product
			var dot float64
			for j := 0; j < hiddenDim; j++ {
				dot += q[j] * kCached[j]
			}
			// Scale by 1/sqrt(d_k)
			scores[i] = dot / math.Sqrt(float64(hiddenDim))
			if scores[i] > maxScore {
				maxScore = scores[i]
			}
		}

		// Softmax exponentiation
		sumExp := 0.0
		for i := 0; i < numCachedTokens; i++ {
			scores[i] = math.Exp(scores[i] - maxScore) // numeric stability offset
			sumExp += scores[i]
		}

		// Self-Attention Value aggregation
		attnOut := make([]float64, hiddenDim)
		for i := 0; i < numCachedTokens; i++ {
			weight := scores[i] / sumExp
			vCached := (*kvCache)[i*(hiddenDim*2)+hiddenDim : hiddenCacheVal(i, hiddenDim)]
			for j := 0; j < hiddenDim; j++ {
				attnOut[j] += weight * vCached[j]
			}
		}

		// 4. Output projection
		y := tl.matMulVec(attnOut, tl.Wo)

		// 5. First Residual connection + LayerNorm
		for j := 0; j < hiddenDim; j++ {
			y[j] = y[j] + x[j] // residual
		}
		yNorm := tl.layerNorm(y)

		// 6. MLP Feed-Forward (MLP1 -> ReLU -> MLP2)
		mlp1 := make([]float64, hiddenDim*2)
		for r := 0; r < hiddenDim*2; r++ {
			var sum float64
			for c := 0; c < hiddenDim; c++ {
				sum += yNorm[c] * tl.Wmlp1[c*(hiddenDim*2)+r]
			}
			// ReLU activation
			if sum > 0 {
				mlp1[r] = sum
			}
		}

		mlp2 := make([]float64, hiddenDim)
		for r := 0; r < hiddenDim; r++ {
			var sum float64
			for c := 0; c < hiddenDim*2; c++ {
				sum += mlp1[c] * tl.Wmlp2[c*hiddenDim+r]
			}
			mlp2[r] = sum
		}

		// 7. Second Residual connection + LayerNorm
		finalOutput := make([]float64, hiddenDim)
		for j := 0; j < hiddenDim; j++ {
			finalOutput[j] = yNorm[j] + mlp2[j]
		}
		finalNorm := tl.layerNorm(finalOutput)

		copy(output[t*hiddenDim:(t+1)*hiddenDim], finalNorm)
	}

	return output, nil
}

// helper to locate cached value slice
func hiddenCacheVal(i int, hiddenDim int) int {
	return i*(hiddenDim*2) + hiddenDim*2
}

// matMulVec multiplies a vector by a square matrix [dim x dim]
func (tl *TransformerLayer) matMulVec(vec []float64, mat []float64) []float64 {
	out := make([]float64, tl.HiddenDim)
	for r := 0; r < tl.HiddenDim; r++ {
		var sum float64
		for c := 0; c < tl.HiddenDim; c++ {
			sum += vec[c] * mat[c*tl.HiddenDim+r]
		}
		out[r] = sum
	}
	return out
}

// layerNorm computes standard Layer Normalization over a vector slice
func (tl *TransformerLayer) layerNorm(vec []float64) []float64 {
	out := make([]float64, len(vec))
	var sum float64
	for _, val := range vec {
		sum += val
	}
	mean := sum / float64(len(vec))

	var varSum float64
	for _, val := range vec {
		diff := val - mean
		varSum += diff * diff
	}
	variance := varSum / float64(len(vec))

	epsilon := 1e-5
	stdDev := math.Sqrt(variance + epsilon)

	for i, val := range vec {
		out[i] = (val - mean) / stdDev
	}
	return out
}
