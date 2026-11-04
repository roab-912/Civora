package state

import (
	"bytes"
	"civora/internal/codec"
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

type Entry struct {
	Table string `json:"table"`
	Key   any    `json:"key"`
	Value any    `json:"value"`
}
type leaf struct {
	key  [32]byte
	hash [32]byte
}

var empty [257][32]byte

func init() {
	empty[256] = sha256.Sum256([]byte{2})
	for d := 255; d >= 0; d-- {
		empty[d] = parent(empty[d+1], empty[d+1])
	}
}
func parent(l, r [32]byte) [32]byte {
	b := append([]byte{1}, l[:]...)
	b = append(b, r[:]...)
	return sha256.Sum256(b)
}
func Root(entries []Entry) string {
	leaves := make([]leaf, len(entries))
	for i, e := range entries {
		kb, _ := hex.DecodeString(codec.Hash("state/key", []any{e.Table, e.Key}))
		var k [32]byte
		copy(k[:], kb)
		vh := sha256.Sum256(codec.Must(e.Value))
		b := append([]byte{0}, k[:]...)
		b = append(b, vh[:]...)
		leaves[i] = leaf{k, sha256.Sum256(b)}
	}
	sort.Slice(leaves, func(i, j int) bool { return bytes.Compare(leaves[i].key[:], leaves[j].key[:]) < 0 })
	for i := 1; i < len(leaves); i++ {
		if leaves[i].key == leaves[i-1].key {
			panic("duplicate state key")
		}
	}
	var walk func([]leaf, int) [32]byte
	walk = func(ls []leaf, d int) [32]byte {
		if len(ls) == 0 {
			return empty[d]
		}
		if d == 256 {
			return ls[0].hash
		}
		split := sort.Search(len(ls), func(i int) bool { return ls[i].key[d/8]&(1<<uint(7-d%8)) != 0 })
		return parent(walk(ls[:split], d+1), walk(ls[split:], d+1))
	}
	r := walk(leaves, 0)
	return hex.EncodeToString(r[:])
}
