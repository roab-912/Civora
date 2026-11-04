package eligibilityindex

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/eligibility"
	"civora/internal/state/store"
	"civora/internal/testutil"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func TestLedgerProjectionResumeAndSameBlockRotation(t *testing.T) {
	for _, sameBlock := range []bool{false, true} {
		t.Run(fmt.Sprint(sameBlock), func(t *testing.T) {
			f := testutil.WithEligibility(testutil.Load(t, "accounts"), "16")
			ledger, e := store.Open(filepath.Join(t.TempDir(), "ledger"), codec.Must(f.Genesis), 1)
			if e != nil {
				t.Fatal(e)
			}
			if sameBlock {
				transactions := []core.Transaction{f.Blocks[0].Transactions[0], f.Blocks[1].Transactions[0], f.Blocks[3].Transactions[0]}
				if e = ledger.Commit(codec.Must(core.Block{Height: "1", TimeMS: "1000", Transactions: transactions})); e != nil {
					t.Fatal(e)
				}
			} else {
				for _, b := range f.Blocks {
					if e = ledger.Commit(codec.Must(b)); e != nil {
						t.Fatal(e)
					}
				}
			}
			dir := t.TempDir()
			index, e := Sync(dir, ledger)
			if e != nil {
				t.Fatal(e)
			}
			want, e := Metadata(ledger.Snapshot())
			if e != nil {
				t.Fatal(e)
			}
			got, e := index.Latest()
			if e != nil || got.SnapshotID != want.SnapshotID {
				t.Fatal("ledger root differs", e)
			}
			_ = index.Close()
			index, e = Sync(dir, ledger)
			if e != nil {
				t.Fatal(e)
			}
			defer index.Close()
			if !sameBlock {
				if _, e = index.Path(2, 1); e != nil {
					t.Fatal(e)
				}
				if _, e = index.Path(4, 1); e == nil {
					t.Fatal("retired leaf returned")
				}
			}
		})
	}
}
func TestExpectedRootMismatchRollsBack(t *testing.T) {
	tree, _ := eligibility.New(16)
	meta := tree.Metadata(codec.Hash("test/network", "root-check"), "1", "accounts", "semaphore-v4.14.5-depth-16", "0")
	s, e := eligibility.OpenStore(t.TempDir(), meta)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	expected := meta
	expected.Height = "1"
	expected.SnapshotID = expected.ID()
	if _, e = s.CommitExpected(1, []eligibility.Change{{Operation: "append", Index: "0", Commitment: "5"}}, &expected); e == nil {
		t.Fatal("mismatching root published")
	}
	latest, e := s.Latest()
	if e != nil || latest.Height != "0" {
		t.Fatal("failed transaction advanced cursor", e)
	}
}
func TestPublicAccountFixtureReplay(t *testing.T) {
	var f testutil.Fixture
	if e := codec.Decode(testutil.Read(t, "v0.3/accounts"), "fixture", &f); e != nil {
		t.Fatal(e)
	}
	var top map[string]json.RawMessage
	if e := json.Unmarshal(testutil.Read(t, "v0.3/accounts"), &top); e != nil || len(top["expected_state"]) == 0 {
		t.Fatal("final state missing")
	}
	engine, e := core.New(codec.Must(f.Genesis), 1)
	if e != nil {
		t.Fatal(e)
	}
	for i, b := range f.Blocks {
		engine, _, e = engine.Apply(codec.Must(b))
		if e != nil {
			t.Fatal(e)
		}
		if engine.Root() != f.ExpectedRoots[i] {
			t.Fatal("fixture root differs")
		}
	}
	if !bytes.Equal(codec.Must(engine.Snapshot()), top["expected_state"]) {
		t.Fatal("fixture final state differs")
	}
}
