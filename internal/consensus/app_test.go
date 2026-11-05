package consensus

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/replica"
	"context"
	"encoding/json"
	abci "github.com/cometbft/cometbft/abci/types"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type fixture struct {
	Genesis json.RawMessage `json:"genesis"`
	Blocks  []core.Block    `json:"blocks"`
	Roots   []string        `json:"expected_roots"`
}

func load(t *testing.T) fixture {
	t.Helper()
	b, e := os.ReadFile("../../spec/fixtures/accounts.json")
	if e != nil {
		t.Fatal(e)
	}
	var f fixture
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	return f
}
func open(t *testing.T, dir string, f fixture) (*Application, *replica.Journal) {
	t.Helper()
	j, e := replica.Open(dir, f.Genesis, func() (replica.Machine, error) {
		engine, e := core.New(f.Genesis, 2)
		return &Ledger{Engine: engine}, e
	})
	if e != nil {
		t.Fatal(e)
	}
	return New(j, "test", f.Genesis), j
}
func req(b core.Block) *abci.RequestFinalizeBlock {
	h, _ := codec.Uint(b.Height)
	tm, _ := codec.Uint(b.TimeMS)
	txs := [][]byte{}
	for _, tx := range b.Transactions {
		txs = append(txs, codec.Must(tx))
	}
	return &abci.RequestFinalizeBlock{Height: int64(h), Time: time.UnixMilli(int64(tm)).UTC(), Hash: bytes.Repeat([]byte{byte(h)}, 32), Txs: txs}
}
func finalize(t *testing.T, a *Application, b core.Block) {
	t.Helper()
	if _, e := a.FinalizeBlock(context.Background(), req(b)); e != nil {
		t.Fatal(e)
	}
}
func commit(t *testing.T, a *Application) {
	t.Helper()
	if _, e := a.Commit(context.Background(), &abci.RequestCommit{}); e != nil {
		t.Fatal(e)
	}
}
func TestABCIIsolationSelectionAndReplay(t *testing.T) {
	ctx := context.Background()
	f := load(t)
	dir := t.TempDir()
	a, j := open(t, dir, f)
	initial := j.Machine.Root()
	r := req(f.Blocks[0])
	bad := []byte(`{}`)
	out, e := a.PrepareProposal(ctx, &abci.RequestPrepareProposal{Height: 1, Time: r.Time, MaxTxBytes: 2 << 20, Txs: [][]byte{bad, r.Txs[0], r.Txs[0]}})
	if e != nil || len(out.Txs) != 1 {
		t.Fatalf("prepare %v %v", out, e)
	}
	rejected, e := a.ProcessProposal(ctx, &abci.RequestProcessProposal{Height: 1, Time: r.Time, Txs: [][]byte{r.Txs[0], r.Txs[0]}})
	if e != nil || rejected.Status != abci.ResponseProcessProposal_REJECT {
		t.Fatal("invalid proposal accepted")
	}
	finalize(t, a, f.Blocks[0])
	info, _ := a.Info(ctx, &abci.RequestInfo{})
	if info.LastBlockHeight != 0 || j.Machine.Root() != initial {
		t.Fatal("speculative state exposed")
	}
	q, _ := a.Query(ctx, &abci.RequestQuery{Path: "/state"})
	if !bytes.Equal(q.Value, j.Machine.State()) {
		t.Fatal("speculative query")
	}
	// Discard a finalized but uncommitted candidate, as on process death.
	a, j = open(t, dir, f)
	if j.Machine.Height() != 0 {
		t.Fatal("uncommitted state durable")
	}
	finalize(t, a, f.Blocks[0])
	commit(t, a)
	for i, b := range f.Blocks {
		if i > 0 {
			finalize(t, a, b)
			commit(t, a)
		}
		if j.Machine.Root() != f.Roots[i] {
			t.Fatalf("root %d", i)
		}
		a, j = open(t, dir, f)
		finalize(t, a, b)
		commit(t, a)
		if j.Machine.Root() != f.Roots[i] {
			t.Fatal("replay changed state")
		}
	}
	conflict := req(f.Blocks[3])
	conflict.Time = conflict.Time.Add(time.Nanosecond)
	if _, e = a.FinalizeBlock(ctx, conflict); e == nil {
		t.Fatal("different header with same millis accepted")
	}
	if _, e = a.Commit(ctx, &abci.RequestCommit{}); e == nil {
		t.Fatal("commit without finalization")
	}
}
func TestCrashChild(t *testing.T) {
	dir := os.Getenv("CIVORA_REPLICA_CRASH_DIR")
	if dir == "" {
		return
	}
	f := load(t)
	a, j := open(t, dir, f)
	finalize(t, a, f.Blocks[0])
	commit(t, a)
	finalize(t, a, f.Blocks[1])
	stage := os.Getenv("CIVORA_REPLICA_CRASH_STAGE")
	if stage == "finalize" {
		os.Exit(77)
	}
	j.Hook = func(s string) {
		if s == stage {
			os.Exit(77)
		}
	}
	commit(t, a)
	t.Fatal("crash not reached")
}
func TestAbruptCommitRecovery(t *testing.T) {
	for _, stage := range []string{"finalize", "partial_write", "after_sync", "after_rename"} {
		t.Run(stage, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "journal")
			cmd := exec.Command(os.Args[0], "-test.run=^TestCrashChild$")
			cmd.Env = append(os.Environ(), "CIVORA_REPLICA_CRASH_DIR="+dir, "CIVORA_REPLICA_CRASH_STAGE="+stage)
			out, e := cmd.CombinedOutput()
			if e == nil {
				t.Fatalf("child survived %s", out)
			}
			if exit, ok := e.(*exec.ExitError); !ok || exit.ExitCode() != 77 {
				t.Fatalf("wrong child failure: %v %s", e, out)
			}
			f := load(t)
			a, j := open(t, dir, f)
			want := uint64(1)
			if stage == "after_rename" {
				want = 2
			}
			if j.Machine.Height() != want {
				t.Fatal("wrong durable boundary")
			}
			finalize(t, a, f.Blocks[1])
			commit(t, a)
			if j.Machine.Root() != f.Roots[1] {
				t.Fatal("recovery diverged")
			}
		})
	}
}
