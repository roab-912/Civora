package electionguard_test

import (
	"civora/internal/codec"
	"civora/internal/electionguard"
	"civora/internal/ports"
	"encoding/json"
	"os"
	"testing"
)

type publicContext struct {
	Network   string   `json:"network_id"`
	Poll      string   `json:"poll_id"`
	Proposal  string   `json:"proposal_id"`
	Epoch     string   `json:"protocol_epoch"`
	Guardians []string `json:"guardian_ids"`
}

func (c publicContext) ports() ports.PollContext {
	return ports.PollContext{NetworkID: c.Network, PollID: c.Poll, ProposalID: c.Proposal, Epoch: c.Epoch, GuardianIDs: c.Guardians}
}

type publicCase struct {
	Label     string        `json:"label"`
	Context   publicContext `json:"context"`
	Ceremony  string        `json:"ceremony_bundle"`
	Ballots   []string      `json:"ballots"`
	Aggregate string        `json:"aggregate"`
	Count     string        `json:"count"`
	Tally     string        `json:"tally_bundle"`
	Yes       string        `json:"yes"`
	No        string        `json:"no"`
}

func loadPublic(t *testing.T) []publicCase {
	t.Helper()
	raw, e := os.ReadFile("../../spec/fixtures/v0.4/crypto.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Cases []publicCase `json:"cases"`
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	return f.Cases
}
func TestIndependentSDKPublicVerification(t *testing.T) {
	v, e := electionguard.NewVerifier()
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range loadPublic(t) {
		t.Run(f.Label, func(t *testing.T) {
			raw, e := codec.Blob(f.Ceremony)
			if e != nil {
				t.Fatal(e)
			}
			c, e := v.Ceremony(f.Context.ports(), raw)
			if e != nil {
				t.Fatal(e)
			}
			aggregate := v.Zero()
			for _, encoded := range f.Ballots {
				d, e := electionguard.Decode(encoded, electionguard.Ballot)
				if e != nil {
					t.Fatal(e)
				}
				b, e := v.Ballot(c, f.Context.Poll, d.Files["ballot.json"])
				if e != nil {
					t.Fatal(e)
				}
				aggregate, e = v.Add(aggregate, b)
				if e != nil {
					t.Fatal(e)
				}
			}
			if aggregate != f.Aggregate {
				t.Fatal("homomorphic aggregate differs from SDK")
			}
			d, e := electionguard.Decode(f.Tally, electionguard.Tally)
			if e != nil {
				t.Fatal(e)
			}
			yes, no, e := v.Tally(c, f.Context.Poll, aggregate, f.Count, d.Files["tally.json"])
			if e != nil {
				t.Fatal(e)
			}
			if yes != f.Yes || no != f.No {
				t.Fatal("global result differs from SDK")
			}
		})
	}
}
