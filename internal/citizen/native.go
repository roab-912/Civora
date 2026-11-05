package citizen

import (
	"civora/internal/chain"
	"civora/internal/consensus"
	"civora/internal/replica"
	"context"
	"fmt"
	cfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	proto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/proxy"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CometBFT 0.38 requests a public identity even for a non-validator. This port
// exposes the P2P PUBLIC key and has no consensus signing capability or secret.
type NoSigner struct{ Public crypto.PubKey }

func (s NoSigner) GetPubKey() (crypto.PubKey, error) { return s.Public, nil }
func (NoSigner) SignVote(string, *proto.Vote) error {
	return fmt.Errorf("observer cannot sign consensus votes")
}
func (NoSigner) SignProposal(string, *proto.Proposal) error {
	return fmt.Errorf("observer cannot sign consensus proposals")
}

type Native struct {
	Node    *node.Node
	App     *consensus.Application
	Genesis *chain.Genesis
	close   func()
}

func StartNative(home string, g *chain.Genesis, peers []string, port int, logger log.Logger) (*Native, error) {
	if port < 1024 || port > 65534 {
		return nil, fmt.Errorf("observer port")
	}
	for _, d := range []string{"config", "data"} {
		if e := os.MkdirAll(filepath.Join(home, d), 0700); e != nil {
			return nil, e
		}
	}
	lock, e := bolt.Open(filepath.Join(home, "data", "observer.lock"), 0600, &bolt.Options{Timeout: time.Second})
	if e != nil {
		return nil, e
	}
	fail := func(e error) (*Native, error) { _ = lock.Close(); return nil, e }
	path := filepath.Join(home, "config", "genesis.json")
	if saved, e := os.ReadFile(path); os.IsNotExist(e) {
		if e = replica.Persist(filepath.Dir(path), path, g.Raw, nil); e != nil {
			return fail(e)
		}
	} else if e != nil || string(saved) != string(g.Raw) {
		return fail(fmt.Errorf("immutable observer genesis mismatch"))
	}
	c := cfg.DefaultConfig().SetRoot(home)
	c.Moniker = "civora-public-observer"
	c.P2P.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", port)
	c.RPC.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", port+1)
	c.P2P.PersistentPeers = strings.Join(peers, ",")
	c.P2P.AllowDuplicateIP = true
	c.P2P.AddrBookStrict = false
	c.P2P.PersistentPeersMaxDialPeriod = time.Second
	c.Consensus.CreateEmptyBlocks = true
	c.Consensus.TimeoutCommit = 500 * time.Millisecond
	if e = c.ValidateBasic(); e != nil {
		return fail(e)
	}
	nk, e := p2p.LoadOrGenNodeKey(c.NodeKeyFile())
	if e != nil {
		return fail(e)
	}
	for _, key := range g.Initial.Keys {
		if fmt.Sprintf("%x", nk.PrivKey.PubKey().Bytes()) == key {
			return fail(fmt.Errorf("P2P identity overlaps validator identity"))
		}
	}
	j, e := replica.Open(filepath.Join(home, "data", "application"), g.Raw, g.Machine)
	if e != nil {
		return fail(e)
	}
	app := consensus.New(j, g.Doc.ChainID, g.Doc.AppState)
	n, e := node.NewNodeWithContext(context.Background(), c, NoSigner{nk.PrivKey.PubKey()}, nk, proxy.NewLocalClientCreator(app), node.DefaultGenesisDocProviderFunc(c), cfg.DefaultDBProvider, node.DefaultMetricsProvider(c.Instrumentation), logger)
	if e != nil {
		return fail(e)
	}
	if e = n.Start(); e != nil {
		return fail(e)
	}
	return &Native{n, app, g, func() { _ = lock.Close() }}, nil
}
func (n *Native) Close()         { _ = n.Node.Stop(); n.Node.Wait(); n.close() }
func (n *Native) Height() uint64 { return uint64(n.Node.BlockStore().Height()) }
func (n *Native) Bundle(h uint64) (chain.Bundle, error) {
	block := n.Node.BlockStore().LoadBlock(int64(h))
	commit := n.Node.BlockStore().LoadSeenCommit(int64(h))
	if commit == nil {
		commit = n.Node.BlockStore().LoadBlockCommit(int64(h))
	}
	return chain.Encode(block, commit)
}
