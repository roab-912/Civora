package electionguard_test

import (
	"civora/internal/codec"
	"civora/internal/electionguard"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRejectCryptographicAttacks(t *testing.T) {
	f := loadPublic(t)[0]
	v, e := electionguard.NewVerifier()
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := codec.Blob(f.Ceremony)
	ceremony, e := v.Ceremony(f.Context.ports(), raw)
	if e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile("../../spec/fixtures/v0.4/reject-crypto.json")
	if e != nil {
		t.Fatal(e)
	}
	var negatives struct {
		Cases []struct{ Name, Stage, Encoded, Aggregate string } `json:"cases"`
	}
	if e = json.Unmarshal(data, &negatives); e != nil {
		t.Fatal(e)
	}
	for _, n := range negatives.Cases {
		t.Run(n.Name, func(t *testing.T) {
			var err error
			switch n.Stage {
			case "ceremony":
				b, e := codec.Blob(n.Encoded)
				err = e
				if err == nil {
					_, err = v.Ceremony(f.Context.ports(), b)
				}
			case "ballot":
				d, e := electionguard.Decode(n.Encoded, electionguard.Ballot)
				err = e
				if err == nil {
					_, err = v.Ballot(ceremony, f.Context.Poll, d.Files["ballot.json"])
				}
			case "tally":
				d, e := electionguard.Decode(n.Encoded, electionguard.Tally)
				err = e
				if err == nil {
					aggregate := f.Aggregate
					if n.Aggregate != "" {
						aggregate = n.Aggregate
					}
					_, _, err = v.Tally(ceremony, f.Context.Poll, aggregate, f.Count, d.Files["tally.json"])
				}
			default:
				t.Fatal("unknown attack stage")
			}
			if err == nil {
				t.Fatal("invalid cryptographic record accepted")
			}
		})
	}
}

func TestScaleIndependenceAndFrozenRouting(t *testing.T) {
	f := loadPublic(t)[0]
	v, e := electionguard.NewVerifier()
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := codec.Blob(f.Ceremony)
	ceremony, e := v.Ceremony(f.Context.ports(), raw)
	if e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile("../../spec/fixtures/v0.4/scale.json")
	if e != nil {
		t.Fatal(e)
	}
	var vector struct {
		Context electionguard.ScaleContext  `json:"context"`
		Valid   electionguard.ScaleEnvelope `json:"valid"`
		Copy    electionguard.ScaleEnvelope `json:"copy_attack"`
	}
	if e = json.Unmarshal(data, &vector); e != nil {
		t.Fatal(e)
	}
	if _, e = v.VerifyScale(ceremony, vector.Context, codec.Must(vector.Valid)); e != nil {
		t.Fatal(e)
	}
	if _, e = v.VerifyScale(ceremony, vector.Context, codec.Must(vector.Copy)); e == nil || !strings.Contains(e.Error(), "ERR_SCALE_INDEPENDENCE") {
		t.Fatal("copied ciphertext with valid second-identity membership was accepted")
	}
	for _, change := range []func(*electionguard.ScaleEnvelope){func(e *electionguard.ScaleEnvelope) { e.Lane = "999" }, func(e *electionguard.ScaleEnvelope) { e.Checkpoint = "ff" + e.Checkpoint[2:] }, func(e *electionguard.ScaleEnvelope) { e.Epoch = "2" }, func(e *electionguard.ScaleEnvelope) { e.NetworkID = "ff" + e.NetworkID[2:] }, func(e *electionguard.ScaleEnvelope) { e.PollID = "00" + e.PollID[2:] }} {
		candidate := vector.Valid
		change(&candidate)
		if _, e = v.VerifyScale(ceremony, vector.Context, codec.Must(candidate)); e == nil {
			t.Fatal("foreign route/checkpoint/context accepted")
		}
	}
	for _, partitions := range []uint64{1, 2, 4, 32, 256} {
		if _, e = electionguard.Lane(vector.Context.NetworkID, vector.Context.PollID, vector.Valid.Membership.Nullifier, partitions); e != nil {
			t.Fatal(e)
		}
	}
	for _, partitions := range []uint64{0, 3, 257} {
		if _, e = electionguard.Lane(vector.Context.NetworkID, vector.Context.PollID, vector.Valid.Membership.Nullifier, partitions); e == nil {
			t.Fatal("invalid partition count")
		}
	}
}
