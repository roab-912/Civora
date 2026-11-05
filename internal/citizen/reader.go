package citizen

import (
	"bytes"
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/core"
	"civora/internal/replica"
	"civora/internal/state"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/types"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type StateProof = consensus.WitnessProof
type Event struct {
	Height     string            `json:"height"`
	Position   string            `json:"position"`
	Type       string            `json:"type"`
	Attributes map[string]string `json:"attributes"`
}
type Transaction struct {
	ID       string `json:"tx_id"`
	Status   string `json:"status"`
	Height   string `json:"height"`
	Position string `json:"position"`
	Bytes    string `json:"bytes"`
}
type View struct {
	Height       string          `json:"height"`
	Time         string          `json:"time_ms"`
	Root         string          `json:"app_hash"`
	Entries      []state.Entry   `json:"entries"`
	Events       []Event         `json:"events"`
	Transactions []Transaction   `json:"transactions"`
	Bundle       archive.Ref     `json:"bundle"`
	Machine      replica.Machine `json:"-"`
}
type Reader struct {
	mu               sync.RWMutex
	Genesis          *chain.Genesis
	Source           chain.Provider
	Journal          string
	Objects          archive.Store
	ArchiveDirectory string
	verifier         *chain.Verifier
	views            map[uint64]*View
	transactions     map[string]Transaction
	index            *bolt.DB
}

func NewReader(g *chain.Genesis, source chain.Provider, journal, indexDir string) (*Reader, error) {
	if e := os.MkdirAll(indexDir, 0700); e != nil {
		return nil, e
	}
	db, e := bolt.Open(filepath.Join(indexDir, "index.db"), 0600, &bolt.Options{Timeout: time.Second})
	if e != nil {
		return nil, e
	}
	v, e := chain.New(g)
	if e != nil {
		_ = db.Close()
		return nil, e
	}
	r := &Reader{Genesis: g, Source: source, Journal: journal, Objects: archive.Store{Directory: indexDir}, verifier: v, views: map[uint64]*View{}, transactions: map[string]Transaction{}, index: db}
	r.ArchiveDirectory = filepath.Join(indexDir, "archive")
	r.Source = localSource{reader: r, remote: source}
	e = db.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{"views", "transactions"} {
			_ = tx.DeleteBucket([]byte(name))
			if _, e := tx.CreateBucket([]byte(name)); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		_ = db.Close()
		return nil, e
	}
	return r, nil
}
func (r *Reader) Close() error { return r.index.Close() }
func TransactionID(raw []byte) string {
	var shape struct {
		Body json.RawMessage `json:"body"`
	}
	_ = json.Unmarshal(raw, &shape)
	if len(shape.Body) > 0 {
		var tx core.Transaction
		if codec.Decode(raw, "transaction", &tx) == nil {
			return core.TxID(tx.Body)
		}
	}
	return codec.Hash("transport/tx", json.RawMessage(raw))
}
func (r *Reader) Refresh() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	maximum := r.Source.Height()
	for r.verifier.Machine.Height() < maximum {
		h := r.verifier.Machine.Height() + 1
		var record replica.Record
		if r.Journal != "" {
			raw, e := os.ReadFile(filepath.Join(r.Journal, fmt.Sprintf("%020d.record.json", h)))
			if os.IsNotExist(e) {
				break
			}
			if e != nil {
				return e
			}
			if e = codec.Decode(raw, "replica_record", &record); e != nil {
				return e
			}
		}
		b, e := r.Source.Bundle(h)
		if e != nil {
			return fmt.Errorf("archive block %d: %w", h, e)
		}
		before := *r.verifier
		if e = r.verifier.Apply(b); e != nil {
			return e
		}
		m := r.verifier.Machine
		if r.Journal != "" && (record.AppHash != m.Root() || !bytes.Equal(record.State, m.State()) || record.Identity.Hash != hex.EncodeToString(r.verifier.LastID.Hash)) {
			*r.verifier = before
			return fmt.Errorf("native journal diverges from committed block")
		}
		object, e := r.Objects.Put(codec.Must(b))
		if e != nil {
			*r.verifier = before
			return e
		}
		if e = r.saveBlock(h, object); e != nil {
			*r.verifier = before
			return e
		}
		block, e := chain.Decode(b)
		if e != nil {
			return e
		}
		entries := m.(interface{ Entries() []state.Entry }).Entries()
		view := &View{fmt.Sprint(h), fmt.Sprint(m.Time()), m.Root(), entries, []Event{}, []Transaction{}, object, m}
		if p, ok := m.(interface{ Events() []abci.Event }); ok {
			for i, event := range p.Events() {
				attrs := map[string]string{}
				for _, a := range event.Attributes {
					attrs[a.Key] = a.Value
				}
				view.Events = append(view.Events, Event{fmt.Sprint(h), fmt.Sprint(i), event.Type, attrs})
			}
		}
		for i, raw := range block.Data.Txs {
			id := TransactionID(raw)
			tx := Transaction{id, "finalized", fmt.Sprint(h), fmt.Sprint(i), base64.RawURLEncoding.EncodeToString(raw)}
			view.Transactions = append(view.Transactions, tx)
		}
		if e = r.index.Update(func(tx *bolt.Tx) error {
			if e := tx.Bucket([]byte("views")).Put([]byte(fmt.Sprintf("%020d", h)), codec.Must(view)); e != nil {
				return e
			}
			for _, t := range view.Transactions {
				bucket := tx.Bucket([]byte("transactions"))
				if bucket.Get([]byte(t.ID)) == nil {
					if e := bucket.Put([]byte(t.ID), codec.Must(t)); e != nil {
						return e
					}
				}
			}
			return nil
		}); e != nil {
			*r.verifier = before
			return e
		}
		r.views[h] = view
		for _, t := range view.Transactions {
			if _, ok := r.transactions[t.ID]; !ok {
				r.transactions[t.ID] = t
			}
		}
	}
	return nil
}
func (r *Reader) Latest() *View {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.views[r.verifier.Machine.Height()]
}
func (r *Reader) At(h uint64) (*View, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v := r.views[h]
	if v == nil {
		return nil, fmt.Errorf("historical height unavailable")
	}
	return v, nil
}
func (r *Reader) FindTransaction(id string) (Transaction, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.transactions[id]
	return v, ok
}
func (r *Reader) StateProof(h uint64, table string, key any) (StateProof, error) {
	v, e := r.At(h)
	if e != nil {
		return StateProof{}, e
	}
	next, e := r.Source.Bundle(h + 1)
	if e != nil {
		return StateProof{}, fmt.Errorf("authenticating header pending")
	}
	header, e := consensus.VerifyHeader(r.Genesis.TrustAt(h+1), next.SignedHeader)
	if e != nil || header.Height != int64(h+1) || hex.EncodeToString(header.AppHash) != v.Root {
		return StateProof{}, fmt.Errorf("authenticating header mismatch")
	}
	p, e := state.WitnessFor(v.Entries, table, key)
	return StateProof{Height: fmt.Sprint(h), SignedHeader: next.SignedHeader, Path: p}, e
}
func (r *Reader) Membership(h uint64, table string, key any) (consensus.StateProof, error) {
	p, e := r.StateProof(h, table, key)
	if e != nil {
		return consensus.StateProof{}, e
	}
	if !p.Path.Exists {
		return consensus.StateProof{}, fmt.Errorf("entry absent")
	}
	siblings := append([]string{}, p.Path.Siblings...)
	for i, j := 0, 255; i < j; i, j = i+1, j-1 {
		siblings[i], siblings[j] = siblings[j], siblings[i]
	}
	return consensus.StateProof{Height: p.Height, SignedHeader: p.SignedHeader, Path: state.Proof{Entry: p.Path.Entry, Siblings: siblings}}, nil
}
func (r *Reader) TransactionProof(id string) (map[string]any, error) {
	t, ok := r.FindTransaction(id)
	if !ok {
		return nil, fmt.Errorf("transaction unknown")
	}
	h, _ := codec.Uint(t.Height)
	position, _ := strconv.Atoi(t.Position)
	b, e := r.Source.Bundle(h)
	if e != nil {
		return nil, e
	}
	block, e := chain.Decode(b)
	if e != nil {
		return nil, e
	}
	proof := block.Data.Txs.Proof(position)
	raw, e := cmtjson.Marshal(proof)
	return map[string]any{"tx_id": id, "height": t.Height, "signed_header": b.SignedHeader, "transaction_proof": base64.RawURLEncoding.EncodeToString(raw)}, e
}
func VerifyState(g *chain.Genesis, p StateProof) error {
	h, e := codec.Uint(p.Height)
	if e != nil || h == 0 {
		return fmt.Errorf("proof height")
	}
	sh, e := consensus.VerifyHeader(g.TrustAt(h+1), p.SignedHeader)
	if e != nil {
		return e
	}
	if sh.Height != int64(h+1) {
		return fmt.Errorf("AppHash offset")
	}
	return state.VerifyWitness(hex.EncodeToString(sh.AppHash), p.Path)
}
func VerifyTransaction(trust consensus.Trust, encodedHeader, encodedProof string) error {
	header, e := consensus.VerifyHeader(trust, encodedHeader)
	if e != nil {
		return e
	}
	raw, e := codec.Blob(encodedProof)
	if e != nil || len(raw) > 400*1024 {
		return fmt.Errorf("transaction proof size")
	}
	var p types.TxProof
	if e = cmtjson.Unmarshal(raw, &p); e != nil {
		return e
	}
	return p.Validate(header.DataHash)
}
