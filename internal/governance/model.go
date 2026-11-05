// Package governance implements deterministic civic transitions without I/O.
package governance

import (
	"civora/internal/codec"
	"civora/internal/ports"
	"civora/internal/state"
	"encoding/json"
	"fmt"
	"strconv"
)

const Hour uint64 = 3600000
const Day uint64 = 24 * Hour

type Policy struct {
	CollectionMS                   string `json:"collection_ms"`
	QueueMS                        string `json:"queue_ms"`
	SetupMS                        string `json:"setup_ms"`
	NoticeMS                       string `json:"notice_ms"`
	VotingMS                       string `json:"voting_ms"`
	TallyMS                        string `json:"tally_ms"`
	EndorsementsRequired           string `json:"endorsements_required"`
	OrdinaryQuorumBPS              string `json:"ordinary_quorum_bps"`
	ConstitutionalQuorumBPS        string `json:"constitutional_quorum_bps"`
	OrdinaryExecutionDelayMS       string `json:"ordinary_execution_delay_ms"`
	ConstitutionalExecutionDelayMS string `json:"constitutional_execution_delay_ms"`
}

func DefaultPolicy() Policy {
	return Policy{"604800000", "604800000", "172800000", "86400000", "259200000", "172800000", "2", "0", "2000", "86400000", "604800000"}
}
func (p Policy) Validate() error {
	for _, s := range []string{p.CollectionMS, p.QueueMS, p.SetupMS, p.NoticeMS, p.VotingMS, p.TallyMS, p.OrdinaryExecutionDelayMS, p.ConstitutionalExecutionDelayMS} {
		n, e := codec.Uint(s)
		if e != nil || n < Hour || n > 30*Day {
			return fmt.Errorf("ERR_POLICY_DURATION")
		}
	}
	if num(p.VotingMS) > 14*Day || num(p.ConstitutionalExecutionDelayMS) < Day {
		return fmt.Errorf("ERR_POLICY_DURATION")
	}
	n, e := codec.Uint(p.EndorsementsRequired)
	if e != nil || n < 1 || n > 20 {
		return fmt.Errorf("ERR_POLICY_ENDORSEMENTS")
	}
	n, e = codec.Uint(p.OrdinaryQuorumBPS)
	if e != nil || n > 5000 {
		return fmt.Errorf("ERR_POLICY_QUORUM")
	}
	n, e = codec.Uint(p.ConstitutionalQuorumBPS)
	if e != nil || n < 2000 || n > 5000 {
		return fmt.Errorf("ERR_POLICY_QUORUM")
	}
	return nil
}

type Guardian struct {
	AccountID string `json:"account_id"`
	SigningPK string `json:"signing_pk"`
}
type Config struct {
	Policy           Policy     `json:"policy"`
	Guardians        []Guardian `json:"guardians"`
	CryptoProfile    string     `json:"crypto_profile"`
	EligibilitySuite string     `json:"eligibility_suite"`
	BallotSuite      string     `json:"ballot_suite"`
	TallySuite       string     `json:"tally_suite"`
	EffectProfile    string     `json:"effect_profile"`
}
type NetworkState struct {
	Policy         Policy     `json:"policy"`
	PolicyRevision string     `json:"policy_revision"`
	GuardianEpoch  string     `json:"guardian_epoch"`
	Guardians      []Guardian `json:"guardians"`
}
type Submission struct {
	Action            string          `json:"action"`
	TargetID          *string         `json:"target_id"`
	ExpectedRevision  *string         `json:"expected_revision"`
	Rationale         string          `json:"rationale"`
	RequestedEffectMS *string         `json:"requested_effect_ms"`
	Changes           json.RawMessage `json:"changes"`
}
type Proposal struct {
	Submission
	Author          string               `json:"author"`
	State           string               `json:"state"`
	Policy          Policy               `json:"policy"`
	PolicyRevision  string               `json:"policy_revision"`
	Guardians       []Guardian           `json:"guardians"`
	GuardianEpoch   string               `json:"guardian_epoch"`
	Endorsements    []string             `json:"endorsements"`
	SubmittedHeight string               `json:"submitted_height"`
	SubmittedMS     string               `json:"submitted_ms"`
	CollectionDueMS string               `json:"collection_due_ms"`
	QualifiedHeight *string              `json:"qualified_height"`
	QualifiedMS     *string              `json:"qualified_ms"`
	QueueDueMS      *string              `json:"queue_due_ms"`
	SetupDueMS      *string              `json:"setup_due_ms"`
	PollID          *string              `json:"poll_id"`
	EffectiveAtMS   *string              `json:"effective_at_ms"`
	Reason          *string              `json:"reason"`
	EffectReceipt   *ports.EffectReceipt `json:"effect_receipt"`
}
type Result struct {
	Yes             string `json:"yes"`
	No              string `json:"no"`
	Empty           bool   `json:"empty"`
	CertifiedHeight string `json:"certified_height"`
	CertifiedMS     string `json:"certified_ms"`
}
type Poll struct {
	ProposalID       string   `json:"proposal_id"`
	State            string   `json:"state"`
	ProtocolEpoch    string   `json:"protocol_epoch"`
	EligibilitySuite string   `json:"eligibility_suite"`
	BallotSuite      string   `json:"ballot_suite"`
	TallySuite       string   `json:"tally_suite"`
	OpensAtMS        *string  `json:"opens_at_ms"`
	ClosesAtMS       *string  `json:"closes_at_ms"`
	TallyDueAtMS     *string  `json:"tally_due_at_ms"`
	GroupRoot        *string  `json:"group_root"`
	EligibleCount    string   `json:"eligible_count"`
	SnapshotHeight   *string  `json:"snapshot_height"`
	Commitments      []string `json:"commitments"`
	Aggregate        string   `json:"aggregate"`
	Count            string   `json:"count"`
	ClosedHeight     *string  `json:"closed_height"`
	AggregateDigest  *string  `json:"aggregate_digest"`
	CeremonyBundle   *string  `json:"ceremony_bundle"`
	CeremonyDigest   *string  `json:"ceremony_digest"`
	Result           *Result  `json:"result"`
}
type Object struct {
	Kind     string `json:"kind"`
	Revision string `json:"revision"`
	Status   string `json:"status"`
}
type Revision struct {
	Content        json.RawMessage `json:"content"`
	ProposalID     string          `json:"proposal_id"`
	SourceRevision *string         `json:"source_revision"`
	Status         string          `json:"status"`
	EffectiveAtMS  string          `json:"effective_at_ms"`
}
type Grant struct {
	AccountID string  `json:"account_id"`
	RoleID    string  `json:"role_id"`
	FromMS    string  `json:"from_ms"`
	UntilMS   string  `json:"until_ms"`
	Status    string  `json:"status"`
	RevokedBy *string `json:"revoked_by"`
	EndedAtMS *string `json:"ended_at_ms"`
}
type RoleContent struct {
	Label        string   `json:"label"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities"`
	ScopeID      *string  `json:"scope_id"`
}
type LawContent struct {
	Title    string `json:"title"`
	Text     string `json:"text"`
	Language string `json:"language"`
}
type Publication struct {
	RoleID     string `json:"role_id"`
	Capability string `json:"capability"`
	TargetID   string `json:"target_id"`
	Content    string `json:"content"`
}
type Spent struct {
	TxID     string `json:"tx_id"`
	Height   string `json:"height"`
	Position string `json:"position"`
}
type Ballot struct {
	Digest           string `json:"digest"`
	ConfirmationCode string `json:"confirmation_code"`
	Height           string `json:"height"`
	TxID             string `json:"tx_id"`
}
type State struct {
	Network     NetworkState
	Proposals   map[string]*Proposal
	Polls       map[string]*Poll
	Objects     map[string]Object
	Revisions   map[string]Revision
	Grants      map[string]*Grant
	Spent       map[string]Spent
	Ballots     map[string]Ballot
	Ciphertexts map[string]string
}

func New(c Config) (*State, error) {
	if e := c.Policy.Validate(); e != nil {
		return nil, e
	}
	if len(c.Guardians) != 5 {
		return nil, fmt.Errorf("ERR_GUARDIANS")
	}
	ids, keys := map[string]bool{}, map[string]bool{}
	for _, g := range c.Guardians {
		if ids[g.AccountID] || keys[g.SigningPK] {
			return nil, fmt.Errorf("ERR_GUARDIANS")
		}
		ids[g.AccountID] = true
		keys[g.SigningPK] = true
	}
	return &State{Network: NetworkState{c.Policy, "1", "1", c.Guardians}, Proposals: map[string]*Proposal{}, Polls: map[string]*Poll{}, Objects: map[string]Object{}, Revisions: map[string]Revision{}, Grants: map[string]*Grant{}, Spent: map[string]Spent{}, Ballots: map[string]Ballot{}, Ciphertexts: map[string]string{}}, nil
}
func (s *State) Clone() *State {
	b, _ := json.Marshal(s)
	var n State
	if e := json.Unmarshal(b, &n); e != nil {
		panic(e)
	}
	return &n
}
func ptr(s string) *string { return &s }
func str(n uint64) string  { return strconv.FormatUint(n, 10) }
func num(s string) uint64 {
	n, e := codec.Uint(s)
	if e != nil {
		panic(e)
	}
	return n
}
func val(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func add(a, b uint64) (uint64, error) {
	if b > ^uint64(0)-a {
		return 0, fmt.Errorf("ERR_TIME_OVERFLOW")
	}
	return a + b, nil
}
func tuple(parts ...string) string {
	v := make([]any, len(parts))
	for i, p := range parts {
		v[i] = p
	}
	return string(codec.Must(v))
}
func (s *State) Entries() []state.Entry {
	out := []state.Entry{}
	for id, p := range s.Proposals {
		out = append(out, state.Entry{Table: "proposals", Key: id, Value: p})
	}
	for id, p := range s.Polls {
		out = append(out, state.Entry{Table: "polls", Key: id, Value: p})
	}
	appendTuples := func(table string, values map[string]any) {
		for k, v := range values {
			var key []any
			_ = json.Unmarshal([]byte(k), &key)
			out = append(out, state.Entry{Table: table, Key: key, Value: v})
		}
	}
	a := map[string]any{}
	for k, v := range s.Objects {
		a[k] = v
	}
	appendTuples("objects", a)
	a = map[string]any{}
	for k, v := range s.Revisions {
		a[k] = v
	}
	appendTuples("revisions", a)
	a = map[string]any{}
	for k, v := range s.Grants {
		a[k] = v
	}
	appendTuples("grants", a)
	a = map[string]any{}
	for k, v := range s.Spent {
		a[k] = v
	}
	appendTuples("spent", a)
	a = map[string]any{}
	for k, v := range s.Ballots {
		a[k] = v
	}
	appendTuples("ballots", a)
	a = map[string]any{}
	for k, v := range s.Ciphertexts {
		a[k] = v
	}
	appendTuples("ciphertexts", a)
	return out
}

type AccountView struct {
	Active     bool
	SigningPK  string
	Commitment string
}
type Execution struct {
	Partitioned              bool
	NetworkID, Epoch, Height string
	Time                     uint64
	Config                   Config
	Accounts                 map[string]AccountView
	PreviousAccounts         map[string]AccountView
	PreviousHeight           string
	Eligibility              ports.EligibilityProvider
	Crypto                   ports.VotingCrypto
	Effects                  ports.EffectAdapter
	Membership               ports.MembershipVerifier
	Authorize                func(epoch, operation, module string, historical bool) error
	Emit                     func(kind, subject string)
}

func (s *State) LivePolls() int {
	n := 0
	for _, p := range s.Proposals {
		switch p.State {
		case "preparing", "scheduled", "open", "tallying", "processing", "provisional":
			n++
		}
	}
	return n
}
