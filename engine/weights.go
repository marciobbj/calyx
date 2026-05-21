package engine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Binary Weight Format constants
const (
	weightsMagic   = "CALYXW" // 6 magic bytes
	weightsVersion = uint16(1)
)

// SaveWeights serializes a TransformerLayer's weights into a custom binary file
func SaveWeights(filePath string, layer *TransformerLayer) error {
	if layer == nil {
		return errors.New("cannot save nil transformer layer weights")
	}

	// Create directory structure if needed
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create weights directory: %w", err)
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create weights file: %w", err)
	}
	defer file.Close()

	// Write magic bytes
	if _, err := file.Write([]byte(weightsMagic)); err != nil {
		return fmt.Errorf("failed to write magic bytes: %w", err)
	}

	// Write version
	if err := binary.Write(file, binary.BigEndian, weightsVersion); err != nil {
		return fmt.Errorf("failed to write version: %w", err)
	}

	// Write HiddenDim
	hiddenDim := uint32(layer.HiddenDim)
	if err := binary.Write(file, binary.BigEndian, hiddenDim); err != nil {
		return fmt.Errorf("failed to write hidden dimension: %w", err)
	}

	// Helper to write float64 slices
	writeSlice := func(name string, slice []float64) error {
		if err := binary.Write(file, binary.BigEndian, slice); err != nil {
			return fmt.Errorf("failed to write weight slice %s: %w", name, err)
		}
		return nil
	}

	// Write weight matrices sequentially
	if err := writeSlice("Wq", layer.Wq); err != nil {
		return err
	}
	if err := writeSlice("Wk", layer.Wk); err != nil {
		return err
	}
	if err := writeSlice("Wv", layer.Wv); err != nil {
		return err
	}
	if err := writeSlice("Wo", layer.Wo); err != nil {
		return err
	}
	if err := writeSlice("Wmlp1", layer.Wmlp1); err != nil {
		return err
	}
	if err := writeSlice("Wmlp2", layer.Wmlp2); err != nil {
		return err
	}

	return nil
}

// LoadWeights deserializes a custom binary weights file and returns a populated TransformerLayer
func LoadWeights(filePath string) (*TransformerLayer, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open weights file: %w", err)
	}
	defer file.Close()

	// Verify magic bytes
	magic := make([]byte, 6)
	if _, err := io.ReadFull(file, magic); err != nil {
		return nil, fmt.Errorf("failed to read magic bytes: %w", err)
	}
	if string(magic) != weightsMagic {
		return nil, fmt.Errorf("invalid weights file: magic mismatch (expected '%s', got '%s')", weightsMagic, string(magic))
	}

	// Verify version
	var version uint16
	if err := binary.Read(file, binary.BigEndian, &version); err != nil {
		return nil, fmt.Errorf("failed to read version: %w", err)
	}
	if version != weightsVersion {
		return nil, fmt.Errorf("unsupported weights file version: %d", version)
	}

	// Read HiddenDim
	var hiddenDim uint32
	if err := binary.Read(file, binary.BigEndian, &hiddenDim); err != nil {
		return nil, fmt.Errorf("failed to read hidden dimension: %w", err)
	}
	dim := int(hiddenDim)

	// Create new layer shell
	layer := &TransformerLayer{
		HiddenDim: dim,
		Wq:        make([]float64, dim*dim),
		Wk:        make([]float64, dim*dim),
		Wv:        make([]float64, dim*dim),
		Wo:        make([]float64, dim*dim),
		Wmlp1:     make([]float64, dim*dim*2),
		Wmlp2:     make([]float64, dim*2*dim),
	}

	// Helper to read float64 slices
	readSlice := func(name string, slice []float64) error {
		if err := binary.Read(file, binary.BigEndian, slice); err != nil {
			return fmt.Errorf("failed to read weight slice %s: %w", name, err)
		}
		return nil
	}

	// Read weight matrices sequentially
	if err := readSlice("Wq", layer.Wq); err != nil {
		return nil, err
	}
	if err := readSlice("Wk", layer.Wk); err != nil {
		return nil, err
	}
	if err := readSlice("Wv", layer.Wv); err != nil {
		return nil, err
	}
	if err := readSlice("Wo", layer.Wo); err != nil {
		return nil, err
	}
	if err := readSlice("Wmlp1", layer.Wmlp1); err != nil {
		return nil, err
	}
	if err := readSlice("Wmlp2", layer.Wmlp2); err != nil {
		return nil, err
	}

	return layer, nil
}

// EnsureWeightsExist checks if the weights file is present.
// If missing, it generates a stable set of identity weights and saves them to filePath.
func EnsureWeightsExist(filePath string, hiddenDim int) error {
	_, err := os.Stat(filePath)
	if err == nil {
		// File already exists
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}

	// File is missing, generate and save stable default weights
	layer := NewTransformerLayer(hiddenDim)
	if err := SaveWeights(filePath, layer); err != nil {
		return fmt.Errorf("failed to generate stable weights file: %w", err)
	}

	return nil
}
