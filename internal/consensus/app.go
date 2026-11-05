// Package consensus implements ABCI++ without exposing speculative state.
package consensus

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/replica"
	"civora/internal/state"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	abci "github.com/cometbft/cometbft/abci/types"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Application struct {
	abci.BaseApplication
	mu        sync.Mutex
	journal   *replica.Journal
	chain     string
	genesis   []byte
	pending   *replica.Record
	candidate replica.Machine
	poisoned  bool
}

var _ abci.Application = (*Application)(nil)

func New(j *replica.Journal, chain string, appState []byte) *Application {
	return &Application{journal: j, chain: chain, genesis: append([]byte{}, appState...)}
}
func (a *Application) Info(context.Context, *abci.RequestInfo) (*abci.ResponseInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.poisoned {
		return nil, fmt.Errorf("restart required")
	}
	h, _ := hex.DecodeString(a.journal.Machine.Root())
	return &abci.ResponseInfo{Version: "civora-0.5-development", AppVersion: 1, LastBlockHeight: int64(a.journal.Machine.Height()), LastBlockAppHash: h}, nil
}
func (a *Application) InitChain(_ context.Context, r *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.ChainId != a.chain || r.InitialHeight != 1 || !bytes.Equal(r.AppStateBytes, a.genesis) {
		return nil, fmt.Errorf("InitChain context mismatch")
	}
	h, _ := hex.DecodeString(a.journal.Machine.Root())
	return &abci.ResponseInitChain{AppHash: h}, nil
}
func (a *Application) CheckTx(_ context.Context, r *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.poisoned {
		return nil, fmt.Errorf("restart required")
	}
	if len(r.Tx) > 256*1024 {
		return &abci.ResponseCheckTx{Code: 1}, nil
	}
	if e := a.journal.Machine.Check(r.Tx); e != nil {
		return &abci.ResponseCheckTx{Code: 1, Log: "invalid transaction", Codespace: "civora"}, nil
	}
	return &abci.ResponseCheckTx{GasWanted: 1}, nil
}
func block(m replica.Machine, height int64, t time.Time, txs [][]byte) (replica.Block, error) {
	b := replica.Block{Height: strconv.FormatInt(height, 10), Transactions: []string{}}
	if height <= 0 || t.Year() < 1970 || t.Year() > 9999 {
		return b, fmt.Errorf("invalid height/time")
	}
	tm := uint64(t.UnixMilli())
	if tm < m.Time() {
		tm = m.Time()
	}
	b.TimeMS = fmt.Sprint(tm)
	var size int
	for _, tx := range txs {
		size += len(tx)
		if len(tx) > 256*1024 || size > replica.MaxBlockBytes {
			return b, fmt.Errorf("block budget")
		}
		b.Transactions = append(b.Transactions, codecBlob(tx))
	}
	if len(codec.Must(b)) > 3*1024*1024 {
		return b, fmt.Errorf("block journal budget")
	}
	return b, nil
}
func codecBlob(raw []byte) string { return encode(raw) }
func (a *Application) PrepareProposal(_ context.Context, r *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.poisoned || a.pending != nil {
		return nil, fmt.Errorf("application not ready")
	}
	m := a.journal.Machine
	selected := [][]byte{}
	b, e := block(m, r.Height, r.Time, selected)
	if e != nil {
		return nil, e
	}
	if _, e = m.Apply(b); e != nil {
		return nil, e
	}
	var size int64
	for _, tx := range r.Txs {
		if len(tx) > 256*1024 || size+int64(len(tx)) > r.MaxTxBytes {
			continue
		}
		candidate := append(append([][]byte{}, selected...), tx)
		b, e := block(m, r.Height, r.Time, candidate)
		if e != nil {
			continue
		}
		if _, e = m.Apply(b); e != nil {
			continue
		}
		selected = candidate
		size += int64(len(tx))
	}
	return &abci.ResponsePrepareProposal{Txs: selected}, nil
}
func (a *Application) ProcessProposal(_ context.Context, r *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.poisoned {
		return nil, fmt.Errorf("restart required")
	}
	b, e := block(a.journal.Machine, r.Height, r.Time, r.Txs)
	if e == nil {
		_, e = a.journal.Machine.Apply(b)
	}
	if e != nil {
		if strings.Contains(e.Error(), "ERR_UNSUPPORTED_EPOCH") {
			return nil, e
		}
		return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}, nil
	}
	return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
}
func response(r *replica.Record, m replica.Machine) *abci.ResponseFinalizeBlock {
	h, _ := hex.DecodeString(r.AppHash)
	out := &abci.ResponseFinalizeBlock{AppHash: h, TxResults: []*abci.ExecTxResult{}}
	if p, ok := m.(interface{ Events() []abci.Event }); ok {
		out.Events = p.Events()
	}
	if p, ok := m.(interface{ ValidatorUpdates() []abci.ValidatorUpdate }); ok {
		out.ValidatorUpdates = p.ValidatorUpdates()
	}
	for range r.Block.Transactions {
		out.TxResults = append(out.TxResults, &abci.ExecTxResult{GasWanted: 1, GasUsed: 1})
	}
	return out
}
func (a *Application) FinalizeBlock(_ context.Context, r *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.poisoned || len(r.Hash) != 32 {
		return nil, fmt.Errorf("invalid finalization")
	}
	identity := replica.Identity{Hash: hex.EncodeToString(r.Hash), Time: r.Time.UTC().Format(time.RFC3339Nano)}
	for _, old := range []*replica.Record{a.pending, a.journal.Last} {
		if old == nil || old.Block.Height != fmt.Sprint(r.Height) {
			continue
		}
		b, e := block(a.journal.Machine, r.Height, r.Time, r.Txs)
		if e != nil || old.Identity != identity || !bytes.Equal(codec.Must(old.Block), codec.Must(b)) {
			return nil, fmt.Errorf("conflicting decided-block replay")
		}
		a.pending = old
		m := a.candidate
		if old.Block.Height == fmt.Sprint(a.journal.Machine.Height()) {
			m = a.journal.Machine
		}
		return response(old, m), nil
	}
	if a.pending != nil {
		return nil, fmt.Errorf("previous block uncommitted")
	}
	b, e := block(a.journal.Machine, r.Height, r.Time, r.Txs)
	if e != nil {
		return nil, e
	}
	next, e := a.journal.Machine.Apply(b)
	if e != nil {
		return nil, e
	}
	a.pending = &replica.Record{Block: b, Identity: identity, State: next.State(), AppHash: next.Root()}
	a.candidate = next
	return response(a.pending, a.candidate), nil
}
func (a *Application) Commit(context.Context, *abci.RequestCommit) (*abci.ResponseCommit, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.poisoned || a.pending == nil {
		return nil, fmt.Errorf("Commit without ready finalization")
	}
	if a.pending.Block.Height != fmt.Sprint(a.journal.Machine.Height()) {
		a.poisoned = true
		if e := a.journal.Commit(*a.pending, a.candidate); e != nil {
			return nil, e
		}
		a.poisoned = false
	}
	a.pending = nil
	a.candidate = nil
	return &abci.ResponseCommit{}, nil
}
func (a *Application) Query(_ context.Context, r *abci.RequestQuery) (*abci.ResponseQuery, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := int64(a.journal.Machine.Height())
	if !a.poisoned && r.Path == "/proof" && !r.Prove && (r.Height == 0 || r.Height == h) && len(r.Data) <= 4096 {
		var key struct {
			Table string `json:"table"`
			Key   any    `json:"key"`
		}
		v, e := codec.Parse(r.Data)
		if e == nil {
			_ = v
			e = json.Unmarshal(r.Data, &key)
		}
		provider, ok := a.journal.Machine.(interface{ Entries() []state.Entry })
		if e == nil && ok {
			p, err := state.Prove(provider.Entries(), key.Table, key.Key)
			if err == nil {
				return &abci.ResponseQuery{Height: h, Value: codec.Must(p)}, nil
			}
		}
		return &abci.ResponseQuery{Code: 1, Height: h, Log: "state proof unavailable"}, nil
	}
	if a.poisoned || r.Prove || r.Path != "/state" || r.Height != 0 && r.Height != h {
		return &abci.ResponseQuery{Code: 1, Height: h, Log: "unsupported query/proof"}, nil
	}
	return &abci.ResponseQuery{Height: h, Value: a.journal.Machine.State()}, nil
}
func (a *Application) OfferSnapshot(context.Context, *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	return &abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT}, nil
}
func (a *Application) ApplySnapshotChunk(context.Context, *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	return &abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ABORT}, nil
}
