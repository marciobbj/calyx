package crypto

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// Challenge represents a Proof-of-Work puzzle challenge
type Challenge struct {
	Salt       string
	Difficulty int
	Timestamp  int64
}

// GenerateChallenge creates a new challenge with a salt and timestamp
func GenerateChallenge(salt string, difficulty int) *Challenge {
	return &Challenge{
		Salt:       salt,
		Difficulty: difficulty,
		Timestamp:  time.Now().Unix(),
	}
}

// Solve finds a nonce that satisfies the PoW difficulty for a challenge
func Solve(salt string, difficulty int) string {
	targetPrefix := strings.Repeat("0", difficulty)
	var nonce int64
	for {
		candidate := fmt.Sprintf("%s:%d", salt, nonce)
		hash := sha256.Sum256([]byte(candidate))
		hexHash := fmt.Sprintf("%x", hash)
		if strings.HasPrefix(hexHash, targetPrefix) {
			return fmt.Sprintf("%d", nonce)
		}
		nonce++
	}
}

// Verify checks if the ticket nonce satisfies the PoW difficulty for a challenge
func Verify(salt string, nonce string, difficulty int) bool {
	targetPrefix := strings.Repeat("0", difficulty)
	candidate := fmt.Sprintf("%s:%s", salt, nonce)
	hash := sha256.Sum256([]byte(candidate))
	hexHash := fmt.Sprintf("%x", hash)
	return strings.HasPrefix(hexHash, targetPrefix)
}
