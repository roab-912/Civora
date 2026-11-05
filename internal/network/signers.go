package network

import (
	"encoding/hex"
	"fmt"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"path/filepath"
)

// Generation keys are provisioned once at initialization, only for NEW test
// identities. Reusing a public key always reuses its original last-sign state.
func generationKeys(g Genesis, chain string, seat int) ([]ed25519.PrivKey, error) {
	initial := TestKey(chain, "consensus", seat)
	keys := []ed25519.PrivKey{initial}
	known := map[string]bool{hex.EncodeToString(initial.PubKey().Bytes()): true}
	for i, r := range g.Rotations {
		if len(r.Keys) != 7 {
			return nil, fmt.Errorf("rotation roster")
		}
		expected := r.Keys[seat]
		if known[expected] {
			continue
		}
		role := "consensus-next"
		if i > 0 {
			role = fmt.Sprintf("consensus-next-%d", i+1)
		}
		k := TestKey(chain, role, seat)
		if hex.EncodeToString(k.PubKey().Bytes()) != expected {
			return nil, fmt.Errorf("development rotation key must match its TEST_ONLY seat and generation")
		}
		known[expected] = true
		keys = append(keys, k)
	}
	return keys, nil
}
func generationPaths(home, public string) (string, string) {
	return filepath.Join(home, "config", "priv_validator_key."+public+".json"), filepath.Join(home, "data", "priv_validator_state."+public+".json")
}
func expectedSigner(g Genesis, chain string, seat int, generation uint64) (string, error) {
	if generation > uint64(len(g.Rotations)) {
		return "", fmt.Errorf("signer generation is not preauthorized in genesis")
	}
	if generation == 0 {
		return hex.EncodeToString(TestKey(chain, "consensus", seat).PubKey().Bytes()), nil
	}
	return g.Rotations[generation-1].Keys[seat], nil
}
