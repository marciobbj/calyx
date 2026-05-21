package dht

import (
	"sync"
	"testing"
)

func TestKademliaXORMetric(t *testing.T) {
	idA := HashString("peer_A")
	idB := HashString("peer_B")
	idC := HashString("peer_C")

	t.Logf("A: %s", idA)
	t.Logf("B: %s", idB)
	t.Logf("C: %s", idC)

	distAB := XOR(idA, idB)

	// In Kademlia, distance is symmetric: dist(a, b) == dist(b, a)
	distBA := XOR(idB, idA)
	if distAB != distBA {
		t.Error("XOR distance is not symmetric")
	}

	// Compare distances to Target (A)
	cmp := CompareDistances(idA, idB, idC)
	if cmp < 0 {
		t.Log("B is closer to A than C is")
	} else if cmp > 0 {
		t.Log("C is closer to A than B is")
	} else {
		t.Log("B and C are at equal distance from A")
	}

	// XOR of self should be all 0s
	distAA := XOR(idA, idA)
	var zeros NodeID
	if distAA != zeros {
		t.Error("XOR of self is not zeros")
	}
}

func TestKademliaDHTRoutingAndLookup(t *testing.T) {
	network := &sync.Map{}

	// Create 3 virtual DHT nodes in the network
	node1 := NewKademliaDHT("127.0.0.1:8001", network)
	node2 := NewKademliaDHT("127.0.0.1:8002", network)
	node3 := NewKademliaDHT("127.0.0.1:8003", network)

	// Populate routing tables (Node 1 knows Node 2, Node 2 knows Node 3)
	node1.AddPeer(Peer{ID: node2.LocalID, Address: node2.Address, StartLayer: 1, EndLayer: 4})
	node2.AddPeer(Peer{ID: node3.LocalID, Address: node3.Address, StartLayer: 5, EndLayer: 8})

	// Store value in Node 3 (Node 3 provides layer 7)
	node3.Store(7, "127.0.0.1:8003")

	t.Log("Running recursive lookup from Node 1 for layer 7...")
	providers := node1.RecursiveFindValue(7, nil)

	if len(providers) == 0 {
		t.Fatal("Failed to locate providers for layer 7 recursively through DHT")
	}

	if providers[0] != "127.0.0.1:8003" {
		t.Errorf("Expected provider '127.0.0.1:8003', got '%s'", providers[0])
	}

	t.Log("Successfully resolved provider address via Kademlia DHT recursive search!")
}
