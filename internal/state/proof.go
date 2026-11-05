package state

import (
	"bytes"
	"civora/internal/codec"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Proof lists siblings from the root to the leaf. Only membership is exposed.
type Proof struct {
	Entry    Entry    `json:"entry"`
	Siblings []string `json:"siblings"`
}

func Prove(entries []Entry, table string, key any) (Proof, error) {
	p := Proof{Siblings: []string{}}
	target := codec.Hash("state/key", []any{table, key})
	kb, _ := hex.DecodeString(target)
	found := false
	for _, e := range entries {
		if e.Table == table && bytes.Equal(codec.Must(e.Key), codec.Must(key)) {
			p.Entry = e
			found = true
		}
	}
	if !found {
		return p, fmt.Errorf("state entry absent")
	}
	current := append([]Entry{}, entries...)
	for d := 0; d < 256; d++ {
		same, other := []Entry{}, []Entry{}
		for _, e := range current {
			k, _ := hex.DecodeString(codec.Hash("state/key", []any{e.Table, e.Key}))
			if (k[d/8]>>(7-uint(d%8)))&1 == (kb[d/8]>>(7-uint(d%8)))&1 {
				same = append(same, e)
			} else {
				other = append(other, e)
			}
		}
		// Prefixes have already been removed: reconstruct this subtree recursively.
		var subtree func([]Entry, int) [32]byte
		subtree = func(es []Entry, depth int) [32]byte {
			if len(es) == 0 {
				return empty[depth]
			}
			if depth == 256 {
				e := es[0]
				k, _ := hex.DecodeString(codec.Hash("state/key", []any{e.Table, e.Key}))
				v := sha256.Sum256(codec.Must(e.Value))
				return sha256.Sum256(append(append([]byte{0}, k...), v[:]...))
			}
			l, r := []Entry{}, []Entry{}
			for _, e := range es {
				k, _ := hex.DecodeString(codec.Hash("state/key", []any{e.Table, e.Key}))
				if k[depth/8]&(1<<uint(7-depth%8)) == 0 {
					l = append(l, e)
				} else {
					r = append(r, e)
				}
			}
			return parent(subtree(l, depth+1), subtree(r, depth+1))
		}
		h := subtree(other, d+1)
		p.Siblings = append(p.Siblings, hex.EncodeToString(h[:]))
		current = same
	}
	return p, nil
}
func Verify(root string, p Proof) error {
	if len(p.Siblings) != 256 {
		return fmt.Errorf("state proof depth")
	}
	k, _ := hex.DecodeString(codec.Hash("state/key", []any{p.Entry.Table, p.Entry.Key}))
	v := sha256.Sum256(codec.Must(p.Entry.Value))
	h := sha256.Sum256(append(append([]byte{0}, k...), v[:]...))
	for d := 255; d >= 0; d-- {
		raw, e := hex.DecodeString(p.Siblings[d])
		if e != nil || len(raw) != 32 || hex.EncodeToString(raw) != p.Siblings[d] {
			return fmt.Errorf("state sibling encoding")
		}
		var s [32]byte
		copy(s[:], raw)
		if k[d/8]&(1<<uint(7-d%8)) == 0 {
			h = parent(h, s)
		} else {
			h = parent(s, h)
		}
	}
	if hex.EncodeToString(h[:]) != root {
		return fmt.Errorf("state proof root mismatch")
	}
	return nil
}
