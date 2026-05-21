package tests

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"calyx/bootstrap"
	"calyx/client"
	"calyx/crypto"
	"calyx/engine"
	"calyx/server"
)

// TestDifferentialPrivacyNoiseBounds verifies that:
// 1. Noise successfully perturbs embedding activations.
// 2. The standard deviation of the injected noise matches mathematical bounds.
// 3. Self-attention calculations remain stable within stable output bounds.
func TestDifferentialPrivacyNoiseBounds(t *testing.T) {
	// Original dummy activation representing a 4-dimensional embedding
	original := []float64{1.5, -0.5, 2.0, 0.8}
	stdDev := 0.1

	// Perturb multiple times to compute empirical variance/stdDev
	iterations := 10000
	var sumDiff, sumSqDiff float64

	for i := 0; i < iterations; i++ {
		perturbed := crypto.AddGaussianNoise(original, stdDev)
		if len(perturbed) != len(original) {
			t.Fatalf("perturbed size mismatch: expected %d, got %d", len(original), len(perturbed))
		}
		
		// Accumulate difference on first dimension to measure variance
		diff := perturbed[0] - original[0]
		sumDiff += diff
		sumSqDiff += diff * diff
	}

	empiricalMean := sumDiff / float64(iterations)
	empiricalVar := (sumSqDiff / float64(iterations)) - (empiricalMean * empiricalMean)
	empiricalStdDev := math.Sqrt(empiricalVar)

	t.Logf("Empirical Mean of Noise: %f (expected ~0.0)", empiricalMean)
	t.Logf("Empirical StdDev of Noise: %f (expected ~%f)", empiricalStdDev, stdDev)

	// Assert within 5% tolerance bounds for 10000 samples
	if math.Abs(empiricalMean) > 0.01 {
		t.Errorf("empirical mean too far from 0: got %f", empiricalMean)
	}
	if math.Abs(empiricalStdDev-stdDev)/stdDev > 0.05 {
		t.Errorf("empirical stddev too far from expected: got %f, expected %f", empiricalStdDev, stdDev)
	}

	// Verify that with noise, the attention forward pass remains stable
	transLayer := engine.NewTransformerLayer(4)
	
	// Create original and perturbed outputs
	originalInput := make([]float64, 40)
	for i := 0; i < 40; i++ {
		originalInput[i] = float64(i) * 0.1
	}

	perturbedInput := crypto.AddGaussianNoise(originalInput, 0.001) // very small DP noise

	var cache []float64
	outOriginal, err := transLayer.Forward(originalInput, []int64{10, 4}, &cache)
	if err != nil {
		t.Fatalf("original forward failed: %v", err)
	}

	var cachePerturbed []float64
	outPerturbed, err := transLayer.Forward(perturbedInput, []int64{10, 4}, &cachePerturbed)
	if err != nil {
		t.Fatalf("perturbed forward failed: %v", err)
	}

	// Assert output layer utility is preserved (mean absolute error is very small)
	var mae float64
	for i := 0; i < len(outOriginal); i++ {
		mae += math.Abs(outOriginal[i] - outPerturbed[i])
	}
	mae /= float64(len(outOriginal))

	t.Logf("Transformer Mean Absolute Error under 0.001 DP Noise: %e", mae)
	if mae > 0.02 {
		t.Errorf("transformer output too unstable under DP noise: MAE = %f", mae)
	}
}

// TestTEEAttestationCryptographicAudit verifies that:
// 1. A genuine server enclave generates a valid attestation report.
// 2. The client correctly verifies the signature and MRENCLAVE measurement.
// 3. An invalid manufacturer signature is rejected.
// 4. An expired attestation report is rejected.
// 5. A mismatched MRENCLAVE measurement is rejected.
func TestTEEAttestationCryptographicAudit(t *testing.T) {
	addr := "localhost:50060"
	measurement := crypto.DefaultMRENCLAVE

	// 1. Generate standard valid attestation report
	report, err := crypto.GenerateAttestationReport(addr, measurement)
	if err != nil {
		t.Fatalf("Failed to generate attestation report: %v", err)
	}

	// Verify standard report succeeds
	err = crypto.VerifyAttestationReport(report, measurement)
	if err != nil {
		t.Errorf("Expected valid TEE report verification to succeed, got error: %v", err)
	}

	// 2. Mismatched MRENCLAVE measurement check
	err = crypto.VerifyAttestationReport(report, "tampered_code_measurement_hash")
	if err == nil {
		t.Error("Expected verification to fail for mismatched MRENCLAVE, but it succeeded")
	} else {
		t.Logf("Successfully caught mismatched MRENCLAVE: %v", err)
	}

	// 3. Expired attestation report check (simulate timestamp in the past > 5 min)
	expiredReport := &crypto.AttestationReport{
		EnclaveAddr: report.EnclaveAddr,
		MRENCLAVE:   report.MRENCLAVE,
		Timestamp:   time.Now().Add(-10 * time.Minute).Unix(),
		Signature:   report.Signature, // uses valid signature of valid fields, but old timestamp
	}
	
	err = crypto.VerifyAttestationReport(expiredReport, measurement)
	if err == nil {
		t.Error("Expected verification to fail for expired report timestamp, but it succeeded")
	} else if err.Error() != "enclave attestation report has expired" {
		t.Errorf("Expected 'enclave attestation report has expired' error, got: %v", err)
	} else {
		t.Logf("Successfully caught expired TEE report: %v", err)
	}

	// 4. Invalid signature check
	tamperedReport := &crypto.AttestationReport{
		EnclaveAddr: report.EnclaveAddr,
		MRENCLAVE:   report.MRENCLAVE,
		Timestamp:   report.Timestamp,
		Signature:   "010203040506:0708090a0b0c", // random fake signature
	}
	err = crypto.VerifyAttestationReport(tamperedReport, measurement)
	if err == nil {
		t.Error("Expected verification to fail for tampered signature, but it succeeded")
	} else {
		t.Logf("Successfully caught invalid signature: %v", err)
	}
}

// TestTEEClusterIntegration verifies that:
// 1. When a client and servers are configured with TEE, they execute the pipeline successfully and establish transitively verified secure enclaves.
// 2. If a client expects TEE attestation but one of the server nodes fails to provide a verified report, it terminates execution immediately.
func TestTEEClusterIntegration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	bootstrapAddr := getFreeTCPPort(t)
	server1Addr := getFreeTCPPort(t)
	server2Addr := getFreeTCPPort(t)

	// 1. Start Bootstrap Server
	bSrv, err := bootstrap.StartBootstrapServer(bootstrapAddr, &wg)
	if err != nil {
		t.Fatalf("Failed to start Bootstrap: %v", err)
	}
	defer bSrv.GracefulStop()
	time.Sleep(50 * time.Millisecond)

	// 2. Start Servers with TEE Enclave enabled (teeEnclave = true)
	sSrv1, err := server.StartServer(ctx, bootstrapAddr, 1, 4, server1Addr, 5*time.Second, 2, nil, &wg, true)
	if err != nil {
		t.Fatalf("Failed to start Server 1: %v", err)
	}
	defer sSrv1.GracefulStop()

	sSrv2, err := server.StartServer(ctx, bootstrapAddr, 5, 8, server2Addr, 5*time.Second, 2, nil, &wg, true)
	if err != nil {
		t.Fatalf("Failed to start Server 2: %v", err)
	}
	defer sSrv2.GracefulStop()
	time.Sleep(100 * time.Millisecond)

	// 3. Run client with TEE attestation required (using crypto.DefaultMRENCLAVE)
	taskID := fmt.Sprintf("tee_integration_success_%d", time.Now().Unix())
	err = client.RunClient(bootstrapAddr, 1, 8, taskID, 2, nil, 0.001, crypto.DefaultMRENCLAVE)
	if err != nil {
		t.Fatalf("Expected secure TEE pipeline to succeed, but got error: %v", err)
	}
	t.Log("[Test] TEE pipeline integration successfully executed and cryptographically verified")

	// 4. Run client expecting a DIFFERENT (invalid) code measurement. This must fail immediately!
	taskIDFail := fmt.Sprintf("tee_integration_fail_mismatch_%d", time.Now().Unix())
	err = client.RunClient(bootstrapAddr, 1, 8, taskIDFail, 2, nil, 0.0, "wrong_mrenclave_code_measurement")
	if err == nil {
		t.Error("Expected client to reject server due to mismatched MRENCLAVE measurement, but it succeeded")
	} else {
		t.Logf("Successfully caught expected MRENCLAVE audit rejection: %v", err)
	}
}
