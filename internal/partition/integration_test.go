package partition_test

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/electionguard"
	"civora/internal/network"
	"civora/internal/partition"
	"civora/internal/partitiondev"
	"civora/internal/replica"
	"civora/internal/state"
	"encoding/json"
	"fmt"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) partitiondev.Fixture {
	t.Helper()
	raw, e := os.ReadFile("../../spec/fixtures/v0.5/transport.json")
	if e != nil {
		t.Fatal(e)
	}
	var f partitiondev.Fixture
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	return f
}
func replay(t *testing.T, r partitiondev.Run) []replica.Machine {
	t.Helper()
	var m replica.Machine
	var e error
	if r.Kind == "partition" {
		m, e = partition.New(r.Genesis, 2)
	} else {
		m, e = partition.NewMain(r.Genesis, 2)
	}
	if e != nil {
		t.Fatal(e)
	}
	states := []replica.Machine{m}
	for i, b := range r.Blocks {
		m, e = m.Apply(b)
		if e != nil {
			t.Fatalf("block %d: %v", i+1, e)
		}
		if m.Root() != r.Roots[i] {
			t.Fatalf("root %d", i+1)
		}
		states = append(states, m)
	}
	return states
}
func copyJSON[T any](v T) T { var out T; _ = json.Unmarshal(codec.Must(v), &out); return out }
func apply(t *testing.T, m replica.Machine, tm string, commands ...[]byte) replica.Machine {
	t.Helper()
	b := partitiondev.Block(fmt.Sprint(m.Height()+1), tm, commands...)
	n, e := m.Apply(b)
	if e != nil {
		t.Fatal(e)
	}
	return n
}
func TestEncryptedPartitionReplayAndGlobalResult(t *testing.T) {
	f := fixture(t)
	main := replay(t, f.Main)
	counted, invalid, empty := 0, 0, 0
	for _, run := range f.Lanes {
		states := replay(t, run)
		last := states[len(states)-1].(*partition.Machine)
		for id, p := range last.Polls {
			if !last.Summary(id).ProcessingSealed {
				t.Fatal("unfinished partition")
			}
			if len(p.Deposits) == 0 {
				empty++
			}
			for _, d := range p.Deposits {
				switch d.Status {
				case "counted":
					counted++
				case "invalid":
					invalid++
				default:
					t.Fatal("unexpected deposit status")
				}
			}
		}
	}
	if counted != 5 || invalid != 1 || empty == 0 {
		t.Fatalf("counts %d/%d/%d", counted, invalid, empty)
	}
	result := main[len(main)-1].(*partition.Main).Results[f.Frozen.Context.PollID]
	if result.State != "provisional" || result.Yes != "3" || result.No != "2" {
		t.Fatal("global result")
	}
	// The transport pilot cannot apply a law before the audit milestone.
	for _, p := range main[len(main)-1].(*partition.Main).Engine.Governance.Proposals {
		if p.State == "applied" {
			t.Fatal("unaudited transport result applied")
		}
	}
}
func TestMissingLaneAndBatchPreventResult(t *testing.T) {
	f := fixture(t)
	states := replay(t, f.Main)
	base := states[6].(*partition.Main)
	commands := [][]byte{}
	missingEmpty := -1
	for i, cp := range f.Final {
		var s partition.Summary
		_ = json.Unmarshal(codec.Must(cp.Proof.Path.Entry.Value), &s)
		if missingEmpty < 0 && s.Admitted == "0" {
			missingEmpty = i
			continue
		}
		commands = append(commands, partitiondev.Command("lane.checkpoint", cp))
	}
	if missingEmpty < 0 {
		t.Fatal("no empty fixture lane")
	}
	m := apply(t, base, f.Main.Blocks[6].TimeMS, commands...)
	before := m.Root()
	last := f.Main.Blocks[7]
	if _, e := m.Apply(last); e == nil {
		t.Fatal("result accepted without empty lane seal")
	}
	if m.Root() != before {
		t.Fatal("failed result mutated state")
	}
	for i, cp := range f.Early {
		if len(cp.Extension) == 0 {
			continue
		}
		cp = copyJSON(cp)
		cp.Extension = cp.Extension[:len(cp.Extension)-1]
		if _, e := partition.VerifyCheckpoint(f.Frozen, cp, nil); e == nil {
			t.Fatalf("missing batch accepted in lane %d", i)
		}
		break
	}
}
func TestCheckpointQuorumContextAndPredecessor(t *testing.T) {
	f := fixture(t)
	for i, cp := range f.Early {
		early, e := partition.VerifyCheckpoint(f.Frozen, cp, nil)
		if e != nil {
			t.Fatal(e)
		}
		final, e := partition.VerifyCheckpoint(f.Frozen, f.Final[i], early)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = partition.VerifyCheckpoint(f.Frozen, f.Final[i], final); e == nil {
			t.Fatal("stale checkpoint accepted")
		}
		bad := copyJSON(f.Final[i])
		bad.PreviousID = strings.Repeat("0", 64)
		if _, e = partition.VerifyCheckpoint(f.Frozen, bad, early); e == nil {
			t.Fatal("unlinked checkpoint")
		}
		bad = copyJSON(cp)
		bad.Proof.Path.Siblings[0] = codec.Hash("tampered", nil)
		if _, e = partition.VerifyCheckpoint(f.Frozen, bad, nil); e == nil {
			t.Fatal("foreign state root")
		}
	}
	for _, signatures := range []int{4, 5} {
		header, e := partitiondev.SignedHeader(network.Trust(f.Frozen.Lanes[0].ChainID), 3, 3601005, f.Lanes[0].Roots[1], signatures)
		if e != nil {
			t.Fatal(e)
		}
		_, e = consensus.VerifyHeader(f.Frozen.Lanes[0], header)
		if (e == nil) != (signatures == 5) {
			t.Fatal("strict quorum 5/7")
		}
		if _, e = consensus.VerifyHeader(network.Trust("foreign-chain"), header); e == nil {
			t.Fatal("foreign chain accepted")
		}
	}
	bad := f.Opening
	bad.Height = "4"
	if _, e := consensus.VerifyState(network.Trust(f.Frozen.Lanes[0].ChainID[:len(f.Frozen.Lanes[0].ChainID)-4]), bad); e == nil {
		t.Fatal("AppHash height offset ignored")
	}
}
func TestLogAndArchiveAttacks(t *testing.T) {
	f := fixture(t)
	for _, cp := range f.Early {
		if len(cp.Extension) == 0 {
			continue
		}
		for _, attack := range []string{"signature", "duplicate", "count", "inclusion", "root"} {
			bad := copyJSON(cp)
			switch attack {
			case "signature":
				bad.Extension[0].Certificate[0].Signature = strings.Repeat("0", 128)
			case "duplicate":
				bad.Extension[0].Certificate[1] = bad.Extension[0].Certificate[0]
			case "count":
				bad.Extension[0].Reference.Count = "2"
			case "inclusion":
				bad.Extension[0].Inclusion = append(bad.Extension[0].Inclusion, strings.Repeat("0", 64))
			case "root":
				bad.AdmissionConsistency = []string{strings.Repeat("0", 64)}
			}
			if _, e := partition.VerifyCheckpoint(f.Frozen, bad, nil); e == nil {
				t.Fatalf("archive/log attack %s", attack)
			}
		}
		break
	}
}
func TestForgedTerminalSealWithRealQuorumRejected(t *testing.T) {
	f := fixture(t)
	for i, run := range f.Lanes {
		states := replay(t, run)
		last := states[len(states)-1].(*partition.Machine)
		summary := last.Summary(f.Frozen.Context.PollID)
		if summary.Admitted == "0" {
			continue
		}
		summary.Processed = "0"
		summary.ProcessingSealed = true
		entries := copyJSON(last.Entries())
		for j := range entries {
			if entries[j].Table == "lane_checkpoint" {
				entries[j].Value = summary
			}
		}
		path, e := state.Prove(entries, "lane_checkpoint", f.Frozen.Context.PollID)
		if e != nil {
			t.Fatal(e)
		}
		sh, e := partitiondev.SignedHeader(f.Frozen.Lanes[i], last.Height()+1, last.Time()+1, state.Root(entries), 5)
		if e != nil {
			t.Fatal(e)
		}
		bad := copyJSON(f.Final[i])
		bad.Proof = consensus.StateProof{Height: fmt.Sprint(last.Height()), SignedHeader: sh, Path: path}
		previous, e := partition.VerifyCheckpoint(f.Frozen, f.Early[i], nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = partition.VerifyCheckpoint(f.Frozen, bad, previous); e == nil {
			t.Fatal("incomplete processing seal accepted")
		}
		break
	}
}
func TestAdmissionRetryDeadlineAndRoute(t *testing.T) {
	f := fixture(t)
	for _, run := range f.Lanes {
		if len(run.Blocks[1].Transactions) == 0 {
			continue
		}
		states := replay(t, run)
		open := states[1].(*partition.Machine)
		admitted := states[2].(*partition.Machine)
		id := f.Frozen.Context.PollID
		raw, _ := codec.Blob(run.Blocks[1].Transactions[0])
		retry := apply(t, admitted, run.Blocks[1].TimeMS, raw).(*partition.Machine)
		if len(retry.Polls[id].Deposits) != len(admitted.Polls[id].Deposits) {
			t.Fatal("retry consumed an attempt")
		}
		closes, _ := codec.Uint(f.Frozen.Closes)
		b := run.Blocks[1]
		b.TimeMS = fmt.Sprint(closes)
		if _, e := open.Apply(b); e == nil {
			t.Fatal("late deposit accepted")
		}
		b.TimeMS = fmt.Sprint(closes - 1)
		if _, e := open.Apply(b); e != nil {
			t.Fatal("timely deposit refused", e)
		}
		var cmd partition.Command
		_ = json.Unmarshal(raw, &cmd)
		var batch partition.Batch
		_ = json.Unmarshal(cmd.Value, &batch)
		var envelope electionguard.ScaleEnvelope
		bytes, _ := codec.Blob(batch.Envelopes[0])
		_ = json.Unmarshal(bytes, &envelope)
		envelope.Lane = fmt.Sprint((mustUint(run.Genesis, "lane_id") + 1) % 8)
		batch.Envelopes[0] = partitiondev.Encode(codec.Must(envelope))
		r, _ := partition.Reference(f.Frozen.Context, open.Config.Lane, "0", batch.Envelopes)
		batch.Certificate = partitiondev.Certificate(open.Config.Source.ChainID, r)
		b = partitiondev.Block("2", run.Blocks[1].TimeMS, partitiondev.Command("partition.admit", batch))
		if _, e := open.Apply(b); e == nil {
			t.Fatal("foreign route accepted")
		}
		break
	}
}
func mustUint(raw []byte, key string) uint64 {
	var c map[string]any
	_ = json.Unmarshal(raw, &c)
	v, _ := codec.Uint(c[key].(string))
	return v
}
func TestDurableReceiptAndTamper(t *testing.T) {
	f := fixture(t)
	for i, run := range f.Lanes {
		b := run.Blocks[1]
		if len(b.Transactions) == 0 {
			continue
		}
		txs := types.Txs{}
		for _, encoded := range b.Transactions {
			raw, _ := codec.Blob(encoded)
			txs = append(txs, types.Tx(raw))
		}
		proof := txs.Proof(0)
		encoded, e := cmtjson.Marshal(proof)
		if e != nil {
			t.Fatal(e)
		}
		var c partition.Command
		_ = json.Unmarshal(txs[0], &c)
		var batch partition.Batch
		_ = json.Unmarshal(c.Value, &batch)
		ref, _ := partition.Reference(f.Frozen.Context, fmt.Sprint(i), "0", batch.Envelopes)
		items := [][]byte{}
		for _, s := range batch.Envelopes {
			b, _ := codec.Blob(s)
			items = append(items, b)
		}
		path, _ := partition.Inclusion(items, 0)
		tm, _ := codec.Uint(b.TimeMS)
		h, _ := codec.Uint(b.Height)
		header, e := partitiondev.SignedHeaderData(f.Frozen.Lanes[i], h, tm, run.Roots[0], 5, txs)
		if e != nil {
			t.Fatal(e)
		}
		receipt := partition.Receipt{SignedHeader: header, TransactionProof: partitiondev.Encode(encoded), Reference: ref, Envelope: batch.Envelopes[0], Index: "0", Inclusion: path}
		if e = partition.VerifyReceipt(f.Frozen, receipt); e != nil {
			t.Fatal(e)
		}
		for _, attack := range []string{"envelope", "index", "inclusion", "quorum", "time"} {
			bad := copyJSON(receipt)
			switch attack {
			case "envelope":
				bad.Envelope = partitiondev.Encode([]byte(`{}`))
			case "index":
				bad.Index = "1"
			case "inclusion":
				bad.Inclusion = []string{strings.Repeat("0", 64)}
			case "quorum":
				bad.SignedHeader, _ = partitiondev.SignedHeaderData(f.Frozen.Lanes[i], h, tm, run.Roots[0], 4, txs)
			case "time":
				close, _ := codec.Uint(f.Frozen.Closes)
				bad.SignedHeader, _ = partitiondev.SignedHeaderData(f.Frozen.Lanes[i], h, close, run.Roots[0], 5, txs)
			}
			if partition.VerifyReceipt(f.Frozen, bad) == nil {
				t.Fatal("receipt tamper", attack)
			}
		}
		break
	}
}
func TestPartitionCrashChild(t *testing.T) {
	dir := os.Getenv("CIVORA_PARTITION_CRASH_DIR")
	if dir == "" {
		return
	}
	f := fixture(t)
	lane := mustEnvInt("CIVORA_PARTITION_CRASH_LANE")
	target := mustEnvInt("CIVORA_PARTITION_CRASH_BLOCK")
	run := f.Lanes[lane]
	j, e := replica.Open(dir, run.Genesis, func() (replica.Machine, error) { return partition.New(run.Genesis, 2) })
	if e != nil {
		t.Fatal(e)
	}
	for i, b := range run.Blocks[:target] {
		next, e := j.Machine.Apply(b)
		if e != nil {
			t.Fatal(e)
		}
		if i == target-1 {
			j.Hook = func(stage string) {
				if stage == os.Getenv("CIVORA_PARTITION_CRASH_STAGE") {
					os.Exit(77)
				}
			}
		}
		tm, _ := codec.Uint(b.TimeMS)
		record := replica.Record{Block: b, Identity: replica.Identity{Hash: codec.Hash("TEST_ONLY/block", b), Time: time.UnixMilli(int64(tm)).UTC().Format(time.RFC3339Nano)}, State: next.State(), AppHash: next.Root()}
		if e = j.Commit(record, next); e != nil {
			t.Fatal(e)
		}
	}
	t.Fatal("crash not reached")
}
func mustEnvInt(key string) int { var n int; _, _ = fmt.Sscan(os.Getenv(key), &n); return n }
func TestProvisionalRequiresAuditAndExpiresWithoutEffect(t *testing.T) {
	f := fixture(t)
	states := replay(t, f.Main)
	m := states[len(states)-1].(*partition.Main)
	id := f.Frozen.Context.PollID
	p := m.Engine.Governance.Polls[id]
	if p.State != "provisional" || p.Count != "5" || p.Result != nil || !m.Active() {
		t.Fatal("partition result bypassed audit")
	}
	closes, _ := codec.Uint(f.Frozen.Closes)
	n := apply(t, m, fmt.Sprint(closes+48*60*60*1000)).(*partition.Main)
	if n.Engine.Governance.Polls[id].State != "failed_certification" || n.Results[id].State != "failed_certification" || n.Active() {
		t.Fatal("uncertified poll did not expire safely")
	}
	if len(n.Engine.Governance.Objects) != 0 {
		t.Fatal("uncertified law applied")
	}
}
func TestPartitionAbruptAdmissionProcessingSealRecovery(t *testing.T) {
	f := fixture(t)
	lane := -1
	for i, r := range f.Lanes {
		if len(r.Blocks) > 3 {
			lane = i
			break
		}
	}
	if lane < 0 {
		t.Fatal("missing populated lane")
	}
	run := f.Lanes[lane]
	targets := []int{2, 3, len(run.Blocks)}
	for _, target := range targets {
		for _, stage := range []string{"partial_write", "after_sync", "after_rename"} {
			t.Run(fmt.Sprintf("block%d/%s", target, stage), func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "journal")
				child := exec.Command(os.Args[0], "-test.run=^TestPartitionCrashChild$")
				child.Env = append(os.Environ(), "CIVORA_PARTITION_CRASH_DIR="+dir, "CIVORA_PARTITION_CRASH_LANE="+fmt.Sprint(lane), "CIVORA_PARTITION_CRASH_BLOCK="+fmt.Sprint(target), "CIVORA_PARTITION_CRASH_STAGE="+stage)
				out, e := child.CombinedOutput()
				if exit, ok := e.(*exec.ExitError); !ok || exit.ExitCode() != 77 {
					t.Fatalf("child: %v %s", e, out)
				}
				j, e := replica.Open(dir, run.Genesis, func() (replica.Machine, error) { return partition.New(run.Genesis, 2) })
				if e != nil {
					t.Fatal(e)
				}
				want := uint64(target - 1)
				if stage == "after_rename" {
					want++
				}
				if j.Machine.Height() != want {
					t.Fatal("durability boundary")
				}
				for _, b := range run.Blocks[j.Machine.Height():] {
					next, e := j.Machine.Apply(b)
					if e != nil {
						t.Fatal(e)
					}
					tm, _ := codec.Uint(b.TimeMS)
					if e = j.Commit(replica.Record{Block: b, Identity: replica.Identity{Hash: codec.Hash("TEST_ONLY/block", b), Time: time.UnixMilli(int64(tm)).UTC().Format(time.RFC3339Nano)}, State: next.State(), AppHash: next.Root()}, next); e != nil {
						t.Fatal(e)
					}
				}
				if j.Machine.Root() != run.Roots[len(run.Roots)-1] {
					t.Fatal("recovery divergence")
				}
				saved := j.Machine.State()
				j, e = replica.Open(dir, run.Genesis, func() (replica.Machine, error) { return partition.New(run.Genesis, 2) })
				if e != nil || !bytes.Equal(saved, j.Machine.State()) {
					t.Fatal("second replay divergence", e)
				}
			})
		}
	}
}
