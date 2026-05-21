package tests

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"calyx/bootstrap"
	"calyx/crypto"
	"calyx/engine"
	pb "calyx/proto"
	"calyx/server"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// runMockSTUNServer spins up a lightweight, RFC 5389-compliant local UDP STUN server
// to make STUN client tests 100% deterministic, hermetic, and offline-friendly.
func runMockSTUNServer(t *testing.T) (string, func()) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to start mock STUN server: %v", err)
	}

	stopChan := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			select {
			case <-stopChan:
				return
			default:
				_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
				n, remoteAddr, err := conn.ReadFromUDP(buf)
				if err != nil {
					continue
				}
				if n < 20 {
					continue
				}

				// Parse STUN Header
				msgType := binary.BigEndian.Uint16(buf[0:2])
				cookie := binary.BigEndian.Uint32(buf[4:8])
				txID := buf[8:20]

				if msgType == 0x0001 && cookie == 0x2112A442 { // Binding Request
					// Build Binding Success Response (type 0x0101)
					resp := make([]byte, 32)
					binary.BigEndian.PutUint16(resp[0:2], 0x0101)
					binary.BigEndian.PutUint16(resp[2:4], 12) // Attributes size (8-byte XOR-MAPPED-ADDRESS + 4-byte header)
					binary.BigEndian.PutUint32(resp[4:8], 0x2112A442)
					copy(resp[8:20], txID)

					// XOR-MAPPED-ADDRESS attribute
					binary.BigEndian.PutUint16(resp[20:22], 0x0020) // Type
					binary.BigEndian.PutUint16(resp[22:24], 8)      // Length
					resp[24] = 0x00                                 // Reserved
					resp[25] = 0x01                                 // Family IPv4

					// XOR port: say 12345
					xPort := uint16(12345) ^ 0x2112
					binary.BigEndian.PutUint16(resp[26:28], xPort)

					// XOR IP: 127.0.0.1
					ipBytes := net.ParseIP("127.0.0.1").To4()
					ipVal := binary.BigEndian.Uint32(ipBytes)
					xIP := ipVal ^ 0x2112A442
					binary.BigEndian.PutUint32(resp[28:32], xIP)

					_, _ = conn.WriteToUDP(resp, remoteAddr)
				}
			}
		}
	}()

	addrStr := conn.LocalAddr().String()
	cleanup := func() {
		close(stopChan)
		wg.Wait()
		conn.Close()
	}

	return addrStr, cleanup
}

func TestSTUNClientNATTraversal(t *testing.T) {
	stunAddr, cleanup := runMockSTUNServer(t)
	defer cleanup()

	t.Logf("Mock STUN server listening on %s", stunAddr)

	mappedAddr, err := crypto.GetExternalIPMappedAddress(stunAddr)
	if err != nil {
		t.Fatalf("Failed to resolve mapped address via STUN: %v", err)
	}

	expectedAddr := "127.0.0.1:12345"
	if mappedAddr != expectedAddr {
		t.Errorf("Mismatched external mapped address: expected '%s', got '%s'", expectedAddr, mappedAddr)
	}
	t.Logf("STUN resolution success! Mapped address: %s", mappedAddr)
}

func TestModelWeightsSerializationAndSelfHealing(t *testing.T) {
	tempDir := t.TempDir()
	weightsFile := filepath.Join(tempDir, "test_layer.calyx")

	// 1. Assert self-healing logic creates stable identity defaults
	err := engine.EnsureWeightsExist(weightsFile, 4)
	if err != nil {
		t.Fatalf("EnsureWeightsExist failed: %v", err)
	}

	if _, err := os.Stat(weightsFile); os.IsNotExist(err) {
		t.Fatalf("Self-healing failed to write weights file to disk")
	}

	// 2. Load self-healed weights and verify identity invariants
	layer, err := engine.LoadWeights(weightsFile)
	if err != nil {
		t.Fatalf("Failed to load weights: %v", err)
	}

	if layer.HiddenDim != 4 {
		t.Errorf("Mismatched HiddenDim: expected 4, got %d", layer.HiddenDim)
	}

	// Wq is identity by default
	for i := 0; i < 16; i++ {
		expected := 0.0
		if i%5 == 0 {
			expected = 1.0
		}
		if layer.Wq[i] != expected {
			t.Errorf("Wq matrix mismatch at index %d: expected %f, got %f", i, expected, layer.Wq[i])
		}
	}

	// 3. Mutate layer and save Custom Weights
	layer.Wq[0] = 42.42
	layer.Wmlp1[3] = -99.9
	err = engine.SaveWeights(weightsFile, layer)
	if err != nil {
		t.Fatalf("SaveWeights failed: %v", err)
	}

	// 4. Reload mutated weights and verify values
	reloaded, err := engine.LoadWeights(weightsFile)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	if reloaded.Wq[0] != 42.42 || reloaded.Wmlp1[3] != -99.9 {
		t.Errorf("Saved weight mutations were not preserved correctly in binary file")
	}

	t.Log("Model weights custom binary file load/save validated successfully!")
}

func TestBinarySGXQuoteAttestationReport(t *testing.T) {
	enclaveAddr := "localhost:50051"
	expectedMeasurement := "5a556d3570696c65645f43616c79785f456e636c6176655f436f64655f48617368"

	// 1. Generate Quote
	report, err := crypto.GenerateAttestationReport(enclaveAddr, expectedMeasurement)
	if err != nil {
		t.Fatalf("GenerateAttestationReport failed: %v", err)
	}

	// 2. Parse binary quote fields manually
	rawQuote, err := hex.DecodeString(report.Signature)
	if err != nil {
		t.Fatalf("Failed to decode base64/hex signature quote: %v", err)
	}

	quote, err := crypto.DeserializeSGXQuote(rawQuote)
	if err != nil {
		t.Fatalf("DeserializeSGXQuote failed: %v", err)
	}

	if quote.Version != 3 {
		t.Errorf("Unexpected quote version: %d", quote.Version)
	}
	if string(quote.QEid[:16]) != "IntelSGXEnclaveI" {
		t.Errorf("QEid mismatch: expected 'IntelSGXEnclaveI', got '%s'", string(quote.QEid[:]))
	}

	// Check MRENCLAVE match
	var expectedMRENCLAVEBytes [32]byte
	decodedExpected, err := hex.DecodeString(expectedMeasurement)
	if err != nil || len(decodedExpected) != 32 {
		expectedMRENCLAVEBytes = sha256.Sum256([]byte(expectedMeasurement))
	} else {
		copy(expectedMRENCLAVEBytes[:], decodedExpected)
	}

	if !bytesEqual(quote.MRENCLAVE[:], expectedMRENCLAVEBytes[:]) {
		t.Errorf("Enclave measurement inside quote does not match expected")
	}

	// 3. Cryptographic Validation
	err = crypto.VerifyAttestationReport(report, expectedMeasurement)
	if err != nil {
		t.Fatalf("VerifyAttestationReport failed on fresh valid quote: %v", err)
	}

	// 4. Expiration validation
	expiredReport := &crypto.AttestationReport{
		EnclaveAddr: report.EnclaveAddr,
		MRENCLAVE:   report.MRENCLAVE,
		Timestamp:   time.Now().Unix() - 600, // 10 minutes ago
		Signature:   report.Signature,
	}
	err = crypto.VerifyAttestationReport(expiredReport, expectedMeasurement)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("VerifyAttestationReport did not catch expired timestamp (err: %v)", err)
	}

	// 5. Sealed UserData address mismatch validation
	tamperedReport := &crypto.AttestationReport{
		EnclaveAddr: "localhost:99999", // mismatch with sealed address
		MRENCLAVE:   report.MRENCLAVE,
		Timestamp:   report.Timestamp,
		Signature:   report.Signature,
	}
	err = crypto.VerifyAttestationReport(tamperedReport, expectedMeasurement)
	if err == nil || !strings.Contains(err.Error(), "enclave address mismatch") {
		t.Errorf("VerifyAttestationReport did not catch address mismatch in sealed UserData (err: %v)", err)
	}

	t.Log("Binary Intel SGX Quote attestation layout and verification successfully validated!")
}

func TestModelDirectoryDiscoveryRouting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	bootstrapAddr := getFreeTCPPort(t)
	server1Addr := getFreeTCPPort(t)
	server2Addr := getFreeTCPPort(t)

	// Start Bootstrap Coordinator
	bSrv, err := bootstrap.StartBootstrapServer(bootstrapAddr, &wg)
	if err != nil {
		t.Fatalf("Failed to start Bootstrap server: %v", err)
	}
	defer bSrv.GracefulStop()
	time.Sleep(100 * time.Millisecond)

	// Configure model IDs
	modelGemma := "google/gemma-2b"
	modelLlama := "meta-llama/Llama-3-8b"

	// Start Server 1 (Layers 1-4) under modelGemma
	server.ModelID = modelGemma
	sSrv1, err := server.StartServer(ctx, bootstrapAddr, 1, 4, server1Addr, 5*time.Second, 2, nil, &wg, false)
	if err != nil {
		t.Fatalf("Failed to start Server 1: %v", err)
	}
	defer sSrv1.GracefulStop()

	// Start Server 2 (Layers 1-8) under modelLlama
	server.ModelID = modelLlama
	sSrv2, err := server.StartServer(ctx, bootstrapAddr, 1, 8, server2Addr, 5*time.Second, 2, nil, &wg, false)
	if err != nil {
		t.Fatalf("Failed to start Server 2: %v", err)
	}
	defer sSrv2.GracefulStop()
	time.Sleep(200 * time.Millisecond)

	// Test Route Queries
	cert, err := crypto.GenerateKeyPairAndCert()
	if err != nil {
		t.Fatalf("Failed to generate TLS cert: %v", err)
	}
	clientTLS := crypto.GetClientTLSConfig(cert)
	conn, err := grpc.NewClient(bootstrapAddr, grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		t.Fatalf("Client failed to dial bootstrap: %v", err)
	}
	defer conn.Close()
	bootstrapClient := pb.NewBootstrapServiceClient(conn)

	// 1. Query Route for Gemma layers 1-4
	mdGemma := metadata.Pairs("model-id", modelGemma)
	ctxGemma := metadata.NewOutgoingContext(ctx, mdGemma)
	respGemma, err := bootstrapClient.FindRoute(ctxGemma, &pb.RouteRequest{StartLayer: 1, EndLayer: 4})
	if err != nil {
		t.Fatalf("FindRoute for Gemma failed: %v", err)
	}
	if len(respGemma.Addresses) != 1 || respGemma.Addresses[0] != server1Addr {
		t.Errorf("Unexpected route resolved for Gemma: expected [%s], got %v", server1Addr, respGemma.Addresses)
	}

	// 2. Query Route for Llama layers 1-8
	mdLlama := metadata.Pairs("model-id", modelLlama)
	ctxLlama := metadata.NewOutgoingContext(ctx, mdLlama)
	respLlama, err := bootstrapClient.FindRoute(ctxLlama, &pb.RouteRequest{StartLayer: 1, EndLayer: 8})
	if err != nil {
		t.Fatalf("FindRoute for Llama failed: %v", err)
	}
	if len(respLlama.Addresses) != 1 || respLlama.Addresses[0] != server2Addr {
		t.Errorf("Unexpected route resolved for Llama: expected [%s], got %v", server2Addr, respLlama.Addresses)
	}

	// 3. Query Listing Catalog (list-models = true)
	mdList := metadata.Pairs("list-models", "true")
	ctxList := metadata.NewOutgoingContext(ctx, mdList)
	respList, err := bootstrapClient.FindRoute(ctxList, &pb.RouteRequest{StartLayer: 1, EndLayer: 1})
	if err != nil {
		t.Fatalf("FindRoute listing failed: %v", err)
	}

	// Verify catalog elements exist
	gemmaFound := false
	llamaFound := false
	s1Found := false
	s2Found := false

	for _, entry := range respList.Addresses {
		if entry == "MODEL:"+modelGemma {
			gemmaFound = true
		} else if entry == "MODEL:"+modelLlama {
			llamaFound = true
		} else if strings.Contains(entry, server1Addr) && strings.Contains(entry, "LAYERS:1-4") {
			s1Found = true
		} else if strings.Contains(entry, server2Addr) && strings.Contains(entry, "LAYERS:1-8") {
			s2Found = true
		}
	}

	if !gemmaFound || !llamaFound {
		t.Errorf("Model catalog listing incomplete: gemmaFound=%t, llamaFound=%t", gemmaFound, llamaFound)
	}
	if !s1Found || !s2Found {
		t.Errorf("Model catalog nodes listing incomplete: s1Found=%t, s2Found=%t", s1Found, s2Found)
	}

	t.Log("Context-based gRPC model discovery registration and list-models routing catalog fully verified!")
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}
