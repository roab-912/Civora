package state

import (
	"civora/internal/codec"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestSingleLeafPathAndOrder(t *testing.T) {
	e := Entry{"accounts", "a", map[string]any{"value": "é"}}
	k, _ := hex.DecodeString(codec.Hash("state/key", []any{e.Table, e.Key}))
	vh := sha256.Sum256(codec.Must(e.Value))
	raw := append([]byte{0}, k...)
	raw = append(raw, vh[:]...)
	h := sha256.Sum256(raw)
	for d := 255; d >= 0; d-- {
		if k[d/8]&(1<<uint(7-d%8)) == 0 {
			h = parent(h, empty[d+1])
		} else {
			h = parent(empty[d+1], h)
		}
	}
	if Root([]Entry{e}) != hex.EncodeToString(h[:]) {
		t.Fatal("wrong bit direction/domain")
	}
	b := Entry{"accounts", "b", map[string]any{"value": "b"}}
	if Root([]Entry{e, b}) != Root([]Entry{b, e}) {
		t.Fatal("insertion order changed root")
	}
	if Root([]Entry{e}) == Root([]Entry{b}) {
		t.Fatal("key not committed")
	}
}
