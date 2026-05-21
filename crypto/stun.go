package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

// STUN protocol header and attribute constants
const (
	stunBindingRequest = 0x0001
	stunBindingSuccess = 0x0101
	magicCookie        = 0x2112A442
	attrXorMappedAddr  = 0x0020
	attrMappedAddr     = 0x0001
)

// GetExternalIPMappedAddress queries a STUN server over UDP and returns the public IP and mapped port.
// If it fails, it returns an error.
func GetExternalIPMappedAddress(stunServer string) (string, error) {
	if stunServer == "" {
		stunServer = "stun.l.google.com:19302"
	}

	// Resolve STUN server address
	addr, err := net.ResolveUDPAddr("udp", stunServer)
	if err != nil {
		return "", fmt.Errorf("failed to resolve STUN server: %w", err)
	}

	// Dial STUN server UDP connection
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return "", fmt.Errorf("failed to connect to STUN server: %w", err)
	}
	defer conn.Close()

	// Build STUN Binding Request header (20 bytes)
	// Format:
	// - Type (2 bytes): 0x0001 (Binding Request)
	// - Length (2 bytes): 0x0000 (No attributes)
	// - Magic Cookie (4 bytes): 0x2112A442
	// - Transaction ID (12 bytes): Random bytes
	req := make([]byte, 20)
	binary.BigEndian.PutUint16(req[0:2], stunBindingRequest)
	binary.BigEndian.PutUint16(req[2:4], 0)
	binary.BigEndian.PutUint32(req[4:8], magicCookie)
	if _, err := rand.Read(req[8:20]); err != nil {
		return "", fmt.Errorf("failed to generate STUN transaction ID: %w", err)
	}

	// Send request
	if _, err := conn.Write(req); err != nil {
		return "", fmt.Errorf("failed to send STUN request: %w", err)
	}

	// Read response with a timeout
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return "", err
	}

	resp := make([]byte, 1024)
	n, _, err := conn.ReadFrom(resp)
	if err != nil {
		return "", fmt.Errorf("failed to read STUN response: %w", err)
	}

	if n < 20 {
		return "", errors.New("invalid STUN response: message too short")
	}

	// Parse header
	msgType := binary.BigEndian.Uint16(resp[0:2])
	msgLen := binary.BigEndian.Uint16(resp[2:4])
	cookie := binary.BigEndian.Uint32(resp[4:8])

	if msgType != stunBindingSuccess {
		return "", fmt.Errorf("STUN query failed: received non-success response type 0x%04x", msgType)
	}
	if cookie != magicCookie {
		return "", errors.New("invalid STUN response: magic cookie mismatch")
	}
	if int(msgLen) > n-20 {
		return "", errors.New("invalid STUN response: length mismatch")
	}

	// Parse Attributes
	offset := 20
	end := 20 + int(msgLen)
	if end > n {
		end = n
	}

	for offset+4 <= end {
		attrType := binary.BigEndian.Uint16(resp[offset : offset+2])
		attrLen := int(binary.BigEndian.Uint16(resp[offset+2 : offset+4]))
		offset += 4

		if offset+attrLen > end {
			break
		}

		attrVal := resp[offset : offset+attrLen]
		// Align offset to 4-byte boundaries
		offset += attrLen
		if remainder := attrLen % 4; remainder != 0 {
			offset += 4 - remainder
		}

		switch attrType {
		case attrXorMappedAddr:
			if attrLen < 8 {
				continue
			}
			// Byte 0: Reserved (0x00)
			// Byte 1: Family (0x01 = IPv4, 0x02 = IPv6)
			family := attrVal[1]
			if family == 0x01 { // IPv4
				// Port is XORed with high 16 bits of Magic Cookie (0x2112)
				xPort := binary.BigEndian.Uint16(attrVal[2:4])
				port := xPort ^ 0x2112

				// IP is XORed with Magic Cookie (0x2112A442)
				xIP := binary.BigEndian.Uint32(attrVal[4:8])
				ip := xIP ^ magicCookie

				ipBytes := make([]byte, 4)
				binary.BigEndian.PutUint32(ipBytes, ip)
				externalIP := net.IP(ipBytes).String()
				return fmt.Sprintf("%s:%d", externalIP, port), nil
			}
		case attrMappedAddr:
			if attrLen < 8 {
				continue
			}
			family := attrVal[1]
			if family == 0x01 { // IPv4
				port := binary.BigEndian.Uint16(attrVal[2:4])
				ipBytes := attrVal[4:8]
				externalIP := net.IP(ipBytes).String()
				return fmt.Sprintf("%s:%d", externalIP, port), nil
			}
		}
	}

	return "", errors.New("STUN server did not return a valid mapped address")
}
