package state

import "testing"

func TestPresenceAbsenceAndPublicSiblingOrder(t *testing.T) {
	entries := []Entry{{"table", "a", "present"}, {"table", "nil", nil}, {"table", []any{"x", "y"}, true}}
	for _, key := range []any{"a", "missing", "nil", []any{"x", "y"}} {
		p, e := WitnessFor(entries, "table", key)
		if e != nil {
			t.Fatal(e)
		}
		if e = VerifyWitness(Root(entries), p); e != nil {
			t.Fatal(e)
		}
		if key == "missing" && p.Exists {
			t.Fatal("absence claimed present")
		}
		p.Exists = !p.Exists
		if VerifyWitness(Root(entries), p) == nil {
			t.Fatal("presence bit not authenticated")
		}
	}
	p, _ := WitnessFor(nil, "empty", "absent")
	if e := VerifyWitness(Root(nil), p); e != nil {
		t.Fatal(e)
	}
	p.Entry.Value = "injected"
	if VerifyWitness(Root(nil), p) == nil {
		t.Fatal("absent value injected")
	}
}
