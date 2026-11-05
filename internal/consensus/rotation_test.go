package consensus_test

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/core"
	"civora/internal/governance"
	"civora/internal/network"
	"civora/internal/partitiondev"
	"civora/internal/replica"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/privval"
	proto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func accountFixture(t *testing.T, name string) (json.RawMessage, []core.Block) {
	t.Helper()
	raw, e := os.ReadFile("../../spec/fixtures/" + name + ".json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Genesis json.RawMessage `json:"genesis"`
		Blocks  []core.Block    `json:"blocks"`
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	return f.Genesis, f.Blocks
}
func convert(b core.Block) replica.Block {
	txs := [][]byte{}
	for _, tx := range b.Transactions {
		txs = append(txs, codec.Must(tx))
	}
	return partitiondev.Block(b.Height, b.TimeMS, txs...)
}
func TestValidatorRotationDelayAndDurableResponse(t *testing.T) {
	raw, _ := accountFixture(t, "accounts")
	engine, e := core.New(raw, 2)
	if e != nil {
		t.Fatal(e)
	}
	initial := network.Trust(engine.Config.ChainID)
	keys := append([]string{}, initial.Keys...)
	keys[6] = hex.EncodeToString(network.TestKey(initial.ChainID, "consensus-next", 6).PubKey().Bytes())
	rotation := consensus.Rotation{Height: "2", Keys: keys, Guardians: []governance.Guardian{}, Authorization: "development_genesis_fixture"}
	factory := func() (replica.Machine, error) {
		engine, e := core.New(raw, 2)
		if e != nil {
			return nil, e
		}
		return consensus.WithRotations(&consensus.Ledger{Engine: engine}, initial, []consensus.Rotation{rotation})
	}
	dir := t.TempDir()
	j, e := replica.Open(dir, raw, factory)
	if e != nil {
		t.Fatal(e)
	}
	a := consensus.New(j, initial.ChainID, raw)
	ctx := context.Background()
	for h := int64(1); h <= 4; h++ {
		request := &abci.RequestFinalizeBlock{Height: h, Time: time.UnixMilli(h * 1000).UTC(), Hash: bytes.Repeat([]byte{byte(h)}, 32)}
		r, e := a.FinalizeBlock(ctx, request)
		if e != nil {
			t.Fatal(e)
		}
		if (len(r.ValidatorUpdates) > 0) != (h == 2) {
			t.Fatal("wrong update height")
		}
		if _, e = a.Commit(ctx, &abci.RequestCommit{}); e != nil {
			t.Fatal(e)
		}
		j, e = replica.Open(dir, raw, factory)
		if e != nil {
			t.Fatal(e)
		}
		a = consensus.New(j, initial.ChainID, raw)
		again, e := a.FinalizeBlock(ctx, request)
		if e != nil || len(again.ValidatorUpdates) != len(r.ValidatorUpdates) {
			t.Fatal("validator update lost on durable replay", e)
		}
		if _, e = a.Commit(ctx, &abci.RequestCommit{}); e != nil {
			t.Fatal(e)
		}
	}
	scheduled := j.Machine.(*consensus.Scheduled)
	if scheduled.TrustAt(3).Keys[6] != initial.Keys[6] || scheduled.TrustAt(4).Keys[6] != keys[6] {
		t.Fatal("CometBFT H+2 delay")
	}
	bad := rotation
	bad.Keys = append([]string{}, keys...)
	bad.Keys[1] = bad.Keys[0]
	if _, e = consensus.WithRotations(&consensus.Ledger{Engine: engine}, initial, []consensus.Rotation{bad}); e == nil {
		t.Fatal("duplicate validator")
	}
}
func TestGuardianRotationOutsideActivePolls(t *testing.T) {
	raw, e := os.ReadFile("../../spec/fixtures/v0.4/replay-source.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Cases []struct {
			Genesis json.RawMessage `json:"genesis"`
			Blocks  []core.Block    `json:"blocks"`
		} `json:"cases"`
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	engine, e := core.New(f.Cases[0].Genesis, 2)
	if e != nil {
		t.Fatal(e)
	}
	next, _, e := engine.Apply(codec.Must(f.Cases[0].Blocks[0]))
	if e != nil {
		t.Fatal(e)
	}
	guardians := append([]governance.Guardian{}, next.Governance.Network.Guardians...)
	known := map[string]bool{}
	for _, g := range guardians {
		known[g.AccountID] = true
	}
	for id, a := range next.Accounts {
		if !known[id] {
			guardians[0] = governance.Guardian{AccountID: id, SigningPK: a.SigningPK}
			break
		}
	}
	rotation := consensus.Rotation{Height: "2", Keys: network.Trust(engine.Config.ChainID).Keys, Guardians: guardians, Authorization: "development_genesis_fixture"}
	scheduled, e := consensus.WithRotations(&consensus.Ledger{Engine: next}, network.Trust(engine.Config.ChainID), []consensus.Rotation{rotation})
	if e != nil {
		t.Fatal(e)
	}
	updated, e := scheduled.Apply(partitiondev.Block("2", "1001"))
	if e != nil {
		t.Fatal(e)
	}
	after := updated.(*consensus.Scheduled).Base.(*consensus.Ledger).Engine
	if after.Governance.Network.GuardianEpoch != "2" || after.Governance.Network.Guardians[0].AccountID != guardians[0].AccountID {
		t.Fatal("guardian rotation missing")
	}
	if next.Governance.Network.GuardianEpoch != "1" {
		t.Fatal("mutated prior state")
	}
	// Applying the proposal before the barrier would create a preparing poll.
	qualified, _, e := next.Apply(codec.Must(f.Cases[0].Blocks[1]))
	if e != nil {
		t.Fatal(e)
	}
	rotation.Height = "3"
	rotation.Guardians = []governance.Guardian{}
	scheduled, e = consensus.WithRotations(&consensus.Ledger{Engine: qualified}, network.Trust(engine.Config.ChainID), []consensus.Rotation{rotation})
	if e != nil {
		t.Fatal(e)
	}
	before := scheduled.Root()
	if _, e = scheduled.Apply(convert(f.Cases[0].Blocks[2])); e == nil || !strings.Contains(e.Error(), "ROTATION_ACTIVE_POLL") {
		t.Fatal("opening across rotation allowed", e)
	}
	if scheduled.Root() != before {
		t.Fatal("failed rotation mutated source")
	}
}
func TestSevenApplicationsMixedEpochActivation(t *testing.T) {
	raw, blocks := accountFixture(t, "activation")
	roots := map[string]bool{}
	for seat := 0; seat < 7; seat++ {
		cap := uint64(2)
		if seat >= 5 {
			cap = 1
		}
		engine, e := core.New(raw, cap)
		if e != nil {
			t.Fatal(e)
		}
		machine := replica.Machine(&consensus.Ledger{Engine: engine})
		for i, b := range blocks {
			next, e := machine.Apply(convert(b))
			if i == 2 && cap == 1 {
				if e == nil || !strings.Contains(e.Error(), "ERR_UNSUPPORTED_EPOCH") {
					t.Fatal("old version crossed barrier", e)
				}
				if machine.Height() != 2 {
					t.Fatal("old node mutated")
				}
				break
			}
			if e != nil {
				t.Fatal(e)
			}
			machine = next
		}
		if cap == 2 {
			roots[machine.Root()] = true
		}
	}
	if len(roots) != 1 {
		t.Fatal("five upgraded replicas disagree")
	}
}
func TestSignerRejectsConflictingVoteAfterRestart(t *testing.T) {
	dir := t.TempDir()
	keyPath, statePath := filepath.Join(dir, "key.TEST_ONLY.json"), filepath.Join(dir, "sign-state.json")
	key := network.TestKey("civora-signer-test", "consensus", 0)
	pv := privval.NewFilePV(key, keyPath, statePath)
	pv.Save()
	block := func(label string) proto.BlockID {
		h, _ := hex.DecodeString(codec.Hash("TEST_ONLY/block", label))
		id := types.BlockID{Hash: h, PartSetHeader: types.PartSetHeader{Total: 1, Hash: h}}
		return id.ToProto()
	}
	vote := proto.Vote{Type: proto.PrecommitType, Height: 7, Round: 0, BlockID: block("a"), Timestamp: time.Unix(7, 0).UTC(), ValidatorAddress: key.PubKey().Address(), ValidatorIndex: 0}
	if e := pv.SignVote("test-chain", &vote); e != nil {
		t.Fatal(e)
	}
	signature := append([]byte{}, vote.Signature...)
	pv = privval.LoadFilePV(keyPath, statePath)
	same := vote
	same.Signature = nil
	if e := pv.SignVote("test-chain", &same); e != nil || !bytes.Equal(same.Signature, signature) {
		t.Fatal("same vote retry differs", e)
	}
	conflict := vote
	conflict.Signature = nil
	conflict.BlockID = block("b")
	before, _ := os.ReadFile(statePath)
	if e := pv.SignVote("test-chain", &conflict); e == nil {
		t.Fatal("double sign allowed")
	}
	after, _ := os.ReadFile(statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("conflicting vote overwrote signer state")
	}
	t.Log(fmt.Sprintf("persistent signer protected height %d", vote.Height))
}
func TestNativeZIP215BoundaryVector(t *testing.T) {
	raw, e := os.ReadFile("../../spec/fixtures/v0.5/zip215.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Trust  consensus.Trust `json:"trust"`
		Header string          `json:"signed_header"`
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	if _, e = consensus.VerifyHeader(f.Trust, f.Header); e != nil {
		t.Fatal("native ZIP-215 vector", e)
	}
	raw, e = codec.Blob(f.Header)
	if e != nil {
		t.Fatal(e)
	}
	var sh types.SignedHeader
	if e = cmtjson.Unmarshal(raw, &sh); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 32; i++ {
		sh.Commit.Signatures[0].Signature[i] = 0
	}
	sh.Commit.Signatures[0].Signature[0] = 1
	raw, e = cmtjson.Marshal(sh)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = consensus.VerifyHeader(f.Trust, partitiondev.Encode(raw)); e == nil {
		t.Fatal("signature challenge did not bind unreduced R bytes")
	}
}
