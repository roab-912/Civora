package partition_test

import (
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/electionguard"
	"civora/internal/network"
	"civora/internal/partition"
	"civora/internal/partitiondev"
	"civora/internal/state"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func oneLane(t *testing.T) (*partition.Machine, partition.Frozen, []electionguard.ScaleEnvelope, electionguard.ScaleEnvelope) {
	t.Helper()
	f := fixture(t).Frozen
	main := f.Lanes[0].ChainID[:len(f.Lanes[0].ChainID)-4]
	f.Lanes = f.Lanes[:1]
	f.Context.Partitions = "1"
	f.Context.Checkpoint = f.ID()
	entries := []state.Entry{{Table: "transport_polls", Key: f.Context.PollID, Value: f}}
	path, e := state.Prove(entries, "transport_polls", f.Context.PollID)
	if e != nil {
		t.Fatal(e)
	}
	h, _ := codec.Uint(f.SourceHeight)
	tm, _ := codec.Uint(f.Opens)
	header, e := partitiondev.SignedHeader(network.Trust(main), h+1, tm+1, state.Root(entries), 5)
	if e != nil {
		t.Fatal(e)
	}
	proof := consensus.StateProof{Height: f.SourceHeight, SignedHeader: header, Path: path}
	c := partition.Config{NetworkID: f.Context.NetworkID, ChainID: f.Lanes[0].ChainID, Lane: "0", Epoch: f.Context.Epoch, Source: network.Trust(main)}
	m, e := partition.New(codec.Must(c), 2)
	if e != nil {
		t.Fatal(e)
	}
	m = apply(t, m, fmt.Sprint(tm+1), partitiondev.Command("partition.open", proof)).(*partition.Machine)
	raw, e := os.ReadFile("../../spec/fixtures/v0.5/ballots.json")
	if e != nil {
		t.Fatal(e)
	}
	var data struct {
		Ballots []electionguard.ScaleEnvelope `json:"ballots"`
		Attack  electionguard.ScaleEnvelope   `json:"copy_attack"`
		Third   electionguard.ScaleEnvelope   `json:"third_attempt"`
	}
	if e = json.Unmarshal(raw, &data); e != nil {
		t.Fatal(e)
	}
	envelopes := append([]electionguard.ScaleEnvelope{data.Attack}, data.Ballots...)
	for i := range envelopes {
		envelopes[i].Lane = "0"
		envelopes[i].Checkpoint = f.Context.Checkpoint
	}
	data.Third.Lane = "0"
	data.Third.Checkpoint = f.Context.Checkpoint
	return m, f, envelopes, data.Third
}
func admitCommand(t *testing.T, m *partition.Machine, f partition.Frozen, envelopes []electionguard.ScaleEnvelope) []byte {
	t.Helper()
	xs := []string{}
	for _, e := range envelopes {
		xs = append(xs, partitiondev.Encode(codec.Must(e)))
	}
	r, e := partition.Reference(f.Context, m.Config.Lane, fmt.Sprint(len(m.Polls[f.Context.PollID].Deposits)), xs)
	if e != nil {
		t.Fatal(e)
	}
	return partitiondev.Command("partition.admit", partition.Batch{PollID: f.Context.PollID, Envelopes: xs, Certificate: partitiondev.Certificate(m.Config.Source.ChainID, r)})
}
func TestSeparateBudgetsTwoAttemptsAndFirstValid(t *testing.T) {
	m, f, envelopes, third := oneLane(t)
	tm, _ := codec.Uint(f.Opens)
	id := f.Context.PollID
	command := admitCommand(t, m, f, envelopes)
	m = apply(t, m, fmt.Sprint(tm+2), command).(*partition.Machine)
	if len(m.Polls[id].Deposits) != 6 || m.Polls[id].Processed != "0" {
		t.Fatal("admission performed heavy processing")
	}
	thirdCommand := admitCommand(t, m, f, []electionguard.ScaleEnvelope{third})
	before := m.Root()
	if _, e := m.Apply(partitiondev.Block("3", fmt.Sprint(tm+3), thirdCommand)); e == nil || !strings.Contains(e.Error(), "two distinct attempts") {
		t.Fatal("third fully valid proof did not hit attempt cap", e)
	}
	if m.Root() != before {
		t.Fatal("failed admission mutated")
	}
	process := func(n string) []byte {
		return partitiondev.Command("partition.process", partition.Process{PollID: id, Count: n})
	}
	if _, e := m.Apply(partitiondev.Block("3", fmt.Sprint(tm+3), process("4"), process("2"))); e == nil || !strings.Contains(e.Error(), "budget") {
		t.Fatal("processing budget shared with admission or ignored", e)
	}
	if m.Polls[id].Processed != "0" {
		t.Fatal("overflow processing mutated")
	}
	m = apply(t, m, fmt.Sprint(tm+3), process("4")).(*partition.Machine)
	m = apply(t, m, fmt.Sprint(tm+4), process("2")).(*partition.Machine)
	p := m.Polls[id]
	if p.Processed != "6" || p.Counted != "5" || p.Deposits[0].Status != "invalid" || p.Deposits[2].Status != "counted" {
		t.Fatal("invalid first attempt consumed vote", p.Processed, p.Counted)
	}
	closes, _ := codec.Uint(f.Closes)
	m = apply(t, m, fmt.Sprint(closes), command).(*partition.Machine)
	if !m.Summary(id).ProcessingSealed || len(m.Polls[id].Deposits) != 6 {
		t.Fatal("post-close retry changed sealed log")
	}
}
func TestFinalSealForEmptyPoll(t *testing.T) {
	m, f, _, _ := oneLane(t)
	closes, _ := codec.Uint(f.Closes)
	m = apply(t, m, fmt.Sprint(closes)).(*partition.Machine)
	proof, e := partitiondev.Proof(f.Lanes[0], m, m.Entries(), "lane_checkpoint", f.Context.PollID)
	if e != nil {
		t.Fatal(e)
	}
	cp, e := m.Checkpoint(f.Context.PollID, proof, nil)
	if e != nil {
		t.Fatal(e)
	}
	verified, e := partition.VerifyCheckpoint(f, cp, nil)
	if e != nil {
		t.Fatal(e)
	}
	aggregate, count, e := partition.Global(f, map[string]*partition.Verified{"0": verified})
	if e != nil || count != "0" {
		t.Fatal("empty final seal", e)
	}
	v, _, _ := f.Election()
	if aggregate != v.Zero() {
		t.Fatal("empty aggregate")
	}
	if _, _, e = partition.Global(f, map[string]*partition.Verified{}); e == nil {
		t.Fatal("empty lane may not be omitted")
	}
}
