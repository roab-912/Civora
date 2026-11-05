// Package citizendev builds public, reproducible v0.6 archive scenarios.
package citizendev

import (
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/citizen"
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/core"
	"civora/internal/network"
	"civora/internal/partition"
	"civora/internal/partitiondev"
	"civora/internal/replica"
	"civora/internal/state"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	version "github.com/cometbft/cometbft/proto/tendermint/version"
	"github.com/cometbft/cometbft/types"
	"os"
	"time"
)

type Run struct {
	ChainID string         `json:"chain_id"`
	Genesis string         `json:"genesis"`
	Blocks  []chain.Bundle `json:"blocks"`
	Roots   []string       `json:"roots"`
}
type Fixture struct {
	Label    string           `json:"label"`
	Manifest citizen.Manifest `json:"manifest"`
	Runs     []Run            `json:"runs"`
}
type Builder struct {
	Run     Run
	Genesis *chain.Genesis
	Machine replica.Machine
	Commit  *types.Commit
	Views   []replica.Machine
}

func New(app network.Genesis) (*Builder, error) {
	id, e := network.ApplicationChain(app)
	if e != nil {
		return nil, e
	}
	vals := []types.GenesisValidator{}
	for i := 0; i < 7; i++ {
		k := network.TestKey(id, "consensus", i)
		vals = append(vals, types.GenesisValidator{Address: k.PubKey().Address(), PubKey: k.PubKey(), Power: 1, Name: fmt.Sprintf("PUBLIC_TEST_ONLY_%d", i)})
	}
	params := types.DefaultConsensusParams()
	params.Block.MaxBytes = replica.MaxBlockBytes
	g := types.GenesisDoc{GenesisTime: time.UnixMilli(1000).UTC(), ChainID: id, InitialHeight: 1, ConsensusParams: params, Validators: vals, AppState: codec.Must(app)}
	raw, e := cmtjson.Marshal(g)
	if e != nil {
		return nil, e
	}
	gen, e := chain.ParseGenesis(raw)
	if e != nil {
		return nil, e
	}
	m, e := gen.Machine()
	return &Builder{Run: Run{id, base64.RawURLEncoding.EncodeToString(raw), []chain.Bundle{}, []string{}}, Genesis: gen, Machine: m, Commit: &types.Commit{Signatures: []types.CommitSig{}}, Views: []replica.Machine{m}}, e
}
func (b *Builder) Add(tm uint64, commands ...[]byte) error {
	h := b.Machine.Height() + 1
	txs := types.Txs{}
	for _, raw := range commands {
		txs = append(txs, types.Tx(raw))
	}
	block := types.MakeBlock(int64(h), txs, b.Commit, nil)
	set, e := b.Genesis.TrustAt(h).Set()
	if e != nil {
		return e
	}
	nextSet, e := b.Genesis.TrustAt(h + 1).Set()
	if e != nil {
		return e
	}
	app, _ := hex.DecodeString(b.Machine.Root())
	lastID := types.BlockID{}
	if h > 1 {
		lastID = b.Commit.BlockID
	}
	block.Header = types.Header{Version: version.Consensus{Block: 11, App: 1}, ChainID: b.Run.ChainID, Height: int64(h), Time: time.UnixMilli(int64(tm)).UTC(), LastBlockID: lastID, LastCommitHash: b.Commit.Hash(), DataHash: block.Data.Hash(), ValidatorsHash: set.Hash(), NextValidatorsHash: nextSet.Hash(), ConsensusHash: b.Genesis.Doc.ConsensusParams.Hash(), AppHash: app, LastResultsHash: []byte{}, EvidenceHash: block.Evidence.Hash(), ProposerAddress: set.Validators[0].Address}
	parts, e := block.MakePartSet(types.BlockPartSizeBytes)
	if e != nil {
		return e
	}
	id := types.BlockID{Hash: block.Hash(), PartSetHeader: parts.Header()}
	commit := &types.Commit{Height: int64(h), Round: 0, BlockID: id, Signatures: []types.CommitSig{}}
	for index, val := range set.Validators {
		signature := types.NewCommitSigAbsent()
		if index < 5 {
			vote := types.Vote{Type: tmproto.PrecommitType, Height: int64(h), Round: 0, BlockID: id, Timestamp: block.Time, ValidatorAddress: val.Address, ValidatorIndex: int32(index)}
			found := false
			for seat := 0; seat < 7; seat++ {
				key := network.TestKey(b.Run.ChainID, "consensus", seat)
				if hex.EncodeToString(key.PubKey().Address()) == hex.EncodeToString(val.Address) {
					vote.Signature, e = key.Sign(types.VoteSignBytes(b.Run.ChainID, vote.ToProto()))
					found = true
					break
				}
			}
			if !found || e != nil {
				return fmt.Errorf("fixture signing key")
			}
			signature = vote.CommitSig()
		}
		commit.Signatures = append(commit.Signatures, signature)
	}
	bundle, e := chain.Encode(block, commit)
	if e != nil {
		return e
	}
	r := partitiondev.Block(fmt.Sprint(h), fmt.Sprint(tm), commands...)
	next, e := b.Machine.Apply(r)
	if e != nil {
		return e
	}
	b.Machine = next
	b.Views = append(b.Views, next)
	b.Commit = commit
	b.Run.Blocks = append(b.Run.Blocks, bundle)
	b.Run.Roots = append(b.Run.Roots, next.Root())
	return nil
}
func (b *Builder) Proof(h uint64, table string, key any) (consensus.StateProof, error) {
	if h == 0 || h >= uint64(len(b.Run.Blocks)) {
		return consensus.StateProof{}, fmt.Errorf("fixture proof requires H+1")
	}
	m := b.Views[h]
	p, e := state.Prove(m.(interface{ Entries() []state.Entry }).Entries(), table, key)
	return consensus.StateProof{Height: fmt.Sprint(h), SignedHeader: b.Run.Blocks[h].SignedHeader, Path: p}, e
}
func Build() (Fixture, error) {
	var old partitiondev.Fixture
	raw, e := os.ReadFile("spec/fixtures/v0.5/transport.json")
	if e != nil {
		return Fixture{}, e
	}
	if e = json.Unmarshal(raw, &old); e != nil {
		return Fixture{}, e
	}
	var config partition.MainConfig
	if e = json.Unmarshal(old.Main.Genesis, &config); e != nil {
		return Fixture{}, e
	}
	var ledger core.Config
	if e = json.Unmarshal(config.Ledger, &ledger); e != nil {
		return Fixture{}, e
	}
	ledger.VotingProfile = "transport_dev_v0_6"
	config.Ledger = codec.Must(ledger)
	config.Profile = "transport_dev_v0_6"
	config.Audit = &partition.AuditConfig{Keys: []string{}, Implementations: []string{partition.GoAudit, partition.TSAudit}}
	for i := 0; i < 5; i++ {
		config.Audit.Keys = append(config.Audit.Keys, hex.EncodeToString(network.TestKey(ledger.ChainID, "audit", i).PubKey().Bytes()))
	}
	main, e := New(network.Genesis{Kind: "coordinator", Data: codec.Must(config)})
	if e != nil {
		return Fixture{}, e
	}
	for _, r := range old.Main.Blocks[:5] {
		txs := [][]byte{}
		for _, s := range r.Transactions {
			tx, _ := codec.Blob(s)
			txs = append(txs, tx)
		}
		tm, _ := codec.Uint(r.TimeMS)
		if e = main.Add(tm, txs...); e != nil {
			return Fixture{}, e
		}
	}
	frozen := main.Machine.(*partition.Main).Polls[old.Frozen.Context.PollID]
	id := frozen.Context.PollID
	opens, _ := codec.Uint(frozen.Opens)
	closes, _ := codec.Uint(frozen.Closes)
	if e = main.Add(opens + 1); e != nil {
		return Fixture{}, e
	}
	opening, e := main.Proof(5, "transport_polls", id)
	if e != nil {
		return Fixture{}, e
	}
	lanes := []*Builder{}
	early, final := [][]byte{}, [][]byte{}
	for i, run := range old.Lanes {
		lane, e := New(network.Genesis{Kind: "partition", Data: run.Genesis})
		if e != nil {
			return Fixture{}, e
		}
		if e = lane.Add(1000); e != nil {
			return Fixture{}, e
		}
		if e = lane.Add(opens+2, partitiondev.Command("partition.open", opening)); e != nil {
			return Fixture{}, e
		}
		txs := [][]byte{}
		for _, s := range run.Blocks[1].Transactions {
			tx, _ := codec.Blob(s)
			var command partition.Command
			_ = json.Unmarshal(tx, &command)
			var batch partition.Batch
			_ = json.Unmarshal(command.Value, &batch)
			for j, encoded := range batch.Envelopes {
				raw, _ := codec.Blob(encoded)
				var envelope map[string]any
				_ = json.Unmarshal(raw, &envelope)
				envelope["checkpoint_id"] = frozen.Context.Checkpoint
				batch.Envelopes[j] = base64.RawURLEncoding.EncodeToString(codec.Must(envelope))
			}
			reference, e := partition.Reference(frozen.Context, fmt.Sprint(i), fmt.Sprint(len(txs)), batch.Envelopes)
			if e != nil {
				return Fixture{}, e
			}
			batch.Certificate = partitiondev.Certificate(main.Run.ChainID, reference)
			txs = append(txs, partitiondev.Command("partition.admit", batch))
		}
		if e = lane.Add(opens+3, txs...); e != nil {
			return Fixture{}, e
		}
		earlyHeight := lane.Machine.Height()
		deposits := len(lane.Machine.(*partition.Machine).Polls[id].Deposits)
		if deposits > 0 {
			n := min(deposits, partition.VerificationBudget)
			if e = lane.Add(opens+4, partitiondev.Command("partition.process", partition.Process{PollID: id, Count: fmt.Sprint(n)})); e != nil {
				return Fixture{}, e
			}
			if deposits > n {
				if e = lane.Add(opens+5, partitiondev.Command("partition.process", partition.Process{PollID: id, Count: fmt.Sprint(deposits - n)})); e != nil {
					return Fixture{}, e
				}
			}
		} else {
			if e = lane.Add(opens + 4); e != nil {
				return Fixture{}, e
			}
		}
		proof, e := lane.Proof(earlyHeight, "lane_checkpoint", id)
		if e != nil {
			return Fixture{}, e
		}
		before := lane.Views[earlyHeight].(*partition.Machine)
		cp, e := before.Checkpoint(id, proof, nil)
		if e != nil {
			return Fixture{}, e
		}
		verified, e := partition.VerifyCheckpoint(frozen, cp, nil)
		if e != nil {
			return Fixture{}, e
		}
		early = append(early, partitiondev.Command("lane.checkpoint", cp))
		if e = lane.Add(closes); e != nil {
			return Fixture{}, e
		}
		sealedHeight := lane.Machine.Height()
		if e = lane.Add(closes + 1); e != nil {
			return Fixture{}, e
		}
		proof, e = lane.Proof(sealedHeight, "lane_checkpoint", id)
		if e != nil {
			return Fixture{}, e
		}
		sealed := lane.Views[sealedHeight].(*partition.Machine)
		cp, e = sealed.Checkpoint(id, proof, verified)
		if e != nil {
			return Fixture{}, e
		}
		if _, e = partition.VerifyCheckpoint(frozen, cp, verified); e != nil {
			return Fixture{}, fmt.Errorf("lane %d: %w", i, e)
		}
		final = append(final, partitiondev.Command("lane.checkpoint", cp))
		lanes = append(lanes, lane)
	}
	if e = main.Add(opens+10, early...); e != nil {
		return Fixture{}, e
	}
	if e = main.Add(closes+10, final...); e != nil {
		return Fixture{}, e
	}
	last := old.Main.Blocks[len(old.Main.Blocks)-1]
	raw, e = codec.Blob(last.Transactions[0])
	if e != nil {
		return Fixture{}, e
	}
	if e = main.Add(closes+11, raw); e != nil {
		return Fixture{}, e
	}
	if e = main.Add(closes + 12); e != nil {
		return Fixture{}, e
	}
	descriptors := func(b *Builder, lane *string) citizen.Chain {
		raw, _ := codec.Blob(b.Run.Genesis)
		peers := []string{}
		for i := 0; i < 7; i++ {
			key := network.TestKey(b.Run.ChainID, "p2p", i)
			peers = append(peers, fmt.Sprintf("%s@127.0.0.1:%d", hex.EncodeToString(key.PubKey().Address()), 27650+2*i))
		}
		return citizen.Chain{ChainID: b.Run.ChainID, Lane: lane, Genesis: archive.Ref{SHA256: archive.Digest(raw), Size: fmt.Sprint(len(raw))}, Origins: []citizen.Origin{{API: "http://127.0.0.1:8786", RPC: "http://127.0.0.1:27651"}, {API: "http://127.0.0.1:8787", RPC: "http://127.0.0.1:27653"}}, Peers: peers}
	}
	m := citizen.Manifest{Format: "civic-network-1", Name: "PUBLIC TEST ONLY archive fixture", Mode: "development", NetworkID: frozen.Context.NetworkID, Profile: "transport_dev_v0_6", Main: descriptors(main, nil), Lanes: []citizen.Chain{}}
	f := Fixture{Label: "PUBLIC_TEST_ONLY_REAL_CRYPTO_SYNTHETIC_LINKED_NATIVE_COMMITS", Manifest: m, Runs: []Run{main.Run}}
	for i, lane := range lanes {
		n := fmt.Sprint(i)
		f.Manifest.Lanes = append(f.Manifest.Lanes, descriptors(lane, &n))
		f.Runs = append(f.Runs, lane.Run)
	}
	for _, run := range f.Runs {
		raw, _ := codec.Blob(run.Genesis)
		g, e := chain.ParseGenesis(raw)
		if e != nil {
			return Fixture{}, e
		}
		v, e := chain.New(g)
		if e != nil {
			return Fixture{}, e
		}
		for _, block := range run.Blocks {
			if e = v.Apply(block); e != nil {
				return Fixture{}, fmt.Errorf("fixture chain %s: %w", run.ChainID, e)
			}
		}
	}
	return f, nil
}
