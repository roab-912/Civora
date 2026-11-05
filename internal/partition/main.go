package partition

import (
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/core"
	"civora/internal/electionguard"
	"civora/internal/governance"
	"civora/internal/replica"
	"civora/internal/state"
	"encoding/json"
	"fmt"
	abci "github.com/cometbft/cometbft/abci/types"
	"sort"
)

// MainConfig explicitly selects the v0.5 transport development pilot. It does
// not enable scale_1 certification or law effects from a partition tally.
type MainConfig struct {
	Ledger      json.RawMessage   `json:"ledger"`
	Lanes       []consensus.Trust `json:"lanes"`
	ArchiveKeys []string          `json:"archive_keys"`
	Depth       string            `json:"circuit_depth"`
	Profile     string            `json:"profile"`
}
type Provisional struct {
	Aggregate string `json:"aggregate"`
	Count     string `json:"count"`
	Yes       string `json:"yes"`
	No        string `json:"no"`
	State     string `json:"state"`
	Tally     string `json:"decryption_record"`
}
type Main struct {
	Engine      *core.Engine
	Config      MainConfig
	Polls       map[string]Frozen
	Checkpoints map[string]map[string]*Verified
	Results     map[string]Provisional
	events      []abci.Event
}

func (m *Main) Events() []abci.Event { return m.events }
func (m *Main) emit(kind, id string) {
	m.events = append(m.events, abci.Event{Type: kind, Attributes: []abci.EventAttribute{{Key: "poll_id", Value: id, Index: true}, {Key: "height", Value: fmt.Sprint(m.Height()), Index: true}}})
}

type ResultRequest struct {
	PollID string `json:"poll_id"`
	Tally  string `json:"decryption_record"`
}

func (m *Main) RotateGuardians(g []governance.Guardian) (replica.Machine, error) {
	if m.Active() {
		return nil, fmt.Errorf("guardian rotation during transport poll")
	}
	n := m.clone()
	engine, e := consensus.RotateEngine(n.Engine, g)
	if e != nil {
		return nil, e
	}
	n.Engine = engine
	n.events = append([]abci.Event{}, m.events...)
	return n, nil
}

func NewMain(raw []byte, supported uint64) (*Main, error) {
	var c MainConfig
	if e := codec.Decode(raw, "coordinator_config", &c); e != nil {
		return nil, e
	}
	engine, e := core.New(c.Ledger, supported)
	if e != nil {
		return nil, e
	}
	if engine.Governance == nil || engine.Config.Governance.CryptoProfile != "electionguard_v0_4" {
		return nil, fmt.Errorf("transport pilot requires real ElectionGuard governance")
	}
	if engine.Config.VotingProfile != "transport_dev_v0_5" || engine.Config.Eligibility.CircuitDepth != c.Depth {
		return nil, fmt.Errorf("transport profile/circuit mismatch")
	}
	p := len(c.Lanes)
	if p == 0 || p > 256 || p&(p-1) != 0 {
		return nil, fmt.Errorf("manifest partitions")
	}
	seen := map[string]bool{}
	for i, l := range c.Lanes {
		if l.ChainID != fmt.Sprintf("%s-v%02x", engine.Config.ChainID, i) {
			return nil, fmt.Errorf("manifest chain ID")
		}
		if _, e = l.Set(); e != nil {
			return nil, e
		}
		for _, k := range l.Keys {
			seen[k] = true
		}
	}
	for _, k := range c.ArchiveKeys {
		if seen[k] {
			return nil, fmt.Errorf("archive/validator keys overlap")
		}
	}
	return &Main{Engine: engine, Config: c, Polls: map[string]Frozen{}, Checkpoints: map[string]map[string]*Verified{}, Results: map[string]Provisional{}}, nil
}
func (m *Main) Height() uint64 { return m.Engine.Height }
func (m *Main) Time() uint64   { return m.Engine.Time }
func (m *Main) Entries() []state.Entry {
	entries := m.Engine.Snapshot().Entries
	entries = append(entries, state.Entry{Table: "transport_config", Key: "development", Value: m.Config})
	for id, f := range m.Polls {
		entries = append(entries, state.Entry{Table: "transport_polls", Key: id, Value: f})
	}
	for poll, lanes := range m.Checkpoints {
		for lane, cp := range lanes {
			entries = append(entries, state.Entry{Table: "transport_checkpoints", Key: []any{poll, lane}, Value: cp})
		}
	}
	for id, r := range m.Results {
		entries = append(entries, state.Entry{Table: "transport_results", Key: id, Value: r})
	}
	sort.Slice(entries, func(i, j int) bool {
		return string(codec.Must([]any{entries[i].Table, entries[i].Key})) < string(codec.Must([]any{entries[j].Table, entries[j].Key}))
	})
	return entries
}
func (m *Main) State() []byte {
	return codec.Must(core.State{Height: fmt.Sprint(m.Height()), TimeMS: fmt.Sprint(m.Time()), Entries: m.Entries()})
}
func (m *Main) Root() string { return state.Root(m.Entries()) }
func (m *Main) Active() bool {
	if m.Engine.Governance.LivePolls() > 0 {
		return true
	}
	for id := range m.Polls {
		if m.Engine.Governance.Polls[id].State == "failed_certification" {
			continue
		}
		if _, ok := m.Results[id]; !ok {
			return true
		}
	}
	return false
}
func (m *Main) clone() *Main {
	n := &Main{Engine: m.Engine.Clone(), Config: m.Config}
	_ = json.Unmarshal(codec.Must(m.Polls), &n.Polls)
	_ = json.Unmarshal(codec.Must(m.Checkpoints), &n.Checkpoints)
	_ = json.Unmarshal(codec.Must(m.Results), &n.Results)
	return n
}
func (m *Main) Check(raw []byte) error {
	var shape struct {
		Type string `json:"type"`
	}
	if e := json.Unmarshal(raw, &shape); e != nil {
		return e
	}
	if shape.Type == "lane.checkpoint" || shape.Type == "partition.tally" {
		var c Command
		if e := codec.Decode(raw, "coordinator_tx", &c); e != nil {
			return e
		}
		return m.clone().command(c)
	}
	var tx core.Transaction
	if e := codec.Decode(raw, "transaction", &tx); e != nil {
		return e
	}
	if tx.Body.Type == "vote.cast" || tx.Body.Type == "poll.tally" {
		return fmt.Errorf("partition pilot rejects inline votes/tallies")
	}
	return m.Engine.CheckTx(raw)
}
func (m *Main) Apply(b replica.Block) (replica.Machine, error) {
	height, e := codec.Uint(b.Height)
	if e != nil {
		return nil, e
	}
	for _, a := range m.Engine.Config.Activations {
		if a.At(height) && m.Active() {
			return nil, fmt.Errorf("ERR_UPGRADE_BARRIER")
		}
	}
	n := m.clone()
	ledger := core.Block{Height: b.Height, TimeMS: b.TimeMS, Transactions: []core.Transaction{}}
	commands := []Command{}
	size := 0
	for _, encoded := range b.Transactions {
		raw, e := codec.Blob(encoded)
		if e != nil || len(raw) > 256*1024 {
			return nil, fmt.Errorf("transaction bound")
		}
		size += len(raw)
		if size > replica.MaxBlockBytes {
			return nil, fmt.Errorf("block bound")
		}
		var shape struct {
			Type string `json:"type"`
		}
		if e = json.Unmarshal(raw, &shape); e != nil {
			return nil, e
		}
		if shape.Type == "lane.checkpoint" || shape.Type == "partition.tally" {
			var c Command
			if e = codec.Decode(raw, "coordinator_tx", &c); e != nil {
				return nil, e
			}
			commands = append(commands, c)
		} else {
			var tx core.Transaction
			if e = codec.Decode(raw, "transaction", &tx); e != nil {
				return nil, e
			}
			if tx.Body.Type == "vote.cast" || tx.Body.Type == "poll.tally" {
				return nil, fmt.Errorf("partition pilot rejects inline votes/tallies")
			}
			ledger.Transactions = append(ledger.Transactions, tx)
		}
	}
	// Reserved deterministic checkpoint budget, independent from account traffic.
	if len(commands) > 32 {
		return nil, fmt.Errorf("checkpoint budget")
	}
	next, events, e := m.Engine.Apply(codec.Must(ledger))
	if e != nil {
		return nil, e
	}
	n.Engine = next
	n.events = consensus.CivicEvents(events)
	for id, r := range n.Results {
		if n.Engine.Governance.Polls[id].State == "failed_certification" {
			r.State = "failed_certification"
			n.Results[id] = r
		}
	}
	ids := []string{}
	for id := range n.Engine.Governance.Polls {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		poll := n.Engine.Governance.Polls[id]
		if poll.State != "open" || poll.GroupRoot == nil {
			continue
		}
		if _, ok := n.Polls[id]; ok {
			continue
		}
		guardians := []string{}
		for _, g := range n.Engine.Governance.Proposals[poll.ProposalID].Guardians {
			guardians = append(guardians, g.AccountID)
		}
		sort.Strings(guardians)
		f := Frozen{Context: electionguard.ScaleContext{NetworkID: n.Engine.Config.NetworkID, PollID: id, Epoch: poll.ProtocolEpoch, Root: *poll.GroupRoot, Depth: n.Config.Depth, Partitions: fmt.Sprint(len(n.Config.Lanes))}, ProposalID: poll.ProposalID, GuardianIDs: guardians, Ceremony: *poll.CeremonyBundle, Opens: *poll.OpensAtMS, Closes: *poll.ClosesAtMS, SourceHeight: b.Height, Lanes: n.Config.Lanes, ArchiveKeys: n.Config.ArchiveKeys, EligibleCount: poll.EligibleCount}
		f.Context.Checkpoint = f.ID()
		if e = f.Validate(n.Engine.Config.ChainID); e != nil {
			return nil, e
		}
		n.Polls[id] = f
		n.emit("poll.partition.opened", id)
		n.Checkpoints[id] = map[string]*Verified{}
	}
	for _, c := range commands {
		if e = n.command(c); e != nil {
			return nil, e
		}
	}
	return n, nil
}
func (m *Main) command(c Command) error {
	switch c.Type {
	case "lane.checkpoint":
		var cp Checkpoint
		if e := json.Unmarshal(c.Value, &cp); e != nil {
			return e
		}
		var s Summary
		if e := json.Unmarshal(codec.Must(cp.Proof.Path.Entry.Value), &s); e != nil {
			return e
		}
		f, ok := m.Polls[s.PollID]
		if !ok {
			return fmt.Errorf("unknown poll")
		}
		if _, ok = m.Results[s.PollID]; ok {
			return fmt.Errorf("provisional dossier already frozen")
		}
		phase := m.Engine.Governance.Polls[s.PollID].State
		if phase != "open" && phase != "processing" {
			return fmt.Errorf("checkpoint poll closed")
		}
		v, e := VerifyCheckpoint(f, cp, m.Checkpoints[s.PollID][s.Lane])
		if e != nil {
			return e
		}
		m.Checkpoints[s.PollID][s.Lane] = v
		m.emit("lane.checkpoint", s.PollID)
		return nil
	case "partition.tally":
		var req ResultRequest
		if e := json.Unmarshal(c.Value, &req); e != nil {
			return e
		}
		f, ok := m.Polls[req.PollID]
		if !ok {
			return fmt.Errorf("unknown poll")
		}
		if _, exists := m.Results[req.PollID]; exists {
			return fmt.Errorf("result already frozen")
		}
		aggregate, count, e := Global(f, m.Checkpoints[req.PollID])
		if e != nil {
			return e
		}
		if count == "0" {
			if req.Tally != "" {
				return fmt.Errorf("empty poll requires no decryption record")
			}
			if e = m.Engine.Governance.StagePartitionResult(m.Engine.Config.NetworkID, req.PollID, aggregate, count, m.Time()); e != nil {
				return e
			}
			m.Results[req.PollID] = Provisional{aggregate, "0", "0", "0", "provisional", ""}
			m.emit("poll.provisional", req.PollID)
			return nil
		}
		v, el, e := f.Election()
		if e != nil {
			return e
		}
		d, e := electionguard.Decode(req.Tally, electionguard.Tally)
		if e != nil {
			return e
		}
		yes, no, e := v.Tally(el, req.PollID, aggregate, count, d.Files["tally.json"])
		if e != nil {
			return e
		}
		if e = m.Engine.Governance.StagePartitionResult(m.Engine.Config.NetworkID, req.PollID, aggregate, count, m.Time()); e != nil {
			return e
		}
		m.Results[req.PollID] = Provisional{aggregate, count, yes, no, "provisional", req.Tally}
		m.emit("poll.provisional", req.PollID)
		return nil
	}
	return fmt.Errorf("coordinator command")
}
