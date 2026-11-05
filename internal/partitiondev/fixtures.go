// Package partitiondev creates public TEST_ONLY fixtures with real crypto and
// deterministic signed headers. These fabricated fixture commits are distinct
// from the seven-process native network acceptance test.
package partitiondev

import (
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/core"
	"civora/internal/electionguard"
	"civora/internal/network"
	"civora/internal/partition"
	"civora/internal/replica"
	"civora/internal/state"
	"civora/spec"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtversion "github.com/cometbft/cometbft/proto/tendermint/version"
	"github.com/cometbft/cometbft/types"
	"os"
	"sort"
	"time"
)

type Run struct {
	Kind    string          `json:"kind"`
	Genesis json.RawMessage `json:"genesis"`
	Blocks  []replica.Block `json:"blocks"`
	Roots   []string        `json:"roots"`
}
type Fixture struct {
	Label   string                 `json:"label"`
	Main    Run                    `json:"main"`
	Lanes   []Run                  `json:"lanes"`
	Frozen  partition.Frozen       `json:"frozen"`
	Opening consensus.StateProof   `json:"opening"`
	Early   []partition.Checkpoint `json:"early"`
	Final   []partition.Checkpoint `json:"final"`
}

func Encode(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }
func Command(kind string, value any) []byte {
	return codec.Must(partition.Command{Type: kind, Value: codec.Must(value)})
}
func Block(height, timeMS string, txs ...[]byte) replica.Block {
	b := replica.Block{Height: height, TimeMS: timeMS, Transactions: []string{}}
	for _, tx := range txs {
		b.Transactions = append(b.Transactions, Encode(tx))
	}
	return b
}
func SignedHeader(trust consensus.Trust, height uint64, tm uint64, root string, signers int) (string, error) {
	return SignedHeaderData(trust, height, tm, root, signers, nil)
}
func SignedHeaderData(trust consensus.Trust, height uint64, tm uint64, root string, signers int, txs types.Txs) (string, error) {
	vs, e := trust.Set()
	if e != nil {
		return "", e
	}
	app, _ := hex.DecodeString(root)
	hash, _ := hex.DecodeString(codec.Hash("TEST_ONLY/parts", []any{trust.ChainID, fmt.Sprint(height)}))
	h := types.Header{Version: cmtversion.Consensus{Block: 11, App: 1}, ChainID: trust.ChainID, Height: int64(height), Time: time.UnixMilli(int64(tm)).UTC(), ValidatorsHash: vs.Hash(), NextValidatorsHash: vs.Hash(), ConsensusHash: types.DefaultConsensusParams().Hash(), AppHash: app, ProposerAddress: vs.Validators[0].Address}
	if txs != nil {
		h.DataHash = txs.Hash()
	}
	bid := types.BlockID{Hash: h.Hash(), PartSetHeader: types.PartSetHeader{Total: 1, Hash: hash}}
	commit := types.Commit{Height: int64(height), Round: 0, BlockID: bid, Signatures: []types.CommitSig{}}
	for index, val := range vs.Validators {
		sig := types.NewCommitSigAbsent()
		if index < signers {
			vote := types.Vote{Type: cmtproto.PrecommitType, Height: int64(height), Round: 0, BlockID: bid, Timestamp: h.Time, ValidatorAddress: val.Address, ValidatorIndex: int32(index)}
			for seat := 0; seat < 7; seat++ {
				key := network.TestKey(trust.ChainID, "consensus", seat)
				if hex.EncodeToString(key.PubKey().Address()) == hex.EncodeToString(val.Address) {
					signature, err := key.Sign(types.VoteSignBytes(trust.ChainID, vote.ToProto()))
					if err != nil {
						return "", err
					}
					vote.Signature = signature
					break
				}
			}
			sig = vote.CommitSig()
		}
		commit.Signatures = append(commit.Signatures, sig)
	}
	raw, e := cmtjson.Marshal(types.SignedHeader{Header: &h, Commit: &commit})
	return Encode(raw), e
}
func Proof(trust consensus.Trust, m replica.Machine, entries []state.Entry, table string, key any) (consensus.StateProof, error) {
	p, e := state.Prove(entries, table, key)
	if e != nil {
		return consensus.StateProof{}, e
	}
	header, e := SignedHeader(trust, m.Height()+1, m.Time()+1, m.Root(), 5)
	return consensus.StateProof{Height: fmt.Sprint(m.Height()), SignedHeader: header, Path: p}, e
}
func Certificate(chain string, r partition.BatchRef) []partition.Signature {
	out := []partition.Signature{}
	for i := 0; i < 3; i++ {
		k := network.TestKey(chain, "archive", i)
		s, _ := k.Sign(partition.ArchiveMessage(r))
		out = append(out, partition.Signature{Key: hex.EncodeToString(k.PubKey().Bytes()), Signature: hex.EncodeToString(s)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
func Generate() (Fixture, error) {
	var source struct {
		Cases []struct {
			Genesis json.RawMessage `json:"genesis"`
			Blocks  []core.Block    `json:"blocks"`
		} `json:"cases"`
	}
	raw, e := os.ReadFile("spec/fixtures/v0.4/replay-source.json")
	if e != nil {
		return Fixture{}, e
	}
	if e = json.Unmarshal(raw, &source); e != nil {
		return Fixture{}, e
	}
	var crypto struct {
		Cases []struct {
			Tally string `json:"tally_bundle"`
		} `json:"cases"`
	}
	raw, e = os.ReadFile("spec/fixtures/v0.4/crypto.json")
	if e != nil {
		return Fixture{}, e
	}
	if e = json.Unmarshal(raw, &crypto); e != nil {
		return Fixture{}, e
	}
	var ballots struct {
		Ballots []electionguard.ScaleEnvelope `json:"ballots"`
		Attack  electionguard.ScaleEnvelope   `json:"copy_attack"`
	}
	raw, e = os.ReadFile("spec/fixtures/v0.5/ballots.json")
	if e != nil {
		return Fixture{}, e
	}
	if e = json.Unmarshal(raw, &ballots); e != nil {
		return Fixture{}, e
	}
	gen, e := core.New(source.Cases[0].Genesis, 2)
	if e != nil {
		return Fixture{}, e
	}
	chain := gen.Config.ChainID
	lanes := []consensus.Trust{}
	archives := []string{}
	for i := 0; i < 8; i++ {
		lanes = append(lanes, network.Trust(fmt.Sprintf("%s-v%02x", chain, i)))
	}
	for i := 0; i < 5; i++ {
		archives = append(archives, hex.EncodeToString(network.TestKey(chain, "archive", i).PubKey().Bytes()))
	}
	sort.Strings(archives)
	var ledger core.Config
	if e = json.Unmarshal(source.Cases[0].Genesis, &ledger); e != nil {
		return Fixture{}, e
	}
	ledger.VotingProfile = "transport_dev_v0_5"
	ledger.Eligibility.CircuitDepth = "26"
	ledger.Eligibility.SuiteID = "semaphore-v4.14.5-depth-26"
	ledger.Governance.EligibilitySuite = ledger.Eligibility.SuiteID
	var lock struct {
		Depths []struct {
			Depth string `json:"depth"`
			WASM  struct {
				SHA string `json:"sha256"`
			} `json:"wasm"`
			ZKey struct {
				SHA string `json:"sha256"`
			} `json:"zkey"`
			VK struct {
				SHA string `json:"sha256"`
			} `json:"verification_key"`
		} `json:"depths"`
	}
	raw, e = spec.Crypto.ReadFile("crypto/semaphore-v4.lock.json")
	if e != nil {
		return Fixture{}, e
	}
	if e = json.Unmarshal(raw, &lock); e != nil {
		return Fixture{}, e
	}
	for i := range ledger.Registry.Modules {
		module := &ledger.Registry.Modules[i]
		if module.ID != "semaphore-v4.14.5-depth-16" {
			continue
		}
		next := *module
		module.Status = "legacy_verify_only"
		next.ID = ledger.Eligibility.SuiteID
		next.Parameters.Format = next.ID
		next.Artifacts = []string{}
		for _, d := range lock.Depths {
			if d.Depth == "26" {
				next.Artifacts = []string{d.WASM.SHA, d.ZKey.SHA, d.VK.SHA}
			}
		}
		ledger.Registry.Modules = append(ledger.Registry.Modules, next)
		break
	}
	cfg := partition.MainConfig{Ledger: codec.Must(ledger), Lanes: lanes, ArchiveKeys: archives, Depth: "26", Profile: "transport_dev_v0_5"}
	main, e := partition.NewMain(codec.Must(cfg), 2)
	if e != nil {
		return Fixture{}, e
	}
	f := Fixture{Label: "PUBLIC_TEST_KEYS_REAL_CRYPTO_SYNTHETIC_SIGNED_COMMITS_NATIVE_FINALITY_TESTED_SEPARATELY", Main: Run{"coordinator", codec.Must(cfg), []replica.Block{}, []string{}}, Lanes: []Run{}, Early: []partition.Checkpoint{}, Final: []partition.Checkpoint{}}
	addMain := func(b replica.Block) error {
		next, e := main.Apply(b)
		if e != nil {
			return e
		}
		main = next.(*partition.Main)
		f.Main.Blocks = append(f.Main.Blocks, b)
		f.Main.Roots = append(f.Main.Roots, main.Root())
		return nil
	}
	for _, b := range source.Cases[0].Blocks[:5] {
		txs := [][]byte{}
		for _, tx := range b.Transactions {
			txs = append(txs, codec.Must(tx))
		}
		if e = addMain(Block(b.Height, b.TimeMS, txs...)); e != nil {
			return Fixture{}, e
		}
	}
	for _, p := range main.Polls {
		f.Frozen = p
	}
	id := f.Frozen.Context.PollID
	f.Opening, e = Proof(network.Trust(chain), main, main.Entries(), "transport_polls", id)
	if e != nil {
		return Fixture{}, e
	}
	envelopes := append([]electionguard.ScaleEnvelope{ballots.Attack}, ballots.Ballots...)
	for i := range envelopes {
		envelopes[i].Checkpoint = f.Frozen.Context.Checkpoint
	}
	for i, trust := range lanes {
		c := partition.Config{NetworkID: f.Frozen.Context.NetworkID, ChainID: trust.ChainID, Lane: fmt.Sprint(i), Epoch: "1", Source: network.Trust(chain)}
		m, e := partition.New(codec.Must(c), 2)
		if e != nil {
			return Fixture{}, e
		}
		run := Run{"partition", codec.Must(c), []replica.Block{}, []string{}}
		apply := func(tm uint64, txs ...[]byte) error {
			b := Block(fmt.Sprint(m.Height()+1), fmt.Sprint(tm), txs...)
			next, e := m.Apply(b)
			if e != nil {
				return e
			}
			m = next.(*partition.Machine)
			run.Blocks = append(run.Blocks, b)
			run.Roots = append(run.Roots, m.Root())
			return nil
		}
		opens, _ := codec.Uint(f.Frozen.Opens)
		closes, _ := codec.Uint(f.Frozen.Closes)
		if e = apply(opens+1, Command("partition.open", f.Opening)); e != nil {
			return Fixture{}, e
		}
		txs := [][]byte{}
		start := 0
		for _, env := range envelopes {
			if env.Lane != c.Lane {
				continue
			}
			encoded := []string{Encode(codec.Must(env))}
			r, e := partition.Reference(f.Frozen.Context, c.Lane, fmt.Sprint(start), encoded)
			if e != nil {
				return Fixture{}, e
			}
			txs = append(txs, Command("partition.admit", partition.Batch{PollID: id, Envelopes: encoded, Certificate: Certificate(chain, r)}))
			start++
		}
		if e = apply(opens+2, txs...); e != nil {
			return Fixture{}, e
		}
		proof, e := Proof(trust, m, m.Entries(), "lane_checkpoint", id)
		if e != nil {
			return Fixture{}, e
		}
		early, e := m.Checkpoint(id, proof, nil)
		if e != nil {
			return Fixture{}, e
		}
		verified, e := partition.VerifyCheckpoint(f.Frozen, early, nil)
		if e != nil {
			return Fixture{}, e
		}
		f.Early = append(f.Early, early)
		for left := start; left > 0; {
			n := min(left, partition.VerificationBudget)
			if e = apply(opens+3+uint64(m.Height()), Command("partition.process", partition.Process{PollID: id, Count: fmt.Sprint(n)})); e != nil {
				return Fixture{}, e
			}
			left -= n
		}
		if e = apply(closes); e != nil {
			return Fixture{}, e
		}
		proof, e = Proof(trust, m, m.Entries(), "lane_checkpoint", id)
		if e != nil {
			return Fixture{}, e
		}
		final, e := m.Checkpoint(id, proof, verified)
		if e != nil {
			return Fixture{}, e
		}
		if _, e = partition.VerifyCheckpoint(f.Frozen, final, verified); e != nil {
			return Fixture{}, e
		}
		f.Final = append(f.Final, final)
		f.Lanes = append(f.Lanes, run)
	}
	earlyTxs, finalTxs := [][]byte{}, [][]byte{}
	for _, cp := range f.Early {
		earlyTxs = append(earlyTxs, Command("lane.checkpoint", cp))
	}
	for _, cp := range f.Final {
		finalTxs = append(finalTxs, Command("lane.checkpoint", cp))
	}
	opens, _ := codec.Uint(f.Frozen.Opens)
	closes, _ := codec.Uint(f.Frozen.Closes)
	if e = addMain(Block("6", fmt.Sprint(opens+20), earlyTxs...)); e != nil {
		return Fixture{}, e
	}
	if e = addMain(Block("7", fmt.Sprint(closes+20), finalTxs...)); e != nil {
		return Fixture{}, e
	}
	if e = addMain(Block("8", fmt.Sprint(closes+21), Command("partition.tally", partition.ResultRequest{PollID: id, Tally: crypto.Cases[0].Tally}))); e != nil {
		return Fixture{}, e
	}
	result := main.Results[id]
	if result.Yes != "3" || result.No != "2" || result.Count != "5" || result.State != "provisional" {
		return Fixture{}, fmt.Errorf("unexpected global result")
	}
	return f, nil
}
