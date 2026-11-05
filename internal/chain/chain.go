// Package chain verifies an archive sequentially from authenticated genesis.
package chain

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/network"
	"civora/internal/replica"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/types"
	"time"
)

type Genesis struct {
	Raw         []byte
	Doc         *types.GenesisDoc
	Application network.Genesis
	Initial     consensus.Trust
}
type Bundle struct {
	Block        string `json:"block"`
	SignedHeader string `json:"signed_header"`
}
type Provider interface {
	Height() uint64
	Bundle(uint64) (Bundle, error)
}

func ParseGenesis(raw []byte) (*Genesis, error) {
	if len(raw) > 8*1024*1024 {
		return nil, fmt.Errorf("genesis capacity")
	}
	var value types.GenesisDoc
	if e := cmtjson.Unmarshal(raw, &value); e != nil {
		return nil, e
	}
	if value.GenesisTime.IsZero() || value.InitialHeight != 1 || value.ConsensusParams == nil {
		return nil, fmt.Errorf("explicit native genesis required")
	}
	if e := value.ValidateAndComplete(); e != nil {
		return nil, e
	}
	doc := &value
	var e error
	if doc.InitialHeight != 1 || len(doc.Validators) != 7 || doc.ConsensusParams.Block.MaxBytes != replica.MaxBlockBytes || doc.ConsensusParams.ABCI.VoteExtensionsEnableHeight != 0 {
		return nil, fmt.Errorf("unsupported native genesis parameters")
	}
	trust := consensus.Trust{ChainID: doc.ChainID, Keys: []string{}}
	for _, v := range doc.Validators {
		if v.Power != 1 || v.PubKey.Type() != "ed25519" {
			return nil, fmt.Errorf("unit Ed25519 validators required")
		}
		trust.Keys = append(trust.Keys, hex.EncodeToString(v.PubKey.Bytes()))
	}
	if _, e = trust.Set(); e != nil {
		return nil, e
	}
	var app network.Genesis
	if e = codec.Decode(doc.AppState, "network_genesis", &app); e != nil {
		return nil, e
	}
	g := &Genesis{append([]byte{}, raw...), doc, app, trust}
	if _, e = g.Machine(); e != nil {
		return nil, e
	}
	return g, nil
}
func (g *Genesis) Machine() (replica.Machine, error) {
	return network.MachineWithTrust(g.Application, 2, g.Initial)
}
func (g *Genesis) TrustAt(h uint64) consensus.Trust {
	t := g.Initial
	for _, r := range g.Application.Rotations {
		height, _ := codec.Uint(r.Height)
		if h >= height+2 {
			t.Keys = r.Keys
		}
	}
	return t
}
func Encode(block *types.Block, commit *types.Commit) (Bundle, error) {
	if block == nil || commit == nil {
		return Bundle{}, fmt.Errorf("finalized block unavailable")
	}
	raw, e := cmtjson.Marshal(block)
	if e != nil {
		return Bundle{}, e
	}
	header, e := cmtjson.Marshal(types.SignedHeader{Header: &block.Header, Commit: commit})
	return Bundle{base64.RawURLEncoding.EncodeToString(raw), base64.RawURLEncoding.EncodeToString(header)}, e
}
func Decode(b Bundle) (*types.Block, error) {
	raw, e := codec.Blob(b.Block)
	if e != nil || len(raw) > 4*1024*1024 {
		return nil, fmt.Errorf("native block capacity")
	}
	var block types.Block
	if e = cmtjson.Unmarshal(raw, &block); e != nil {
		return nil, e
	}
	if e = block.ValidateBasic(); e != nil {
		return nil, e
	}
	return &block, nil
}

type Verifier struct {
	Genesis  *Genesis
	Machine  replica.Machine
	LastID   types.BlockID
	LastTime time.Time
}

func New(g *Genesis) (*Verifier, error) {
	m, e := g.Machine()
	return &Verifier{Genesis: g, Machine: m}, e
}
func (v *Verifier) Apply(b Bundle) error {
	block, e := Decode(b)
	if e != nil {
		return e
	}
	height := v.Machine.Height() + 1
	if block.Height != int64(height) {
		return fmt.Errorf("noncontiguous block")
	}
	sh, e := consensus.VerifyHeader(v.Genesis.TrustAt(height), b.SignedHeader)
	if e != nil {
		return e
	}
	if !bytes.Equal(sh.Hash(), block.Hash()) {
		return fmt.Errorf("header/body mismatch")
	}
	if !bytes.Equal(block.AppHash, mustHex(v.Machine.Root())) {
		return fmt.Errorf("prior AppHash mismatch at %d", height)
	}
	nextSet, e := v.Genesis.TrustAt(height + 1).Set()
	if e != nil {
		return e
	}
	if !bytes.Equal(block.NextValidatorsHash, nextSet.Hash()) || !bytes.Equal(block.ConsensusHash, v.Genesis.Doc.ConsensusParams.Hash()) {
		return fmt.Errorf("validator/parameter transition mismatch")
	}
	if height == 1 {
		if !block.Time.Equal(v.Genesis.Doc.GenesisTime) || !block.LastBlockID.IsZero() {
			return fmt.Errorf("first block genesis mismatch")
		}
	} else {
		if !block.LastBlockID.Equals(v.LastID) || !block.Time.After(v.LastTime) {
			return fmt.Errorf("block link/time mismatch")
		}
	}
	if block.Time.Year() < 1970 || block.Time.Year() > 9999 || len(block.Data.Txs) > 8192 {
		return fmt.Errorf("block time/transaction bound")
	}
	tm := uint64(block.Time.UnixMilli())
	if tm < v.Machine.Time() {
		tm = v.Machine.Time()
	}
	r := replica.Block{Height: fmt.Sprint(height), TimeMS: fmt.Sprint(tm), Transactions: []string{}}
	size := 0
	for _, tx := range block.Data.Txs {
		size += len(tx)
		if len(tx) > 256*1024 || size > replica.MaxBlockBytes {
			return fmt.Errorf("transaction budget")
		}
		r.Transactions = append(r.Transactions, base64.RawURLEncoding.EncodeToString(tx))
	}
	next, e := v.Machine.Apply(r)
	if e != nil {
		return e
	}
	v.Machine = next
	v.LastID = sh.Commit.BlockID
	v.LastTime = block.Time
	return nil
}
func mustHex(s string) []byte { b, _ := hex.DecodeString(s); return b }
