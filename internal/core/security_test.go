package core_test

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/recovery"
	"civora/internal/recoverytest"
	"civora/internal/testutil"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSecurityHoldPrecedesTransactionsAndReplaysAcrossLanguages(t *testing.T) {
	f := testutil.Load(t, "accounts")
	f.Genesis.Recovery = recoverytest.Config(t, f.Genesis.NetworkID, "inline_dev")
	e, err := core.New(codec.Must(f.Genesis), 2)
	if err != nil {
		t.Fatal(err)
	}
	hold := recoverytest.Hold(t, f.Genesis.Recovery, "module", "dev.account.ed25519.v1", "1", 1000, 2000)
	initial := e.Root()
	b := f.Blocks[0]
	b.SecurityOrders = []recovery.Bundle{hold}
	if _, _, err = e.Apply(codec.Must(b)); err == nil {
		t.Fatal("held account operation admitted")
	}
	if e.Root() != initial {
		t.Fatal("rejected block changed durable state")
	}
	b.Transactions = []core.Transaction{}
	next, events, err := e.Apply(codec.Must(b))
	if err != nil {
		t.Fatal(err)
	}
	if next.Security.Counter != "1" || len(events) != 1 || events[0].Type != "security.hold" {
		t.Fatal(events)
	}
	b2 := core.Block{Height: "2", TimeMS: "3000", Transactions: []core.Transaction{}}
	final, events2, err := next.Apply(codec.Must(b2))
	if err != nil {
		t.Fatal(err)
	}
	if err = final.CheckTx(codec.Must(f.Blocks[0].Transactions[0])); err == nil {
		t.Fatal("expired pause automatically restored module trust")
	}
	b2.SecurityOrders = []recovery.Bundle{hold}
	if _, _, err = next.Apply(codec.Must(b2)); err == nil {
		t.Fatal("replayed hold accepted")
	}
	// A deterministic public fixture pins the complete roots and events for the
	// independent Node/OpenSSL oracle, including post-expiry safe refusal.
	fixture := map[string]any{"mode": "PUBLIC_TEST_ONLY", "genesis": f.Genesis, "blocks": []core.Block{b, {Height: "2", TimeMS: "3000", Transactions: []core.Transaction{}}}, "expected_roots": []string{next.Root(), final.Root()}, "expected_events": [][]core.Event{events, events2}, "rejected_transaction": f.Blocks[0].Transactions[0]}
	path := filepath.Join(testutil.Root(), "spec", "fixtures", "v0.9", "onchain-hold.TEST_ONLY.json")
	raw := codec.Must(fixture)
	if os.Getenv("CIVORA_WRITE_SAFETY_FIXTURES") == "1" {
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("on-chain fixture differs", err)
	}
	legacy, _ := core.New(codec.Must(testutil.Load(t, "accounts").Genesis), 2)
	if _, _, err = legacy.Apply(codec.Must(b)); err == nil {
		t.Fatal("root added by an incoming order")
	}
	if err = codec.Decode(codec.Must(next.Snapshot()), "state", new(core.State)); err != nil {
		t.Fatal(err)
	}
}
func TestSecurityNativeFixture(t *testing.T) {
	path := os.Getenv("CIVORA_NATIVE_SAFETY_OUTPUT")
	if path == "" {
		t.Skip("explicit PUBLIC_TEST_ONLY native exercise")
	}
	f := testutil.Load(t, "accounts")
	f.Genesis.Recovery = recoverytest.Config(t, f.Genesis.NetworkID, "inline_dev")
	now := uint64(time.Now().UnixMilli())
	hold := recoverytest.Hold(t, f.Genesis.Recovery, "module", "dev.account.ed25519.v1", "1", now-60000, now+600000)
	value := map[string]any{"mode": "PUBLIC_TEST_ONLY", "app": map[string]any{"kind": "ledger", "data": f.Genesis}, "hold": hold, "transactions": f.Blocks}
	if err := os.WriteFile(path, codec.Must(value), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestSecurityInterruptsPendingEffectAtItsDeadline(t *testing.T) {
	var source struct {
		Genesis core.Config  `json:"genesis"`
		Blocks  []core.Block `json:"blocks"`
	}
	if err := json.Unmarshal(testutil.Read(t, "v0.4/replay-mixed"), &source); err != nil {
		t.Fatal(err)
	}
	cfg := source.Genesis
	cfg.Recovery = recoverytest.Config(t, cfg.NetworkID, cfg.VotingProfile)
	e, err := core.New(codec.Must(cfg), 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range source.Blocks[:8] {
		e, _, err = e.Apply(codec.Must(b))
		if err != nil {
			t.Fatal(err)
		}
	}
	var id, pollID string
	for key, p := range e.Governance.Proposals {
		if p.State == "adopted" {
			id = key
			pollID = *p.PollID
		}
	}
	if id == "" {
		t.Fatal("real encrypted fixture did not adopt a decision")
	}
	now, _ := codec.Uint(source.Blocks[8].TimeMS)
	hold := recoverytest.Hold(t, cfg.Recovery, "effect", id, "1", now, now+1000)
	block := source.Blocks[8]
	block.SecurityOrders = []recovery.Bundle{hold}
	n, _, err := e.Apply(codec.Must(block))
	if err != nil {
		t.Fatal(err)
	}
	if n.Governance.Proposals[id].State != "interrupted" || len(n.Governance.Objects) != 0 || n.Governance.Polls[pollID].Result.Yes != "3" {
		t.Fatal("effect escaped or evidence lost")
	}
	n, _, err = n.Apply(codec.Must(core.Block{Height: "10", TimeMS: recovery.Number(now + 2000), Transactions: []core.Transaction{}}))
	if err != nil || n.Governance.Proposals[id].State != "interrupted" {
		t.Fatal("automatic poll resumption", err)
	}
}
