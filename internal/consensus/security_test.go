package consensus_test

import (
	"bufio"
	"bytes"
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/core"
	"civora/internal/recoverytest"
	"civora/internal/replica"
	"civora/internal/testutil"
	"context"
	"encoding/base64"
	"fmt"
	abci "github.com/cometbft/cometbft/abci/types"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSecurityPriorityAndDurableABCICommit(t *testing.T) {
	f := testutil.Load(t, "accounts")
	f.Genesis.Recovery = recoverytest.Config(t, f.Genesis.NetworkID, "inline_dev")
	raw := codec.Must(f.Genesis)
	dir := filepath.Join(t.TempDir(), "ledger")
	factory := func() (replica.Machine, error) {
		e, err := core.New(raw, 2)
		if err != nil {
			return nil, err
		}
		return &consensus.Ledger{Engine: e}, nil
	}
	j, err := replica.Open(dir, raw, factory)
	if err != nil {
		t.Fatal(err)
	}
	app := consensus.New(j, f.Genesis.ChainID, raw)
	hold := recoverytest.Hold(t, f.Genesis.Recovery, "module", "dev.account.ed25519.v1", "1", 1000, 2000)
	order := recoverytest.Command(hold)
	normal := codec.Must(f.Blocks[0].Transactions[0])
	ctx := context.Background()
	now := time.UnixMilli(1000).UTC()
	prep, err := app.PrepareProposal(ctx, &abci.RequestPrepareProposal{Height: 1, Time: now, MaxTxBytes: 2 << 20, Txs: [][]byte{normal, order}})
	if err != nil || len(prep.Txs) != 1 || !bytes.Equal(prep.Txs[0], order) {
		t.Fatal("hold hidden behind a normal transaction", err)
	}
	r := &abci.RequestFinalizeBlock{Height: 1, Time: now, Hash: bytes.Repeat([]byte{1}, 32), Txs: prep.Txs}
	if _, err = app.FinalizeBlock(ctx, r); err != nil {
		t.Fatal(err)
	}
	if j.Machine.Height() != 0 {
		t.Fatal("speculative hold leaked")
	}
	if _, err = app.Commit(ctx, &abci.RequestCommit{}); err != nil {
		t.Fatal(err)
	}
	root := j.Machine.Root()
	j, err = replica.Open(dir, raw, factory)
	if err != nil || j.Machine.Root() != root {
		t.Fatal("hold lost on restart", err)
	}
	if err = j.Machine.Check(normal); err == nil {
		t.Fatal("held operation available after restart")
	}
}

func TestSecurityCrashChild(t *testing.T) {
	dir := os.Getenv("CIVORA_SECURITY_CRASH_DIR")
	if dir == "" {
		t.Skip("PUBLIC_TEST_ONLY child process")
	}
	f := testutil.Load(t, "accounts")
	f.Genesis.Recovery = recoverytest.Config(t, f.Genesis.NetworkID, "inline_dev")
	raw := codec.Must(f.Genesis)
	factory := func() (replica.Machine, error) {
		e, err := core.New(raw, 2)
		if err != nil {
			return nil, err
		}
		return &consensus.Ledger{Engine: e}, nil
	}
	j, err := replica.Open(dir, raw, factory)
	if err != nil {
		t.Fatal(err)
	}
	hold := recoverytest.Hold(t, f.Genesis.Recovery, "module", "dev.account.ed25519.v1", "1", 1000, 2000)
	b := replica.Block{Height: "1", TimeMS: "1000", Transactions: []string{partitionEncode(recoverytest.Command(hold))}}
	next, err := j.Machine.Apply(b)
	if err != nil {
		t.Fatal(err)
	}
	j.Hook = func(stage string) {
		if stage == os.Getenv("CIVORA_SECURITY_CRASH_STAGE") {
			fmt.Fprintln(os.Stdout, "READY")
			for {
				time.Sleep(time.Hour)
			}
		}
	}
	if err = j.Commit(replica.Record{Block: b, Identity: replica.Identity{Hash: codec.Hash("TEST_ONLY/block", b), Time: time.UnixMilli(1000).UTC().Format(time.RFC3339Nano)}, State: next.State(), AppHash: next.Root()}, next); err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash hook was not reached")
}
func partitionEncode(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func TestSecurityCrashBoundary(t *testing.T) {
	for _, stage := range []string{"after_sync", "after_rename"} {
		t.Run(stage, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "journal")
			cmd := exec.Command(os.Args[0], "-test.run=^TestSecurityCrashChild$")
			cmd.Env = append(os.Environ(), "CIVORA_SECURITY_CRASH_DIR="+dir, "CIVORA_SECURITY_CRASH_STAGE="+stage)
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			line, err := bufio.NewReader(out).ReadString('\n')
			if err != nil || line != "READY\n" {
				t.Fatal("child not ready", line, err)
			}
			cmd.Process.Kill()
			cmd.Wait()
			f := testutil.Load(t, "accounts")
			f.Genesis.Recovery = recoverytest.Config(t, f.Genesis.NetworkID, "inline_dev")
			raw := codec.Must(f.Genesis)
			factory := func() (replica.Machine, error) {
				e, err := core.New(raw, 2)
				if err != nil {
					return nil, err
				}
				return &consensus.Ledger{Engine: e}, nil
			}
			j, err := replica.Open(dir, raw, factory)
			if err != nil {
				t.Fatal(err)
			}
			want := uint64(0)
			if stage == "after_rename" {
				want = 1
			}
			if j.Machine.Height() != want {
				t.Fatal("wrong durable boundary")
			}
			blocked := j.Machine.Check(codec.Must(f.Blocks[0].Transactions[0])) != nil
			if blocked != (want == 1) {
				t.Fatal("hold state and counter diverged from the commit boundary")
			}
		})
	}
}
