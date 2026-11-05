package partition

import (
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/electionguard"
	"civora/internal/replica"
	"civora/internal/semaphore"
	"civora/internal/state"
	"encoding/json"
	"fmt"
	abci "github.com/cometbft/cometbft/abci/types"
	"sort"
)

const AdmissionBudget = 32
const VerificationBudget = 4

type Config struct {
	NetworkID string          `json:"network_id"`
	ChainID   string          `json:"chain_id"`
	Lane      string          `json:"lane_id"`
	Epoch     string          `json:"protocol_epoch"`
	Source    consensus.Trust `json:"source"`
}
type Deposit struct {
	Envelope  string `json:"envelope"`
	Digest    string `json:"digest"`
	Status    string `json:"status"`
	Nullifier string `json:"nullifier"`
	Height    string `json:"height"`
	Batch     string `json:"batch_id"`
}
type StoredBatch struct {
	Reference   BatchRef    `json:"reference"`
	Certificate []Signature `json:"certificate"`
}
type Poll struct {
	Frozen      Frozen            `json:"frozen"`
	Deposits    []Deposit         `json:"deposits"`
	Batches     []StoredBatch     `json:"batches"`
	Processed   string            `json:"processed"`
	Counted     string            `json:"counted"`
	Aggregate   string            `json:"aggregate"`
	Spent       map[string]string `json:"spent"`
	Ballots     map[string]string `json:"ballots"`
	Ciphertexts map[string]string `json:"ciphertexts"`
}
type Machine struct {
	Config    Config           `json:"config"`
	H         string           `json:"height"`
	T         string           `json:"time_ms"`
	Polls     map[string]*Poll `json:"polls"`
	supported uint64
	events    []abci.Event
}

func (m *Machine) Events() []abci.Event { return m.events }

type Command struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}
type Process struct {
	PollID string `json:"poll_id"`
	Count  string `json:"count"`
}

func New(raw []byte, supported uint64) (*Machine, error) {
	var c Config
	if e := codec.Decode(raw, "partition_config", &c); e != nil {
		return nil, e
	}
	lane, _ := codec.Uint(c.Lane)
	epoch, _ := codec.Uint(c.Epoch)
	main, e := codec.ChainID(c.NetworkID)
	if e != nil || c.Source.ChainID != main || c.ChainID != fmt.Sprintf("%s-v%02x", main, lane) || lane > 255 || epoch == 0 || epoch > supported {
		return nil, fmt.Errorf("partition genesis context/epoch")
	}
	if _, e = c.Source.Set(); e != nil {
		return nil, e
	}
	return &Machine{Config: c, H: "0", T: "0", Polls: map[string]*Poll{}, supported: supported}, nil
}
func (m *Machine) Height() uint64 { n, _ := codec.Uint(m.H); return n }
func (m *Machine) Time() uint64   { n, _ := codec.Uint(m.T); return n }
func (m *Machine) State() []byte  { return codec.Must(m) }
func (m *Machine) Root() string   { return state.Root(m.Entries()) }
func (m *Machine) Active() bool {
	for id := range m.Polls {
		if !m.Summary(id).ProcessingSealed {
			return true
		}
	}
	return false
}
func (m *Machine) Entries() []state.Entry {
	es := []state.Entry{{Table: "replica_meta", Key: "partition", Value: map[string]any{"config": m.Config, "height": m.H, "time_ms": m.T}}}
	ids := []string{}
	for id := range m.Polls {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := m.Polls[id]
		es = append(es, state.Entry{Table: "lane_checkpoint", Key: id, Value: m.Summary(id)}, state.Entry{Table: "lane_context", Key: id, Value: p.Frozen})
		for i, d := range p.Deposits {
			es = append(es, state.Entry{Table: "lane_deposits", Key: []any{id, fmt.Sprint(i)}, Value: d})
		}
		for i, b := range p.Batches {
			es = append(es, state.Entry{Table: "lane_batches", Key: []any{id, fmt.Sprint(i)}, Value: b})
		}
	}
	return es
}
func admissionItems(p *Poll) [][]byte {
	xs := [][]byte{}
	for _, d := range p.Deposits {
		b, _ := codec.Blob(d.Envelope)
		xs = append(xs, b)
	}
	return xs
}
func batchItems(p *Poll) [][]byte {
	xs := [][]byte{}
	for _, b := range p.Batches {
		xs = append(xs, codec.Must(b.Reference))
	}
	return xs
}
func (m *Machine) Summary(id string) Summary {
	p := m.Polls[id]
	processed, _ := codec.Uint(p.Processed)
	closed, _ := codec.Uint(p.Frozen.Closes)
	statuses := []state.Entry{}
	for i := uint64(0); i < processed; i++ {
		d := p.Deposits[i]
		statuses = append(statuses, state.Entry{Table: "status", Key: fmt.Sprint(i), Value: map[string]any{"digest": d.Digest, "status": d.Status}})
	}
	spent := []state.Entry{}
	for n, d := range p.Spent {
		spent = append(spent, state.Entry{Table: "spent", Key: n, Value: d})
	}
	sealed := m.Time() >= closed
	return Summary{m.Config.NetworkID, id, m.Config.Lane, p.Frozen.Context.Epoch, p.Frozen.Context.Checkpoint, m.H, m.T, fmt.Sprint(len(p.Deposits)), LogRoot(admissionItems(p)), p.Processed, state.Root(statuses), p.Counted, state.Root(spent), p.Aggregate, fmt.Sprint(len(p.Batches)), LogRoot(batchItems(p)), sealed, sealed && processed == uint64(len(p.Deposits))}
}
func (m *Machine) clone() *Machine {
	var n Machine
	_ = json.Unmarshal(m.State(), &n)
	n.supported = m.supported
	return &n
}
func (m *Machine) Check(raw []byte) error {
	if len(raw) > 256*1024 {
		return fmt.Errorf("transaction bound")
	}
	var c Command
	if e := codec.Decode(raw, "partition_tx", &c); e != nil {
		return e
	}
	n := m.clone()
	_, _, e := n.command(c)
	return e
}
func (m *Machine) Apply(b replica.Block) (replica.Machine, error) {
	h, e := codec.Uint(b.Height)
	if e != nil || h != m.Height()+1 {
		return nil, fmt.Errorf("partition height")
	}
	tm, e := codec.Uint(b.TimeMS)
	if e != nil || tm < m.Time() {
		return nil, fmt.Errorf("partition time")
	}
	n := m.clone()
	n.H = b.Height
	n.T = b.TimeMS
	admitted, processed, size := 0, 0, 0
	for _, encoded := range b.Transactions {
		raw, e := codec.Blob(encoded)
		if e != nil || len(raw) > 256*1024 {
			return nil, fmt.Errorf("transaction bound")
		}
		size += len(raw)
		if size > replica.MaxBlockBytes {
			return nil, fmt.Errorf("block bound")
		}
		var c Command
		if e = codec.Decode(raw, "partition_tx", &c); e != nil {
			return nil, e
		}
		a, p, e := n.command(c)
		if e != nil {
			return nil, e
		}
		admitted += a
		processed += p
		if admitted > AdmissionBudget || processed > VerificationBudget {
			return nil, fmt.Errorf("separate admission/verification budget")
		}
	}
	ids := []string{}
	for id := range n.Polls {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		summary := n.Summary(id)
		previouslySealed := false
		if _, ok := m.Polls[id]; ok {
			previouslySealed = m.Summary(id).ProcessingSealed
		}
		if !summary.ProcessingSealed || !previouslySealed {
			n.events = append(n.events, abci.Event{Type: "lane.checkpoint.ready", Attributes: []abci.EventAttribute{{Key: "poll_id", Value: id, Index: true}, {Key: "lane_id", Value: n.Config.Lane, Index: true}, {Key: "height", Value: n.H, Index: true}, {Key: "processing_sealed", Value: fmt.Sprint(summary.ProcessingSealed), Index: true}}})
		}
	}
	return n, nil
}
func (m *Machine) command(c Command) (int, int, error) {
	switch c.Type {
	case "partition.open":
		var proof consensus.StateProof
		if e := json.Unmarshal(c.Value, &proof); e != nil {
			return 0, 0, e
		}
		return 0, 0, m.open(proof)
	case "partition.admit":
		var b Batch
		if e := json.Unmarshal(c.Value, &b); e != nil {
			return 0, 0, e
		}
		n, e := m.admit(b)
		return n, 0, e
	case "partition.process":
		var p Process
		if e := json.Unmarshal(c.Value, &p); e != nil {
			return 0, 0, e
		}
		n, e := m.process(p)
		return 0, n, e
	}
	return 0, 0, fmt.Errorf("unknown partition command")
}
func (m *Machine) open(proof consensus.StateProof) error {
	if _, e := consensus.VerifyState(m.Config.Source, proof); e != nil {
		return e
	}
	if proof.Path.Entry.Table != "transport_polls" {
		return fmt.Errorf("opening state table")
	}
	var f Frozen
	if e := json.Unmarshal(codec.Must(proof.Path.Entry.Value), &f); e != nil {
		return e
	}
	if e := f.Validate(m.Config.Source.ChainID); e != nil {
		return e
	}
	p, _ := codec.Uint(f.Context.Partitions)
	lane, _ := codec.Uint(m.Config.Lane)
	epoch, _ := codec.Uint(f.Context.Epoch)
	if f.Context.NetworkID != m.Config.NetworkID || f.Context.Epoch != m.Config.Epoch || epoch > m.supported || lane >= p || string(codec.Must(proof.Path.Entry.Key)) != string(codec.Must(f.Context.PollID)) || f.SourceHeight != proof.Height || f.Lanes[lane].ChainID != m.Config.ChainID {
		return fmt.Errorf("opening context")
	}
	if _, ok := m.Polls[f.Context.PollID]; ok {
		return fmt.Errorf("context already frozen")
	}
	v, _, e := f.Election()
	if e != nil {
		return e
	}
	m.Polls[f.Context.PollID] = &Poll{f, []Deposit{}, []StoredBatch{}, "0", "0", v.Zero(), map[string]string{}, map[string]string{}, map[string]string{}}
	return nil
}

// Admission verifies eligibility and byte binding, deliberately deferring the
// expensive ElectionGuard and nonce-knowledge proofs to the processing queue.
func (m *Machine) admit(b Batch) (int, error) {
	p := m.Polls[b.PollID]
	if p == nil {
		return 0, fmt.Errorf("unknown poll")
	}
	opens, _ := codec.Uint(p.Frozen.Opens)
	closes, _ := codec.Uint(p.Frozen.Closes)
	if len(b.Envelopes) == 0 || len(b.Envelopes) > AdmissionBudget {
		return 0, fmt.Errorf("batch bound")
	}
	ref, e := Reference(p.Frozen.Context, m.Config.Lane, fmt.Sprint(len(p.Deposits)), b.Envelopes)
	if e != nil {
		return 0, e
	}
	// Exact retry of a previously committed batch keeps the original certificate
	// and positions. It cannot replace bytes or consume an additional attempt.
	for _, old := range p.Batches {
		oldStart, _ := codec.Uint(old.Reference.Start)
		r, e := Reference(p.Frozen.Context, m.Config.Lane, fmt.Sprint(oldStart), b.Envelopes)
		if e == nil && r.ID == old.Reference.ID {
			return 0, nil
		}
	}
	// A retry of existing bytes remains readable after closure. Mixed batches
	// must separate new envelopes so their signed start/count stays unambiguous.
	allExisting := true
	for _, encoded := range b.Envelopes {
		exists := false
		for _, d := range p.Deposits {
			if d.Envelope == encoded {
				exists = true
				break
			}
		}
		allExisting = allExisting && exists
	}
	if allExisting {
		return 0, nil
	}
	if m.Time() < opens || m.Time() >= closes {
		return 0, fmt.Errorf("admission closed")
	}
	if e = VerifyCertificate(ref, b.Certificate, p.Frozen.ArchiveKeys); e != nil {
		return 0, e
	}
	s, e := semaphore.New()
	if e != nil {
		return 0, e
	}
	newDeposits := []Deposit{}
	for _, encoded := range b.Envelopes {
		raw, e := codec.Blob(encoded)
		if e != nil || len(raw) > electionguard.MaxScaleEnvelopeBytes {
			return 0, fmt.Errorf("envelope bound")
		}
		var env electionguard.ScaleEnvelope
		if e = codec.Decode(raw, "scale_envelope", &env); e != nil {
			return 0, e
		}
		c := p.Frozen.Context
		if env.NetworkID != c.NetworkID || env.PollID != c.PollID || env.Epoch != c.Epoch || env.Checkpoint != c.Checkpoint || env.Lane != m.Config.Lane {
			return 0, fmt.Errorf("envelope domain")
		}
		partitions, _ := codec.Uint(c.Partitions)
		lane, e := electionguard.Lane(c.NetworkID, c.PollID, env.Membership.Nullifier, partitions)
		if e != nil || fmt.Sprint(lane) != m.Config.Lane {
			return 0, fmt.Errorf("ballot route")
		}
		d, e := electionguard.Decode(env.Ballot, electionguard.Ballot)
		if e != nil {
			return 0, e
		}
		scope, message := electionguard.ScaleBinding(c, digest(d.Files["ballot.json"]), digest(codec.Must(env.Independence)))
		if e = s.VerifyBound(semaphore.Context{NetworkID: c.NetworkID, PollID: c.PollID, Epoch: c.Epoch, SuiteID: "semaphore-v4.14.5-depth-" + c.Depth, Depth: c.Depth, Root: c.Root}, env.Membership, scope, message); e != nil {
			return 0, e
		}
		id := digest(raw)
		attempts := 0
		for _, d := range append(append([]Deposit{}, p.Deposits...), newDeposits...) {
			if d.Digest == id {
				return 0, fmt.Errorf("identical envelope must reuse original batch receipt")
			}
			if d.Nullifier == env.Membership.Nullifier {
				attempts++
			}
		}
		if attempts >= 2 {
			return 0, fmt.Errorf("two distinct attempts per nullifier")
		}
		newDeposits = append(newDeposits, Deposit{encoded, id, "pending", env.Membership.Nullifier, m.H, ref.ID})
	}
	p.Deposits = append(p.Deposits, newDeposits...)
	p.Batches = append(p.Batches, StoredBatch{ref, b.Certificate})
	return len(newDeposits), nil
}
func (m *Machine) process(req Process) (int, error) {
	p := m.Polls[req.PollID]
	if p == nil {
		return 0, fmt.Errorf("unknown poll")
	}
	count, _ := codec.Uint(req.Count)
	start, _ := codec.Uint(p.Processed)
	if count == 0 || count > VerificationBudget || start+count > uint64(len(p.Deposits)) {
		return 0, fmt.Errorf("processing range")
	}
	v, el, e := p.Frozen.Election()
	if e != nil {
		return 0, e
	}
	for i := start; i < start+count; i++ {
		d := &p.Deposits[i]
		raw, _ := codec.Blob(d.Envelope)
		ballot, e := v.VerifyScale(el, p.Frozen.Context, raw)
		d.Status = "invalid"
		if e == nil {
			cipherID := codec.Hash("vote/ciphertext", ballot.Contests[0].Selections[0].Cipher)
			_, spent := p.Spent[d.Nullifier]
			_, duplicateBallot := p.Ballots[ballot.ID]
			_, duplicateCipher := p.Ciphertexts[cipherID]
			if spent || duplicateBallot || duplicateCipher {
				d.Status = "duplicate"
			} else {
				aggregate, e := v.Add(p.Aggregate, ballot)
				if e != nil {
					return 0, e
				}
				p.Aggregate = aggregate
				p.Spent[d.Nullifier] = d.Digest
				p.Ballots[ballot.ID] = d.Digest
				p.Ciphertexts[cipherID] = d.Digest
				n, _ := codec.Uint(p.Counted)
				p.Counted = fmt.Sprint(n + 1)
				d.Status = "counted"
			}
		}
		p.Processed = fmt.Sprint(i + 1)
	}
	return int(count), nil
}
