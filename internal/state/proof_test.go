package state

import (
	"civora/internal/codec"
	"testing"
)

func TestMembershipProof(t *testing.T) {
	es := []Entry{{"accounts", "a", map[string]any{"active": true}}, {"accounts", "b", "other"}, {"polls", []any{"x", "y"}, "frozen"}}
	for _, e := range es {
		p, err := Prove(es, e.Table, e.Key)
		if err != nil {
			t.Fatal(err)
		}
		if err = Verify(Root(es), p); err != nil {
			t.Fatal(err)
		}
		p.Entry.Value = "tampered"
		if Verify(Root(es), p) == nil {
			t.Fatal("accepted foreign value")
		}
	}
	if _, e := Prove(es, "missing", "x"); e == nil {
		t.Fatal("absent")
	}
	p, _ := Prove(es, "accounts", "a")
	p.Siblings[0] = codec.Hash("attack", nil)
	if Verify(Root(es), p) == nil {
		t.Fatal("sibling tamper")
	}
}
