package state

import (
	"civora/internal/codec"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Witness is the v0.6 public API path: siblings run FROM LEAF TO ROOT.
// v0.5 Proof retains its root-to-leaf order for checkpoint compatibility.
type Witness struct {
	Entry    Entry    `json:"entry"`
	Exists   bool     `json:"exists"`
	Siblings []string `json:"siblings"`
}

func WitnessFor(entries []Entry, table string, key any) (Witness, error) {
	p, e := prove(entries, table, key, true)
	if e != nil {
		return Witness{}, e
	}
	exists := false
	for _, v := range entries {
		if v.Table == table && string(codec.Must(v.Key)) == string(codec.Must(key)) {
			exists = true
			break
		}
	}
	siblings := append([]string{}, p.Siblings...)
	for i, j := 0, 255; i < j; i, j = i+1, j-1 {
		siblings[i], siblings[j] = siblings[j], siblings[i]
	}
	return Witness{p.Entry, exists, siblings}, nil
}
func VerifyWitness(root string, p Witness) error {
	if len(p.Siblings) != 256 || !p.Exists && p.Entry.Value != nil {
		return fmt.Errorf("state witness shape")
	}
	k, _ := hex.DecodeString(codec.Hash("state/key", []any{p.Entry.Table, p.Entry.Key}))
	h := empty[256]
	if p.Exists {
		v := sha256.Sum256(codec.Must(p.Entry.Value))
		h = sha256.Sum256(append(append([]byte{0}, k...), v[:]...))
	}
	for i, s := range p.Siblings {
		d := 255 - i
		raw, e := hex.DecodeString(s)
		if e != nil || len(raw) != 32 || hex.EncodeToString(raw) != s {
			return fmt.Errorf("state witness hash")
		}
		var other [32]byte
		copy(other[:], raw)
		if k[d/8]&(1<<uint(7-d%8)) == 0 {
			h = parent(h, other)
		} else {
			h = parent(other, h)
		}
	}
	if hex.EncodeToString(h[:]) != root {
		return fmt.Errorf("state witness root")
	}
	return nil
}
