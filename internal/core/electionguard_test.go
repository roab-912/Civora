package core_test

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// A syntactically valid envelope or a real eligibility proof must never enable
// the unconfigured encryption adapter. Exercise actual ledger replay up to the
// first ceremony instead of fabricating a partially initialized poll in memory.
func TestUnqualifiedElectionGuardCannotOpenPoll(t *testing.T) {
	raw, err := os.ReadFile("../../spec/fixtures/v0.2/governance.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Genesis core.Config  `json:"genesis"`
		Blocks  []core.Block `json:"blocks"`
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"eg-2.0-civic-1", "eg-2.1-civic-1"} {
		f.Genesis.Governance.CryptoProfile = profile
		if _, err := core.New(codec.Must(f.Genesis), 1); err == nil {
			t.Fatal("unqualified profile enabled", profile)
		}
	}
	f.Genesis.Governance.CryptoProfile = "unconfigured"
	var stoppedHash string
	for run := 0; run < 2; run++ {
		engine, err := core.New(codec.Must(f.Genesis), 1)
		if err != nil {
			t.Fatal(err)
		}
		stopped := false
		for _, block := range f.Blocks {
			before := codec.Must(engine.Snapshot())
			next, _, err := engine.Apply(codec.Must(block))
			if err == nil {
				engine = next
				continue
			}
			if !strings.Contains(err.Error(), "ERR_VOTING_ADAPTER") {
				t.Fatal(err)
			}
			if !bytes.Equal(before, codec.Must(engine.Snapshot())) {
				t.Fatal("rejected ceremony modified state")
			}
			if run == 0 {
				stoppedHash = engine.Root()
			} else if engine.Root() != stoppedHash {
				t.Fatal("fail-closed replay diverged")
			}
			stopped = true
			break
		}
		if !stopped {
			t.Fatal("unconfigured adapter accepted ceremony")
		}
	}
}
