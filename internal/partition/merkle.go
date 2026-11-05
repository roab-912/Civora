package partition

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// RFC 9162 Merkle tree: leaves H(0x00 || bytes), nodes H(0x01 || L || R).
func leaf(b []byte) []byte { h := sha256.Sum256(append([]byte{0}, b...)); return h[:] }
func branch(l, r []byte) []byte {
	h := sha256.Sum256(append(append([]byte{1}, l...), r...))
	return h[:]
}
func split(n int) int {
	k := 1
	for k*2 < n {
		k *= 2
	}
	return k
}
func root(items [][]byte) []byte {
	if len(items) == 0 {
		h := sha256.Sum256(nil)
		return h[:]
	}
	if len(items) == 1 {
		return leaf(items[0])
	}
	k := split(len(items))
	return branch(root(items[:k]), root(items[k:]))
}
func LogRoot(items [][]byte) string { return hex.EncodeToString(root(items)) }
func Inclusion(items [][]byte, index int) ([]string, error) {
	if index < 0 || index >= len(items) {
		return nil, fmt.Errorf("inclusion index")
	}
	if len(items) == 1 {
		return []string{}, nil
	}
	k := split(len(items))
	if index < k {
		p, e := Inclusion(items[:k], index)
		return append(p, LogRoot(items[k:])), e
	}
	p, e := Inclusion(items[k:], index-k)
	return append(p, LogRoot(items[:k])), e
}
func hashes(proof []string) ([][]byte, error) {
	if len(proof) > 64 {
		return nil, fmt.Errorf("Merkle proof bound")
	}
	out := [][]byte{}
	for _, s := range proof {
		b, e := hex.DecodeString(s)
		if e != nil || len(b) != 32 || hex.EncodeToString(b) != s {
			return nil, fmt.Errorf("Merkle hash encoding")
		}
		out = append(out, b)
	}
	return out, nil
}
func VerifyInclusion(digest string, item []byte, index, size uint64, proof []string) error {
	if size == 0 || index >= size {
		return fmt.Errorf("inclusion size/index")
	}
	ps, e := hashes(proof)
	if e != nil {
		return e
	}
	fn, sn := index, size-1
	r := leaf(item)
	for _, p := range ps {
		if sn == 0 {
			return fmt.Errorf("excess inclusion path")
		}
		if fn&1 == 1 || fn == sn {
			r = branch(p, r)
			for fn != 0 && fn&1 == 0 {
				fn >>= 1
				sn >>= 1
			}
		} else {
			r = branch(r, p)
		}
		fn >>= 1
		sn >>= 1
	}
	if sn != 0 || hex.EncodeToString(r) != digest {
		return fmt.Errorf("inclusion root mismatch")
	}
	return nil
}
func Consistency(items [][]byte, old int) ([]string, error) {
	if old < 0 || old > len(items) {
		return nil, fmt.Errorf("consistency size")
	}
	if old == 0 {
		return []string{}, nil
	}
	var sub func([][]byte, int, bool) []string
	sub = func(xs [][]byte, m int, complete bool) []string {
		if m == len(xs) {
			if complete {
				return []string{}
			}
			return []string{LogRoot(xs)}
		}
		k := split(len(xs))
		if m <= k {
			return append(sub(xs[:k], m, complete), LogRoot(xs[k:]))
		}
		return append(sub(xs[k:], m-k, false), LogRoot(xs[:k]))
	}
	return sub(items, old, true), nil
}
func VerifyConsistency(oldRoot, newRoot string, m, n uint64, proof []string) error {
	for _, s := range []string{oldRoot, newRoot} {
		b, e := hex.DecodeString(s)
		if e != nil || len(b) != 32 || hex.EncodeToString(b) != s {
			return fmt.Errorf("log root encoding")
		}
	}
	if m > n {
		return fmt.Errorf("log truncation")
	}
	ps, e := hashes(proof)
	if e != nil {
		return e
	}
	if m == 0 {
		if oldRoot != LogRoot(nil) || len(ps) != 0 || n == 0 && newRoot != LogRoot(nil) {
			return fmt.Errorf("empty log proof")
		}
		return nil
	}
	if m == n {
		if oldRoot != newRoot || len(ps) != 0 {
			return fmt.Errorf("same-size log fork")
		}
		return nil
	}
	old, e := hex.DecodeString(oldRoot)
	if e != nil || len(old) != 32 {
		return fmt.Errorf("old root")
	}
	fn, sn := m-1, n-1
	for fn&1 == 1 {
		fn >>= 1
		sn >>= 1
	}
	var fr, sr []byte
	if fn == 0 {
		fr = old
		sr = old
	} else {
		if len(ps) == 0 {
			return fmt.Errorf("short consistency")
		}
		fr = ps[0]
		sr = ps[0]
		ps = ps[1:]
	}
	for _, p := range ps {
		if sn == 0 {
			return fmt.Errorf("excess consistency")
		}
		if fn&1 == 1 || fn == sn {
			fr = branch(p, fr)
			sr = branch(p, sr)
			for fn != 0 && fn&1 == 0 {
				fn >>= 1
				sn >>= 1
			}
		} else {
			sr = branch(sr, p)
		}
		fn >>= 1
		sn >>= 1
	}
	expected, e := hex.DecodeString(newRoot)
	if e != nil || len(expected) != 32 || sn != 0 || !bytes.Equal(fr, old) || !bytes.Equal(sr, expected) {
		return fmt.Errorf("log consistency mismatch")
	}
	return nil
}
