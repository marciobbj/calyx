package crypto

import (
	"crypto/tls"
	"net"
	"sync"
	"testing"
)

func TestMutualTLS13GenerationAndNegotiation(t *testing.T) {
	// Generate Server Cert
	serverCert, err := GenerateKeyPairAndCert()
	if err != nil {
		t.Fatalf("Failed to generate server certificate: %v", err)
	}

	// Generate Client Cert
	clientCert, err := GenerateKeyPairAndCert()
	if err != nil {
		t.Fatalf("Failed to generate client certificate: %v", err)
	}

	serverConfig := GetServerTLSConfig(serverCert)
	clientConfig := GetClientTLSConfig(clientCert)

	// Bind free TCP port
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer lis.Close()

	addr := lis.Addr().String()

	var wg sync.WaitGroup
	wg.Add(1)

	// Start server routine
	go func() {
		defer wg.Done()
		conn, sErr := lis.Accept()
		if sErr != nil {
			return
		}
		defer conn.Close()

		tlsConn := tls.Server(conn, serverConfig)
		if handshakeErr := tlsConn.Handshake(); handshakeErr != nil {
			t.Errorf("Server handshake failed: %v", handshakeErr)
			return
		}
	}()

	// Connect client
	cConn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Client Dial failed: %v", err)
	}
	defer cConn.Close()

	tlsClientConn := tls.Client(cConn, clientConfig)
	if handshakeErr := tlsClientConn.Handshake(); handshakeErr != nil {
		t.Fatalf("Client handshake failed: %v", handshakeErr)
	}

	wg.Wait()
	t.Log("Successfully completed TLS 1.3 mutual handshake negotiation!")
}
