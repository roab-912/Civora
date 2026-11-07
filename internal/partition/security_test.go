package partition_test

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/electionguard"
	"civora/internal/partition"
	"civora/internal/partitiondev"
	"civora/internal/recoverytest"
	"civora/internal/replica"
	"civora/internal/state"
	"civora/internal/testutil"
	"encoding/json"
	"fmt"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSecurityPartitionPreservesDepositsAndRefusesResumption(t *testing.T) {
	f := fixture(t)
	var cfg partition.Config
	if err := json.Unmarshal(f.Lanes[0].Genesis, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Recovery = recoverytest.Config(t, cfg.NetworkID, "transport_dev_v0_5")
	raw := codec.Must(cfg)
	// Rebind a public development context to the root installed in this isolated
	// partition genesis. Native signed proof construction uses PUBLIC_TEST_ONLY keys.
	frozen := f.Frozen
	frozen.Recovery = cfg.Recovery
	frozen.Context.Checkpoint = frozen.ID()
	entries := []state.Entry{{Table: "transport_polls", Key: frozen.Context.PollID, Value: frozen}}
	proof, err := state.Prove(entries, "transport_polls", frozen.Context.PollID)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := codec.Uint(frozen.SourceHeight)
	opens, _ := codec.Uint(frozen.Opens)
	header, err := partitiondev.SignedHeader(cfg.Source, h+1, opens, state.Root(entries), 5)
	if err != nil {
		t.Fatal(err)
	}
	opening := partitiondev.Command("partition.open", map[string]any{"height": frozen.SourceHeight, "signed_header": header, "path": proof})
	factory := func() (replica.Machine, error) { return partition.New(raw, 2) }
	dir := filepath.Join(t.TempDir(), "journal")
	j, err := replica.Open(dir, raw, factory)
	if err != nil {
		t.Fatal(err)
	}
	commit := func(tm uint64, txs ...[]byte) {
		t.Helper()
		b := partitiondev.Block(fmt.Sprint(j.Machine.Height()+1), fmt.Sprint(tm), txs...)
		n, e := j.Machine.Apply(b)
		if e != nil {
			t.Fatal(e)
		}
		if e = j.Commit(replica.Record{Block: b, Identity: replica.Identity{Hash: codec.Hash("TEST_ONLY/block", b), Time: time.UnixMilli(int64(tm)).UTC().Format(time.RFC3339Nano)}, State: n.State(), AppHash: n.Root()}, n); e != nil {
			t.Fatal(e)
		}
	}
	commit(opens, opening)
	// Reuse the genuine ballot/eligibility proof bytes, changing only the frozen
	// checkpoint identifier. All proof bindings are then verified on admission.
	var original partition.Command
	txRaw, _ := codec.Blob(f.Lanes[0].Blocks[1].Transactions[0])
	json.Unmarshal(txRaw, &original)
	var batch partition.Batch
	json.Unmarshal(original.Value, &batch)
	for i, encoded := range batch.Envelopes {
		b, _ := codec.Blob(encoded)
		var env electionguard.ScaleEnvelope
		if e := json.Unmarshal(b, &env); e != nil {
			t.Fatal(e)
		}
		env.Checkpoint = frozen.Context.Checkpoint
		batch.Envelopes[i] = partitiondev.Encode(codec.Must(env))
	}
	// Recompute the exact archive reference and 3/5 signatures for these bytes.
	ref, err := partition.Reference(frozen.Context, cfg.Lane, "0", batch.Envelopes)
	if err != nil {
		t.Fatal(err)
	}
	batch.Certificate = partitiondev.Certificate(cfg.Source.ChainID, ref)
	admission := partitiondev.Command("partition.admit", batch)
	prior := j.Machine.Root()
	commit(opens+1, admission)
	txs := types.Txs{types.Tx(admission)}
	txProof, err := cmtjson.Marshal(txs.Proof(0))
	if err != nil {
		t.Fatal(err)
	}
	receiptHeader, err := partitiondev.SignedHeaderData(frozen.Lanes[0], 2, opens+1, prior, 5, txs)
	if err != nil {
		t.Fatal(err)
	}
	items := [][]byte{}
	for _, s := range batch.Envelopes {
		raw, _ := codec.Blob(s)
		items = append(items, raw)
	}
	inclusion, _ := partition.Inclusion(items, 0)
	receipt := partition.Receipt{SignedHeader: receiptHeader, TransactionProof: partitiondev.Encode(txProof), Reference: ref, Envelope: batch.Envelopes[0], Index: "0", Inclusion: inclusion}
	if err = partition.VerifyReceipt(frozen, receipt); err != nil {
		t.Fatal(err)
	}
	before := j.Machine.(*partition.Machine)
	count := len(before.Polls[frozen.Context.PollID].Deposits)
	hold := recoverytest.Hold(t, cfg.Recovery, "poll", frozen.Context.PollID, "1", opens+2, opens+1000)
	commit(opens+2, recoverytest.Command(hold))
	after := j.Machine.(*partition.Machine)
	if !after.Polls[frozen.Context.PollID].Interrupted || len(after.Polls[frozen.Context.PollID].Deposits) != count || after.Summary(frozen.Context.PollID).ProcessingSealed {
		t.Fatal("interrupted partition lost evidence or became certifiable")
	}
	root := after.Root()
	checkpointProof, err := state.Prove(after.Entries(), "lane_checkpoint", frozen.Context.PollID)
	if err != nil {
		t.Fatal(err)
	}
	checkpointHeader, err := partitiondev.SignedHeader(frozen.Lanes[0], 4, opens+3, root, 5)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := after.Checkpoint(frozen.Context.PollID, consensus.StateProof{Height: "3", SignedHeader: checkpointHeader, Path: checkpointProof}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = partition.VerifyCheckpoint(frozen, cp, nil); err == nil || !strings.Contains(err.Error(), "SECURITY_INTERRUPTED") {
		t.Fatal("interrupted checkpoint certifiable", err)
	}
	fixtureBytes := codec.Must(map[string]any{"mode": "PUBLIC_TEST_ONLY", "frozen": frozen, "checkpoint": cp, "expected_error": "ERR_SECURITY_INTERRUPTED"})
	fixturePath := filepath.Join(testutil.Root(), "spec", "fixtures", "v0.9", "interrupted-checkpoint.TEST_ONLY.json")
	if os.Getenv("CIVORA_WRITE_SAFETY_FIXTURES") == "1" {
		if err = os.WriteFile(fixturePath, fixtureBytes, 0600); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := os.ReadFile(fixturePath)
	if err != nil || !bytes.Equal(saved, fixtureBytes) {
		t.Fatal("interrupted checkpoint fixture differs", err)
	}
	j, err = replica.Open(dir, raw, factory)
	if err != nil || j.Machine.Root() != root {
		t.Fatal("interruption not replayable", err)
	}
	commit(opens + 2000)
	if err = j.Machine.Check(partitiondev.Command("partition.process", partition.Process{PollID: frozen.Context.PollID, Count: "1"})); err == nil {
		t.Fatal("processing resumed after hold expiry")
	}
	if err = j.Machine.Check(admission); err != nil {
		t.Fatal("original exact deposit retry unavailable", err)
	}
	if err = partition.VerifyReceipt(frozen, receipt); err != nil {
		t.Fatal("historical receipt lost", err)
	}
}
