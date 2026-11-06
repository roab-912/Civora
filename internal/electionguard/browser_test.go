package electionguard_test

import (
	"civora/internal/codec"
	"civora/internal/electionguard"
	"encoding/json"
	"os"
	"testing"
)

func TestBrowserGeneratedBinaryBallotsVerifyInGo(t *testing.T) {
	raw, e := os.ReadFile("../../spec/fixtures/v0.7/browser-crypto.json")
	if e != nil {
		t.Fatal(e)
	}
	var f publicCase
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	if len(f.Ballots) != 2 {
		t.Fatal("Yes and No fixtures required")
	}
	v, e := electionguard.NewVerifier()
	if e != nil {
		t.Fatal(e)
	}
	ceremony, e := electionguard.Decode(f.Ceremony, electionguard.Ceremony)
	if e != nil {
		t.Fatal(e)
	}
	el, e := v.Ceremony(f.Context.ports(), ceremony.Raw)
	if e != nil {
		t.Fatal(e)
	}
	for _, encoded := range f.Ballots {
		b, e := electionguard.Decode(encoded, electionguard.Ballot)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = v.Ballot(el, f.Context.Poll, b.Files["ballot.json"]); e != nil {
			t.Fatal("browser proof rejected", e)
		}
		value, _ := codec.Parse(b.Files["ballot.json"])
		ballot := value.(map[string]any)
		ballot["election_id"] = "forged"
		if _, e = v.Ballot(el, f.Context.Poll, codec.Must(ballot)); e == nil {
			t.Fatal("foreign context accepted")
		}
	}
}
