package core_test

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/testutil"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func replay(t *testing.T, f testutil.Fixture, supported uint64) *core.Engine {
	t.Helper()
	e, err := core.New(codec.Must(f.Genesis), supported)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range f.Blocks {
		next, events, err := e.Apply(codec.Must(b))
		if err != nil {
			t.Fatal(err)
		}
		e = next
		if e.Root() != f.ExpectedRoots[i] {
			t.Fatalf("cross-language root mismatch at %s: %s", b.Height, e.Root())
		}
		if !bytes.Equal(codec.Must(events), codec.Must(f.ExpectedEvents[i])) {
			t.Fatal("events mismatch")
		}
		if err = codec.Validate("state", mustParse(t, codec.Must(e.Snapshot()))); err != nil {
			t.Fatal(err)
		}
	}
	return e
}
func mustParse(t *testing.T, b []byte) any {
	t.Helper()
	v, e := codec.Parse(b)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestDeterministicReplay(t *testing.T) {
	for _, name := range []string{"accounts", "activation"} {
		t.Run(name, func(t *testing.T) {
			f := testutil.Load(t, name)
			a, b := replay(t, f, 2), replay(t, f, 2)
			if !bytes.Equal(codec.Must(a.Snapshot()), codec.Must(b.Snapshot())) {
				t.Fatal("nondeterministic state")
			}
			for _, account := range a.Accounts {
				if account.Status != "retired" || account.NextNonce != "3" || len(account.UsedCommitments) != 2 {
					t.Fatal("incorrect account lifecycle")
				}
			}
		})
	}
}
func TestNegativeFixturesAreAtomic(t *testing.T) {
	files, e := filepath.Glob(filepath.Join(testutil.Root(), "spec", "fixtures", "reject-*.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		t.Run(name, func(t *testing.T) {
			var f struct {
				Name     string       `json:"name"`
				Genesis  core.Config  `json:"genesis"`
				Prefix   []core.Block `json:"prefix_blocks"`
				Rejected core.Block   `json:"rejected_block"`
				Error    string       `json:"expected_error"`
			}
			if e := codec.Decode(testutil.Read(t, name), "negative", &f); e != nil {
				t.Fatal(e)
			}
			engine, e := core.New(codec.Must(f.Genesis), 2)
			if e != nil {
				t.Fatal(e)
			}
			for _, b := range f.Prefix {
				engine, _, e = engine.Apply(codec.Must(b))
				if e != nil {
					t.Fatal(e)
				}
			}
			before := codec.Must(engine.Snapshot())
			_, _, e = engine.Apply(codec.Must(f.Rejected))
			if e == nil || !strings.Contains(e.Error(), f.Error) {
				t.Fatalf("want %s got %v", f.Error, e)
			}
			if !bytes.Equal(before, codec.Must(engine.Snapshot())) {
				t.Fatal("failed block mutated state")
			}
		})
	}
}
func TestOldNodeStopsBeforeActivation(t *testing.T) {
	f := testutil.Load(t, "activation")
	e, err := core.New(codec.Must(f.Genesis), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range f.Blocks[:2] {
		e, _, err = e.Apply(codec.Must(b))
		if err != nil {
			t.Fatal(err)
		}
	}
	before := codec.Must(e.Snapshot())
	_, _, err = e.Apply(codec.Must(f.Blocks[2]))
	if err == nil || !strings.Contains(err.Error(), "ERR_UNSUPPORTED_EPOCH") {
		t.Fatal(err)
	}
	if !bytes.Equal(before, codec.Must(e.Snapshot())) {
		t.Fatal("incompatible node changed state")
	}
}
func TestCheckTxDoesNotReserveNonce(t *testing.T) {
	f := testutil.Load(t, "accounts")
	e, err := core.New(codec.Must(f.Genesis), 1)
	if err != nil {
		t.Fatal(err)
	}
	e, _, err = e.Apply(codec.Must(f.Blocks[0]))
	if err != nil {
		t.Fatal(err)
	}
	tx := codec.Must(f.Blocks[1].Transactions[0])
	before := e.Root()
	for range 2 {
		if err = e.CheckTx(tx); err != nil {
			t.Fatal(err)
		}
	}
	if e.Root() != before {
		t.Fatal("check mutated state")
	}
	e, _, err = e.Apply(codec.Must(f.Blocks[1]))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.CheckTx(tx); err == nil {
		t.Fatal("nonce replay accepted")
	}
}
func resign(t *testing.T, tx core.Transaction, seed string) core.Transaction {
	t.Helper()
	s, _ := hex.DecodeString(seed)
	key := ed25519.NewKeyFromSeed(s)
	digest, _ := hex.DecodeString(core.TxID(tx.Body))
	tx.Auth.Signature = hex.EncodeToString(ed25519.Sign(key, digest))
	return tx
}
func TestCommitmentAndKeyReservations(t *testing.T) {
	f := testutil.Load(t, "accounts")
	engine := replay(t, f, 1)
	// A fresh account signed by its own distinct key cannot reclaim a retired
	// commitment or either role of a key reserved by the retired account.
	seed := strings.Repeat("07", 32)
	s, _ := hex.DecodeString(seed)
	pk := hex.EncodeToString(ed25519.NewKeyFromSeed(s).Public().(ed25519.PublicKey))
	tx := f.Blocks[0].Transactions[0]
	var p core.Payload
	_ = json.Unmarshal(tx.Body.Payload, &p)
	p.SigningPK = pk
	p.RecoveryPK = strings.Repeat("08", 32)
	tx.Body.ActorID = codec.Hash("account", []any{f.Genesis.NetworkID, pk})
	tx.Body.Payload = codec.Must(p)
	tx = resign(t, tx, seed)
	b := core.Block{Height: "5", TimeMS: "5000", Transactions: []core.Transaction{tx}}
	if _, _, e := engine.Apply(codec.Must(b)); e == nil || !strings.Contains(e.Error(), "ERR_COMMITMENT_USED") {
		t.Fatal(e)
	}
	p.Commitment = "7"
	var original core.Payload
	_ = json.Unmarshal(f.Blocks[0].Transactions[0].Body.Payload, &original)
	p.RecoveryPK = original.SigningPK
	tx.Body.Payload = codec.Must(p)
	tx = resign(t, tx, seed)
	b.Transactions[0] = tx
	if _, _, e := engine.Apply(codec.Must(b)); e == nil || !strings.Contains(e.Error(), "ERR_KEY_IN_USE") {
		t.Fatal(e)
	}
}
func TestTimeIsMonotoneAndCommitted(t *testing.T) {
	f := testutil.Load(t, "accounts")
	e, err := core.New(codec.Must(f.Genesis), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range f.Blocks[:3] {
		e, _, err = e.Apply(codec.Must(b))
		if err != nil {
			t.Fatal(err)
		}
	}
	if e.Time != 2000 {
		t.Fatal(e.Time)
	}
	a := e.Clone()
	a.Time++
	if a.Root() == e.Root() {
		t.Fatal("BFT time omitted from state commitment")
	}
}
