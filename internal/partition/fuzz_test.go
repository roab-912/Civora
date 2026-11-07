package partition

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func FuzzLogProofs(f *testing.F) {
	f.Add([]byte("item"), uint64(0), uint64(1), []byte{})
	f.Add([]byte{}, ^uint64(0), ^uint64(0), make([]byte, 64))
	f.Fuzz(func(t *testing.T, item []byte, index, size uint64, path []byte) {
		if len(item) > 8192 || len(path) > 64*32 {
			return
		}
		proof := []string{}
		for i := 0; i < len(path); i += 32 {
			end := i + 32
			if end > len(path) {
				end = len(path)
			}
			proof = append(proof, hex.EncodeToString(path[i:end]))
		}
		h := sha256.Sum256(item)
		root := hex.EncodeToString(h[:])
		_ = VerifyInclusion(root, item, index, size, proof)
		_ = VerifyConsistency(root, root, index, size, proof)
	})
}
