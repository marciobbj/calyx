package crypto

import (
	"testing"
)

func TestProofOfWorkSolveAndVerify(t *testing.T) {
	salt := "test_session_salt_123"
	difficulty := 3 // requires prefix "000"

	t.Log("Solving puzzle...")
	nonce := Solve(salt, difficulty)
	t.Logf("Found nonce: %s", nonce)

	if !Verify(salt, nonce, difficulty) {
		t.Errorf("Expected verification to pass for solved nonce %s", nonce)
	}

	if Verify(salt, "wrong_nonce", difficulty) {
		t.Error("Expected verification to fail for wrong nonce")
	}

	if Verify("wrong_salt", nonce, difficulty) {
		t.Error("Expected verification to fail for wrong salt")
	}
}

func TestProofOfWorkChallengeGen(t *testing.T) {
	c := GenerateChallenge("salt", 4)
	if c.Difficulty != 4 {
		t.Errorf("Expected difficulty 4, got %d", c.Difficulty)
	}
	if c.Salt != "salt" {
		t.Errorf("Expected salt 'salt', got '%s'", c.Salt)
	}
	if c.Timestamp == 0 {
		t.Error("Expected non-zero timestamp")
	}
}
