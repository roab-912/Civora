// Package network runs a loopback-only, reproducible CometBFT development network.
package network

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/core"
	"civora/internal/partition"
	"civora/internal/replica"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	cfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cometbft/cometbft/types"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Settings struct {
	Label         string   `json:"label"`
	ChainID       string   `json:"chain_id"`
	GenesisSHA256 string   `json:"genesis_sha256"`
	Seat          string   `json:"seat"`
	Port          string   `json:"port"`
	Peers         []string `json:"peers"`
}
type Genesis struct {
	Kind      string               `json:"kind"`
	Data      json.RawMessage      `json:"data"`
	Rotations []consensus.Rotation `json:"consensus_rotations,omitempty"`
}

func TestKey(chain, role string, seat int) ed25519.PrivKey {
	s := sha256.Sum256([]byte(fmt.Sprintf("CIVORA/v0.5/TEST_ONLY/%s/%s/%d", chain, role, seat)))
	return ed25519.GenPrivKeyFromSecret(s[:])
}
func Trust(chain string) consensus.Trust {
	t := consensus.Trust{ChainID: chain, Keys: []string{}}
	for i := 0; i < 7; i++ {
		t.Keys = append(t.Keys, hex.EncodeToString(TestKey(chain, "consensus", i).PubKey().Bytes()))
	}
	return t
}
func Init(directory, chain string, app Genesis, port int) error {
	if port < 1024 || port+13 > 65535 {
		return fmt.Errorf("development port range")
	}
	if _, e := Machine(app, 2); e != nil {
		return e
	}
	if expected, e := ApplicationChain(app); e != nil || expected != chain {
		return fmt.Errorf("application/consensus chain mismatch")
	}
	if _, e := Trust(chain).Set(); e != nil {
		return e
	}
	for seat := 0; seat < 7; seat++ {
		if _, e := generationKeys(app, chain, seat); e != nil {
			return e
		}
	}
	// Never replace an existing signer state, even for these public test keys.
	if e := os.Mkdir(directory, 0700); e != nil {
		return e
	}
	params := types.DefaultConsensusParams()
	params.Block.MaxBytes = replica.MaxBlockBytes
	params.Block.MaxGas = -1
	params.ABCI.VoteExtensionsEnableHeight = 0
	vals := []types.GenesisValidator{}
	peers := []string{}
	for i := 0; i < 7; i++ {
		k := TestKey(chain, "consensus", i)
		vals = append(vals, types.GenesisValidator{Address: k.PubKey().Address(), PubKey: k.PubKey(), Power: 1, Name: fmt.Sprintf("TEST_ONLY-seat-%d", i)})
		nk := p2p.NodeKey{PrivKey: TestKey(chain, "p2p", i)}
		peers = append(peers, fmt.Sprintf("%s@127.0.0.1:%d", nk.ID(), port+2*i))
	}
	g := types.GenesisDoc{GenesisTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ChainID: chain, InitialHeight: 1, ConsensusParams: params, Validators: vals, AppState: codec.Must(app)}
	raw, e := cmtjson.Marshal(g)
	if e != nil {
		return e
	}
	digest := sha256.Sum256(raw)
	for i := 0; i < 7; i++ {
		home := filepath.Join(directory, fmt.Sprintf("node%d", i))
		for _, d := range []string{"config", "data"} {
			if e = os.MkdirAll(filepath.Join(home, d), 0700); e != nil {
				return e
			}
		}
		if e = os.WriteFile(filepath.Join(home, "config", "genesis.json"), raw, 0600); e != nil {
			return e
		}
		pv := privval.NewFilePV(TestKey(chain, "consensus", i), filepath.Join(home, "config", "priv_validator_key.json"), filepath.Join(home, "data", "priv_validator_state.json"))
		pv.Save()
		keys, e := generationKeys(app, chain, i)
		if e != nil {
			return e
		}
		for _, k := range keys[1:] {
			keyPath, statePath := generationPaths(home, hex.EncodeToString(k.PubKey().Bytes()))
			next := privval.NewFilePV(k, keyPath, statePath)
			next.Save()
		}
		nk := p2p.NodeKey{PrivKey: TestKey(chain, "p2p", i)}
		if e = nk.SaveAs(filepath.Join(home, "config", "node_key.json")); e != nil {
			return e
		}
		other := []string{}
		for j, p := range peers {
			if j != i {
				other = append(other, p)
			}
		}
		settings := Settings{"PUBLIC_TEST_KEYS_ONLY", chain, hex.EncodeToString(digest[:]), fmt.Sprint(i), fmt.Sprint(port + 2*i), other}
		if e = os.WriteFile(filepath.Join(home, "development.json"), codec.Must(settings), 0600); e != nil {
			return e
		}
	}
	return os.WriteFile(filepath.Join(directory, "README.TEST_ONLY.txt"), []byte("PUBLIC DETERMINISTIC PRIVATE KEYS: DEVELOPMENT ONLY. Never reuse these identities on another network. Never reset priv_validator_state.json.\n"), 0600)
}
func baseMachine(g Genesis, epoch uint64) (replica.Machine, error) {
	if g.Kind == "coordinator" {
		return partition.NewMain(g.Data, epoch)
	}
	if g.Kind == "partition" {
		return partition.New(g.Data, epoch)
	}
	if g.Kind != "ledger" {
		return nil, fmt.Errorf("unknown machine kind")
	}
	e, err := core.New(g.Data, epoch)
	if err != nil {
		return nil, err
	}
	return &consensus.Ledger{Engine: e}, nil
}
func ApplicationChain(g Genesis) (string, error) {
	var c struct {
		Chain  string `json:"chain_id"`
		Ledger struct {
			Chain string `json:"chain_id"`
		} `json:"ledger"`
	}
	if e := json.Unmarshal(g.Data, &c); e != nil {
		return "", e
	}
	if g.Kind == "coordinator" {
		return c.Ledger.Chain, nil
	}
	return c.Chain, nil
}
func Machine(g Genesis, epoch uint64) (replica.Machine, error) {
	chain, e := ApplicationChain(g)
	if e != nil {
		return nil, e
	}
	return MachineWithTrust(g, epoch, Trust(chain))
}
func MachineWithTrust(g Genesis, epoch uint64, initial consensus.Trust) (replica.Machine, error) {
	if epoch < 1 || epoch > 2 {
		return nil, fmt.Errorf("implemented epoch capability is 1 or 2")
	}
	base, e := baseMachine(g, epoch)
	if e != nil {
		return nil, e
	}
	chain, e := ApplicationChain(g)
	if e != nil {
		return nil, e
	}
	if initial.ChainID != chain {
		return nil, fmt.Errorf("native/application chain mismatch")
	}
	if main, ok := base.(*partition.Main); ok && main.Config.Audit != nil {
		keys := append([]string{}, initial.Keys...)
		for _, r := range g.Rotations {
			keys = append(keys, r.Keys...)
			for _, guardian := range r.Guardians {
				keys = append(keys, guardian.SigningPK)
			}
		}
		if e = main.Config.Audit.Validate(keys); e != nil {
			return nil, e
		}
	}
	return consensus.WithRotations(base, initial, g.Rotations)
}
func Start(home string, epoch uint64, logger log.Logger) (*node.Node, func(), error) {
	return StartWithSigner(home, epoch, 0, logger)
}
func StartWithSigner(home string, epoch, generation uint64, logger log.Logger) (*node.Node, func(), error) {
	raw, e := os.ReadFile(filepath.Join(home, "development.json"))
	if e != nil {
		return nil, nil, e
	}
	var settings Settings
	if e = codec.Decode(raw, "network_settings", &settings); e != nil {
		return nil, nil, e
	}
	// bbolt supplies a cross-process exclusive lock before opening keys or journals.
	lock, e := bolt.Open(filepath.Join(home, "data", "application.lock"), 0600, &bolt.Options{Timeout: time.Second})
	if e != nil {
		return nil, nil, e
	}
	cleanup := func() { _ = lock.Close() }
	fail := func(e error) (*node.Node, func(), error) { cleanup(); return nil, nil, e }
	c := cfg.DefaultConfig().SetRoot(home)
	p, e := codec.Uint(settings.Port)
	if e != nil {
		return fail(e)
	}
	c.Moniker = "TEST_ONLY-seat-" + settings.Seat
	c.P2P.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", p)
	c.P2P.PersistentPeers = strings.Join(settings.Peers, ",")
	c.P2P.AllowDuplicateIP = true
	c.P2P.AddrBookStrict = false
	c.RPC.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", p+1)
	c.Consensus.TimeoutCommit = 500 * time.Millisecond
	c.Consensus.CreateEmptyBlocks = true
	c.P2P.PersistentPeersMaxDialPeriod = time.Second
	if e = c.ValidateBasic(); e != nil {
		return fail(e)
	}
	full, e := os.ReadFile(c.GenesisFile())
	if e != nil {
		return fail(e)
	}
	digest := sha256.Sum256(full)
	if hex.EncodeToString(digest[:]) != settings.GenesisSHA256 {
		return fail(fmt.Errorf("genesis fingerprint mismatch"))
	}
	g, e := types.GenesisDocFromJSON(full)
	if e != nil {
		return fail(e)
	}
	if g.ChainID != settings.ChainID || g.InitialHeight != 1 || len(g.Validators) != 7 || g.ConsensusParams.Block.MaxBytes != replica.MaxBlockBytes || g.ConsensusParams.ABCI.VoteExtensionsEnableHeight != 0 {
		return fail(fmt.Errorf("development consensus parameters mismatch"))
	}
	t := Trust(settings.ChainID)
	vs, e := t.Set()
	if e != nil {
		return fail(e)
	}
	gv := []*types.Validator{}
	for _, v := range g.Validators {
		if v.Power != 1 {
			return fail(fmt.Errorf("unit power required"))
		}
		gv = append(gv, types.NewValidator(v.PubKey, v.Power))
	}
	if !bytes.Equal(types.NewValidatorSet(gv).Hash(), vs.Hash()) {
		return fail(fmt.Errorf("development roster mismatch"))
	}
	var appGenesis Genesis
	if e = codec.Decode(g.AppState, "network_genesis", &appGenesis); e != nil {
		return fail(e)
	}
	if chain, e := ApplicationChain(appGenesis); e != nil || chain != g.ChainID {
		return fail(fmt.Errorf("application/consensus chain mismatch"))
	}
	seat, e := codec.Uint(settings.Seat)
	if e != nil || seat > 6 {
		return fail(fmt.Errorf("signer seat"))
	}
	expected, e := expectedSigner(appGenesis, g.ChainID, int(seat), generation)
	if e != nil {
		return fail(e)
	}
	keyPath, statePath := c.PrivValidatorKeyFile(), c.PrivValidatorStateFile()
	if expected != t.Keys[seat] {
		keyPath, statePath = generationPaths(home, expected)
	}
	j, e := replica.Open(filepath.Join(home, "data", "application"), full, func() (replica.Machine, error) { return Machine(appGenesis, epoch) })
	if e != nil {
		return fail(e)
	}
	// Refuse missing signer state before LoadFilePV (which otherwise exits).
	// Never use LoadOrGenFilePV or LoadFilePVEmptyState on restart.
	for _, p := range []string{keyPath, statePath, c.NodeKeyFile()} {
		if st, e := os.Stat(p); e != nil || st.Size() == 0 {
			return fail(fmt.Errorf("missing signer/P2P state: %s", p))
		}
	}
	pv := privval.LoadFilePV(keyPath, statePath)
	pk, e := pv.GetPubKey()
	if e != nil {
		return fail(e)
	}
	if hex.EncodeToString(pk.Bytes()) != expected {
		return fail(fmt.Errorf("signer seat mismatch"))
	}
	nk, e := p2p.LoadNodeKey(c.NodeKeyFile())
	if e != nil {
		return fail(e)
	}
	app := consensus.New(j, g.ChainID, g.AppState)
	n, e := node.NewNodeWithContext(context.Background(), c, pv, nk, proxy.NewLocalClientCreator(app), node.DefaultGenesisDocProviderFunc(c), cfg.DefaultDBProvider, node.DefaultMetricsProvider(c.Instrumentation), logger)
	if e != nil {
		return fail(e)
	}
	if e = n.Start(); e != nil {
		return fail(e)
	}
	return n, cleanup, nil
}
