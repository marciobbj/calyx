package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// DefaultMRENCLAVE is the authorized code measurement hash for the secure Calyx enclave.
const DefaultMRENCLAVE = "5a556d3570696c65645f43616c79785f456e636c6176655f436f64655f48617368"

// AttestationReport represents a simulated hardware enclave attestation report
type AttestationReport struct {
	EnclaveAddr string `json:"enclave_addr"`
	MRENCLAVE   string `json:"mr_enclave"`
	Timestamp   int64  `json:"timestamp"`
	Signature   string `json:"signature"`
}

var (
	// Simulated Manufacturer Root Key
	manufacturerPrivKey *ecdsa.PrivateKey
	ManufacturerPubKey  *ecdsa.PublicKey
)

func init() {
	// Parse hardcoded P-256 keys to ensure they are identical across all client/server processes.
	dBytes, _ := hex.DecodeString("04335d86122683beede628003e507d875482f2773a42418d6d975cb032c5c190")
	xBytes, _ := hex.DecodeString("4ed6599d89d07092d985066a9377adf71dfb5d20ed766072fa596d20513a82bc")
	yBytes, _ := hex.DecodeString("5987af766c21b5ce9347eebcbf11bf4dc757d970143277aa28f7f7560cb05293")

	pub := ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}

	manufacturerPrivKey = &ecdsa.PrivateKey{
		PublicKey: pub,
		D:         new(big.Int).SetBytes(dBytes),
	}
	ManufacturerPubKey = &pub
}

// GenerateAttestationReport produces a signed hardware attestation report for a node
func GenerateAttestationReport(enclaveAddr, measurement string) (*AttestationReport, error) {
	report := &AttestationReport{
		EnclaveAddr: enclaveAddr,
		MRENCLAVE:   measurement,
		Timestamp:   time.Now().Unix(),
	}

	// Serialize report payload (everything except signature)
	payload, err := json.Marshal(struct {
		EnclaveAddr string `json:"enclave_addr"`
		MRENCLAVE   string `json:"mr_enclave"`
		Timestamp   int64  `json:"timestamp"`
	}{
		EnclaveAddr: report.EnclaveAddr,
		MRENCLAVE:   report.MRENCLAVE,
		Timestamp:   report.Timestamp,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal attestation payload: %w", err)
	}

	hash := sha256.Sum256(payload)
	r, s, err := ecdsa.Sign(rand.Reader, manufacturerPrivKey, hash[:])
	if err != nil {
		return nil, fmt.Errorf("failed to sign attestation: %w", err)
	}

	// Hex encode signature: r.String() + ":" + s.String()
	report.Signature = hex.EncodeToString(r.Bytes()) + ":" + hex.EncodeToString(s.Bytes())
	return report, nil
}

// VerifyAttestationReport verifies the cryptographic authenticity of the report
func VerifyAttestationReport(report *AttestationReport, expectedMeasurement string) error {
	if report == nil {
		return errors.New("missing attestation report")
	}

	// 1. Audit code measurement (MRENCLAVE) to prevent running tampered binaries
	if report.MRENCLAVE != expectedMeasurement {
		return fmt.Errorf("enclave code measurement mismatch: expected '%s', got '%s'", expectedMeasurement, report.MRENCLAVE)
	}

	// 2. Validate timestamp freshness (prevent replay attacks, e.g., within 5 minutes)
	if time.Since(time.Unix(report.Timestamp, 0)) > 5*time.Minute {
		return errors.New("enclave attestation report has expired")
	}

	// 3. Verify signature
	payload, err := json.Marshal(struct {
		EnclaveAddr string `json:"enclave_addr"`
		MRENCLAVE   string `json:"mr_enclave"`
		Timestamp   int64  `json:"timestamp"`
	}{
		EnclaveAddr: report.EnclaveAddr,
		MRENCLAVE:   report.MRENCLAVE,
		Timestamp:   report.Timestamp,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal verification payload: %w", err)
	}

	var rBytes, sBytes []byte
	_, err = fmt.Sscanf(report.Signature, "%x:%x", &rBytes, &sBytes)
	if err != nil {
		return fmt.Errorf("invalid signature encoding: %w", err)
	}

	r := new(big.Int).SetBytes(rBytes)
	s := new(big.Int).SetBytes(sBytes)
	hash := sha256.Sum256(payload)

	if !ecdsa.Verify(ManufacturerPubKey, hash[:], r, s) {
		return errors.New("enclave attestation signature verification failed")
	}

	return nil
}
