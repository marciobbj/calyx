package dht

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
)

// NodeID represents a 160-bit Kademlia key
type NodeID [20]byte

// HashString generates a NodeID from a string
func HashString(s string) NodeID {
	hasher := sha1.New()
	hasher.Write([]byte(s))
	var id NodeID
	copy(id[:], hasher.Sum(nil))
	return id
}

// String returns the hex representation of NodeID
func (id NodeID) String() string {
	return hex.EncodeToString(id[:])
}

// XOR computes the bitwise XOR distance between two NodeIDs
func XOR(a, b NodeID) NodeID {
	var res NodeID
	for i := 0; i < 20; i++ {
		res[i] = a[i] ^ b[i]
	}
	return res
}

// CompareDistances compares XOR distances to a target NodeID
// returns -1 if a is closer than b, 1 if b is closer, 0 if equal
func CompareDistances(target, a, b NodeID) int {
	distA := XOR(target, a)
	distB := XOR(target, b)
	for i := 0; i < 20; i++ {
		if distA[i] < distB[i] {
			return -1
		}
		if distA[i] > distB[i] {
			return 1
		}
	}
	return 0
}

// Peer represents an active peer node in the DHT network
type Peer struct {
	ID         NodeID
	Address    string
	StartLayer int32
	EndLayer   int32
}

// KademliaDHT implements the Kademlia DHT routing table and search protocol
type KademliaDHT struct {
	mu           sync.RWMutex
	LocalID      NodeID
	Address      string
	Peers        map[NodeID]Peer
	StoreValues  map[int32][]string // Layer -> list of Node Addresses
	NetworkNodes *sync.Map          // Global simulation network registry of NodeID -> *KademliaDHT
}

var GlobalNetwork = &sync.Map{}

// NewKademliaDHT creates a new Kademlia DHT instance
func NewKademliaDHT(addr string, network *sync.Map) *KademliaDHT {
	if network == nil {
		network = GlobalNetwork
	}
	dht := &KademliaDHT{
		LocalID:      HashString(addr),
		Address:      addr,
		Peers:        make(map[NodeID]Peer),
		StoreValues:  make(map[int32][]string),
		NetworkNodes: network,
	}
	if network != nil {
		network.Store(dht.LocalID, dht)
	}
	return dht
}

// AddPeer adds a peer node to the local routing table
func (d *KademliaDHT) AddPeer(peer Peer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if peer.ID == d.LocalID {
		return
	}
	d.Peers[peer.ID] = peer
}

// Store registers provider address for specific layers
func (d *KademliaDHT) Store(layer int32, providerAddr string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	providers := d.StoreValues[layer]
	for _, addr := range providers {
		if addr == providerAddr {
			return
		}
	}
	d.StoreValues[layer] = append(providers, providerAddr)
}

// FindClosestPeers returns the top k closest peers to a given target key
func (d *KademliaDHT) FindClosestPeers(target NodeID, k int) []Peer {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var allPeers []Peer
	for _, p := range d.Peers {
		allPeers = append(allPeers, p)
	}

	sort.Slice(allPeers, func(i, j int) bool {
		return CompareDistances(target, allPeers[i].ID, allPeers[j].ID) < 0
	})

	if len(allPeers) > k {
		return allPeers[:k]
	}
	return allPeers
}

// GetProviders returns list of addresses providing a specific layer
func (d *KademliaDHT) GetProviders(layer int32) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.StoreValues[layer]
}

// RecursiveFindValue runs a distributed Kademlia search across nodes to locate layers providers
func (d *KademliaDHT) RecursiveFindValue(layer int32, visited map[NodeID]bool) []string {
	if visited == nil {
		visited = make(map[NodeID]bool)
	}
	visited[d.LocalID] = true

	// Check local storage first
	providers := d.GetProviders(layer)
	if len(providers) > 0 {
		return providers
	}

	// Find closest peers to query next
	targetKey := HashString(fmt.Sprintf("layer_%d", layer))
	closest := d.FindClosestPeers(targetKey, 3)

	for _, peer := range closest {
		if visited[peer.ID] {
			continue
		}
		if d.NetworkNodes != nil {
			if val, ok := d.NetworkNodes.Load(peer.ID); ok {
				nextDHT := val.(*KademliaDHT)
				res := nextDHT.RecursiveFindValue(layer, visited)
				if len(res) > 0 {
					return res
				}
			}
		}
	}

	return nil
}
