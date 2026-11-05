package network

import (
	"bytes"
	"civora/internal/codec"
	"encoding/json"
	"github.com/cometbft/cometbft/libs/log"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func genesis(t *testing.T) Genesis {
	t.Helper()
	raw, e := os.ReadFile("../../spec/fixtures/accounts.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Genesis json.RawMessage `json:"genesis"`
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	return Genesis{Kind: "ledger", Data: f.Genesis}
}
func TestNeverReinitializeOrRecoverMissingSignerState(t *testing.T) {
	g := genesis(t)
	chain, _ := ApplicationChain(g)
	dir := filepath.Join(t.TempDir(), "network")
	if e := Init(dir, chain, g, 27650); e != nil {
		t.Fatal(e)
	}
	home := filepath.Join(dir, "node0")
	state := filepath.Join(home, "data", "priv_validator_state.json")
	before, _ := os.ReadFile(state)
	if e := Init(dir, chain, g, 27650); e == nil {
		t.Fatal("reinitialized signer")
	}
	after, _ := os.ReadFile(state)
	if !bytes.Equal(before, after) {
		t.Fatal("sign state overwritten")
	}
	if e := os.Remove(state); e != nil {
		t.Fatal(e)
	}
	_, _, e := Start(home, 2, log.NewNopLogger())
	if e == nil || !strings.Contains(e.Error(), "missing signer") {
		t.Fatal("missing last-sign state not refused", e)
	}
	if _, e = os.Stat(state); !os.IsNotExist(e) {
		t.Fatal("silently regenerated last-sign state")
	}
}
func TestGenesisPinAndSingleWriterLock(t *testing.T) {
	g := genesis(t)
	chain, _ := ApplicationChain(g)
	dir := filepath.Join(t.TempDir(), "network")
	if e := Init(dir, chain, g, 27650); e != nil {
		t.Fatal(e)
	}
	home := filepath.Join(dir, "node0")
	lock, e := bolt.Open(filepath.Join(home, "data", "application.lock"), 0600, nil)
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = Start(home, 2, log.NewNopLogger())
	if e == nil {
		t.Fatal("second writer accepted")
	}
	_ = lock.Close()
	path := filepath.Join(home, "config", "genesis.json")
	raw, _ := os.ReadFile(path)
	raw = append(raw, '\n')
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	_, _, e = Start(home, 2, log.NewNopLogger())
	if e == nil || !strings.Contains(e.Error(), "fingerprint") {
		t.Fatal("different exact genesis accepted", e)
	}
	if _, e := Machine(Genesis{Kind: "ledger", Data: codec.Must(map[string]any{"unknown": true})}, 2); e == nil {
		t.Fatal("unknown genesis accepted")
	}
}
func TestFutureSignerIsPreinitializedAndNeverReset(t *testing.T) {
	raw, e := os.ReadFile("../../spec/fixtures/v0.5/rotation.app.json")
	if e != nil {
		t.Fatal(e)
	}
	var g Genesis
	if e = codec.Decode(raw, "network_genesis", &g); e != nil {
		t.Fatal(e)
	}
	chain, _ := ApplicationChain(g)
	dir := filepath.Join(t.TempDir(), "network")
	if e = Init(dir, chain, g, 27650); e != nil {
		t.Fatal(e)
	}
	home := filepath.Join(dir, "node6")
	key, state := generationPaths(home, g.Rotations[0].Keys[6])
	if _, e = os.Stat(key); e != nil {
		t.Fatal("future key missing", e)
	}
	if e = os.Remove(state); e != nil {
		t.Fatal(e)
	}
	_, _, e = StartWithSigner(home, 2, 1, log.NewNopLogger())
	if e == nil || !strings.Contains(e.Error(), "missing signer") {
		t.Fatal("future signer state silently restored", e)
	}
	if _, e = os.Stat(state); !os.IsNotExist(e) {
		t.Fatal("future last-sign state regenerated")
	}
	if _, e = expectedSigner(g, chain, 6, 2); e == nil {
		t.Fatal("unplanned key generation")
	}
	repeated := g
	plan := g.Rotations[0]
	plan.Height = "8"
	plan.Keys = append([]string{}, Trust(chain).Keys...)
	repeated.Rotations = append(repeated.Rotations, plan)
	keys, e := generationKeys(repeated, chain, 6)
	if e != nil || len(keys) != 2 {
		t.Fatal("key reactivation created another last-sign state", e)
	}
}
