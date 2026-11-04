//go:build civora_sim

package simulation

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/governance"
	"civora/internal/ports"
	"civora/internal/upgrade"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

type TestActor struct {
	Seed         string `json:"seed_TEST_ONLY"`
	RecoverySeed string `json:"recovery_seed_TEST_ONLY"`
	PublicKey    string `json:"public_key"`
	RecoveryKey  string `json:"recovery_key"`
	ID           string `json:"account_id"`
	Commitment   string `json:"commitment"`
}
type Fixture struct {
	Name           string         `json:"name"`
	Genesis        core.Config    `json:"genesis"`
	Blocks         []core.Block   `json:"blocks"`
	ExpectedRoots  []string       `json:"expected_roots"`
	ExpectedEvents [][]core.Event `json:"expected_events"`
	ExpectedState  core.State     `json:"expected_state"`
}
type Builder struct {
	Engine  *core.Engine
	Actors  []TestActor
	Fixture Fixture
	Time    uint64
}

func seed(label string) (string, string) {
	h := sha256.Sum256([]byte("CIVORA PUBLIC TEST KEY ONLY v0.2 " + label))
	key := ed25519.NewKeyFromSeed(h[:])
	return hex.EncodeToString(h[:]), hex.EncodeToString(key.Public().(ed25519.PublicKey))
}
func NewBuilder(name string) (*Builder, error) {
	actors := []TestActor{}
	for i := 0; i < 8; i++ {
		s, pk := seed(fmt.Sprint(i, " signing"))
		r, rpk := seed(fmt.Sprint(i, " recovery"))
		actors = append(actors, TestActor{s, r, pk, rpk, codec.Hash("account", []any{NetworkID(), pk}), strconv.Itoa(i + 1)})
	}
	module := func(id, op, format string) upgrade.Module {
		m := upgrade.Module{ID: id, Status: "active", Operations: []string{op}, Artifacts: []string{}, Dependencies: []string{}, RetirementPolicy: "never_reactivate_revoked", ImplementationSHA256: codec.Hash("simulation/artifact", id)}
		m.Parameters.Format = format
		return m
	}
	chain, _ := codec.ChainID(NetworkID())
	policy := governance.DefaultPolicy()
	policy.CollectionMS = strconv.FormatUint(governance.Day, 10)
	policy.QueueMS = policy.CollectionMS
	policy.SetupMS = fmt.Sprint(governance.Hour)
	policy.NoticeMS = policy.SetupMS
	policy.VotingMS = policy.SetupMS
	policy.TallyMS = policy.SetupMS
	policy.OrdinaryExecutionDelayMS = policy.SetupMS
	policy.ConstitutionalExecutionDelayMS = fmt.Sprint(governance.Day)
	guardians := []governance.Guardian{}
	for _, a := range actors[:5] {
		guardians = append(guardians, governance.Guardian{AccountID: a.ID, SigningPK: a.PublicKey})
	}
	c := core.Config{NetworkID: NetworkID(), ChainID: chain, VotingProfile: "inline_dev", Mode: "development", Activations: []upgrade.Armed{}, Registry: upgrade.Registry{Epoch: "1", StateSchema: "1", TransactionVersion: "1", Modules: []upgrade.Module{module("dev.sim.hash", "state.hash", "sha256"), module("dev.sim.account", "account.sign", "ed25519"), module("dev.sim.eligibility", "eligibility.prove", Profile), module("dev.sim.ballot", "ballot.prove", Profile), module("dev.sim.tally", "tally.prove", Profile)}}, Governance: &governance.Config{Policy: policy, Guardians: guardians, CryptoProfile: Profile, EligibilitySuite: "dev.sim.eligibility", BallotSuite: "dev.sim.ballot", TallySuite: "dev.sim.tally", EffectProfile: "ledger_only_v1"}}
	engine, e := New(codec.Must(c), 1)
	if e != nil {
		return nil, e
	}
	return &Builder{Engine: engine, Actors: actors, Fixture: Fixture{Name: name, Genesis: c, Blocks: []core.Block{}, ExpectedRoots: []string{}, ExpectedEvents: [][]core.Event{}}, Time: 1000}, nil
}
func Sign(body core.Body, seed string) core.Transaction {
	s, _ := hex.DecodeString(seed)
	d, _ := hex.DecodeString(core.TxID(body))
	return core.Transaction{Body: body, Auth: core.Auth{Scheme: "ed25519", Signature: hex.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(s), d))}}
}
func (b *Builder) Signed(actor int, kind string, payload any) core.Transaction {
	a := b.Actors[actor]
	nonce := "0"
	if acc, ok := b.Engine.Accounts[a.ID]; ok {
		nonce = acc.NextNonce
	}
	return Sign(core.Body{V: "1", NetworkID: NetworkID(), Type: kind, ActorID: a.ID, Nonce: nonce, Payload: codec.Must(payload)}, a.Seed)
}
func (b *Builder) Proof(kind string, payload any) core.Transaction {
	return core.Transaction{Body: core.Body{V: "1", NetworkID: NetworkID(), Type: kind, Payload: codec.Must(payload)}, Auth: core.Auth{Scheme: "proof"}}
}
func (b *Builder) Block(at uint64, txs ...core.Transaction) error {
	if txs == nil {
		txs = []core.Transaction{}
	}
	block := core.Block{Height: fmt.Sprint(b.Engine.Height + 1), TimeMS: fmt.Sprint(at), Transactions: txs}
	next, events, e := b.Engine.Apply(codec.Must(block))
	if e != nil {
		return fmt.Errorf("%s block %s: %w", b.Fixture.Name, block.Height, e)
	}
	b.Engine = next
	b.Time = next.Time
	b.Fixture.Blocks = append(b.Fixture.Blocks, block)
	b.Fixture.ExpectedRoots = append(b.Fixture.ExpectedRoots, next.Root())
	b.Fixture.ExpectedEvents = append(b.Fixture.ExpectedEvents, events)
	return nil
}
func (b *Builder) Tick(txs ...core.Transaction) error { return b.Block(b.Time+1, txs...) }
func (b *Builder) Register() error {
	txs := []core.Transaction{}
	for i, a := range b.Actors {
		txs = append(txs, b.Signed(i, "account.register", core.Payload{SigningPK: a.PublicKey, RecoveryPK: a.RecoveryKey, Commitment: a.Commitment}))
	}
	return b.Tick(txs...)
}
func (b *Builder) Submit(req governance.Submission) (string, error) {
	tx := b.Signed(5, "proposal.submit", req)
	id := core.TxID(tx.Body)
	err := b.Tick(tx, b.Signed(6, "proposal.endorse", map[string]any{"proposal_id": id}), b.Signed(7, "proposal.endorse", map[string]any{"proposal_id": id}))
	return id, err
}
func (b *Builder) Context(pollID string) ports.PollContext {
	poll := b.Engine.Governance.Polls[pollID]
	digest := ""
	if poll.CeremonyDigest != nil {
		digest = *poll.CeremonyDigest
	}
	return ports.PollContext{NetworkID: NetworkID(), PollID: pollID, ProposalID: poll.ProposalID, Epoch: poll.ProtocolEpoch, EligibilitySuite: poll.EligibilitySuite, BallotSuite: poll.BallotSuite, TallySuite: poll.TallySuite, GroupRoot: poll.GroupRoot, Commitments: poll.Commitments, CeremonyDigest: digest}
}
func (b *Builder) Configuration(pollID string) core.Transaction {
	poll := b.Engine.Governance.Polls[pollID]
	bundle := codec.Must(Ceremony{Profile, NetworkID(), pollID, poll.ProposalID, codec.Hash("poll/question", []any{NetworkID(), pollID, poll.ProposalID})})
	digest := sha256.Sum256(bundle)
	ackHash, _ := hex.DecodeString(codec.Hash("poll/ceremony", []any{NetworkID(), pollID, hex.EncodeToString(digest[:])}))
	acks := []governance.GuardianAck{}
	for _, a := range b.Actors[:5] {
		s, _ := hex.DecodeString(a.Seed)
		acks = append(acks, governance.GuardianAck{AccountID: a.ID, Signature: hex.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(s), ackHash))})
	}
	sort.Slice(acks, func(i, j int) bool { return acks[i].AccountID < acks[j].AccountID })
	return b.Proof("poll.configure", governance.Configuration{PollID: pollID, CeremonyBundle: base64.RawURLEncoding.EncodeToString(bundle), GuardianAcks: acks})
}
func (b *Builder) Open(req governance.Submission) (string, error) {
	id, e := b.Submit(req)
	if e != nil {
		return "", e
	}
	if e = b.Tick(); e != nil {
		return "", e
	}
	pollID := *b.Engine.Governance.Proposals[id].PollID
	if e = b.Tick(b.Configuration(pollID)); e != nil {
		return "", e
	}
	opens, _ := codec.Uint(*b.Engine.Governance.Polls[pollID].OpensAtMS)
	if e = b.Block(opens); e != nil {
		return "", e
	}
	return id, nil
}
func (b *Builder) Votes(id string, yes, no int) error {
	pollID := *b.Engine.Governance.Proposals[id].PollID
	ctx := b.Context(pollID)
	txs := []core.Transaction{}
	for i := 0; i < yes+no; i++ {
		choice := "0"
		if i < yes {
			choice = "1"
		}
		vote := Vote(ctx, b.Actors[i].Commitment, choice, codec.Hash("simulation/ballot-id", []any{pollID, fmt.Sprint(i)}))
		txs = append(txs, b.Proof("vote.cast", vote))
	}
	return b.Tick(txs...)
}
func (b *Builder) Close(id string) error {
	poll := b.Engine.Governance.Polls[*b.Engine.Governance.Proposals[id].PollID]
	closes, _ := codec.Uint(*poll.ClosesAtMS)
	return b.Block(closes)
}
func (b *Builder) Count(id string) error {
	pollID := *b.Engine.Governance.Proposals[id].PollID
	poll := b.Engine.Governance.Polls[pollID]
	return b.Tick(b.Proof("poll.tally", Tally(b.Context(pollID), poll.Aggregate, poll.Count, *poll.AggregateDigest)))
}
func (b *Builder) Decide(req governance.Submission, yes, no int) (string, error) {
	id, e := b.Open(req)
	if e != nil {
		return "", e
	}
	if e = b.Votes(id, yes, no); e != nil {
		return "", e
	}
	if e = b.Close(id); e != nil {
		return "", e
	}
	p := b.Engine.Governance.Proposals[id]
	if p.State == "tallying" {
		if e = b.Count(id); e != nil {
			return "", e
		}
	}
	p = b.Engine.Governance.Proposals[id]
	if p.State == "adopted" {
		at, _ := codec.Uint(*p.EffectiveAtMS)
		if e = b.Block(at); e != nil {
			return "", e
		}
	}
	return id, nil
}
func Request(action string, target, revision *string, changes any) governance.Submission {
	return governance.Submission{Action: action, TargetID: target, ExpectedRevision: revision, Rationale: "PUBLIC TEST ONLY — deterministic governance fixture", Changes: codec.Must(changes)}
}
func pointer(s string) *string { return &s }
func Law(title string) governance.Submission {
	return Request("law.create", nil, nil, governance.LawContent{Title: title, Text: "Texte pilote de développement.", Language: "fr"})
}

func GovernanceScenario() (*Builder, error) {
	b, e := NewBuilder("governance")
	if e != nil {
		return nil, e
	}
	if e = b.Register(); e != nil {
		return nil, e
	}
	law, e := b.Decide(Law("Loi de démonstration"), 2, 1)
	if e != nil {
		return nil, e
	}
	lawID := codec.Hash("object", []any{"law", law})
	requests := []governance.Submission{Request("law.amend", &lawID, pointer("1"), governance.LawContent{Title: "Loi amendée", Text: "Texte révisé.", Language: "fr"}), Request("law.repeal", &lawID, pointer("2"), map[string]any{}), Request("law.restore", &lawID, pointer("3"), map[string]any{"source_revision": "1"})}
	for _, r := range requests {
		if _, e = b.Decide(r, 2, 1); e != nil {
			return nil, e
		}
	}
	role, e := b.Decide(Request("role.create", nil, nil, governance.RoleContent{Label: "Rapporteur", Description: "Rôle de développement", Capabilities: []string{"publish_notice", "report_implementation"}, ScopeID: &lawID}), 2, 1)
	if e != nil {
		return nil, e
	}
	roleID := codec.Hash("object", []any{"role", role})
	grant, e := b.Decide(Request("role.grant", &roleID, pointer("1"), map[string]any{"account_id": b.Actors[5].ID, "until_ms": fmt.Sprint(b.Time + 100*governance.Day)}), 2, 1)
	if e != nil {
		return nil, e
	}
	if e = b.Tick(b.Signed(5, "role.publish", governance.Publication{RoleID: roleID, Capability: "publish_notice", TargetID: lawID, Content: "Avis public de développement."})); e != nil {
		return nil, e
	}
	requests = []governance.Submission{Request("role.revoke", &roleID, pointer("1"), map[string]any{"grant_id": grant}), Request("role.retire", &roleID, pointer("1"), map[string]any{}), Request("role.restore", &roleID, pointer("2"), map[string]any{"source_revision": "1"})}
	for _, r := range requests {
		if _, e = b.Decide(r, 2, 1); e != nil {
			return nil, e
		}
	}
	if _, e = b.Decide(Request("role.grant", &roleID, pointer("3"), map[string]any{"account_id": b.Actors[5].ID, "until_ms": fmt.Sprint(b.Time + 100*governance.Day)}), 2, 1); e != nil {
		return nil, e
	}
	if _, e = b.Decide(Request("role.amend", &roleID, pointer("3"), governance.RoleContent{Label: "Rapporteur révisé", Description: "Publication de rapports", Capabilities: []string{"report_implementation"}, ScopeID: &lawID}), 2, 1); e != nil {
		return nil, e
	}
	if e = b.Tick(b.Signed(5, "role.publish", governance.Publication{RoleID: roleID, Capability: "report_implementation", TargetID: lawID, Content: "Rapport de développement."})); e != nil {
		return nil, e
	}
	policy := b.Engine.Governance.Network.Policy
	policy.OrdinaryQuorumBPS = "1000"
	if _, e = b.Decide(Request("policy.update", nil, pointer("1"), map[string]any{"policy": policy}), 2, 1); e != nil {
		return nil, e
	}
	return b, nil
}
func GenerateFixtures(dir string, check bool) error {
	b, e := GovernanceScenario()
	if e != nil {
		return e
	}
	b.Fixture.ExpectedState = b.Engine.Snapshot()
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	write := func(name string, v any) error {
		path := filepath.Join(dir, name+".json")
		out := codec.Must(v)
		if check {
			existing, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			if !bytes.Equal(existing, out) {
				return fmt.Errorf("stale fixture: %s", path)
			}
			return nil
		}
		return os.WriteFile(path, out, 0600)
	}
	if e = write("governance", b.Fixture); e != nil {
		return e
	}
	keys := struct {
		Warning string      `json:"warning"`
		Actors  []TestActor `json:"actors"`
	}{"PUBLIC TEST KEYS ONLY. Simulation ballots reveal choices and commitments. NEVER USE FOR A PILOT.", b.Actors}
	return write("keys.TEST_ONLY", keys)
}

var _ ports.EligibilityProvider = Adapter{}
var _ ports.VotingCrypto = Adapter{}
