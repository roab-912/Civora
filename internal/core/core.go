package core

import (
	"civora/internal/codec"
	"civora/internal/effects"
	"civora/internal/electionguard"
	"civora/internal/eligibility"
	"civora/internal/governance"
	"civora/internal/ports"
	"civora/internal/recovery"
	"civora/internal/semaphore"
	"civora/internal/state"
	"civora/internal/upgrade"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
)

const MaxTxBytes = 256 * 1024
const MaxBlockBytes = 2 * 1024 * 1024

type Config struct {
	NetworkID     string              `json:"network_id"`
	ChainID       string              `json:"chain_id"`
	VotingProfile string              `json:"voting_profile"`
	Mode          string              `json:"mode"`
	Registry      upgrade.Registry    `json:"registry"`
	Activations   []upgrade.Armed     `json:"activations"`
	Governance    *governance.Config  `json:"governance,omitempty"`
	Eligibility   *eligibility.Config `json:"eligibility,omitempty"`
	Recovery      *recovery.Config    `json:"recovery,omitempty"`
}
type Payload struct {
	SigningPK  string `json:"signing_pk"`
	RecoveryPK string `json:"recovery_pk"`
	Commitment string `json:"commitment"`
}
type Body struct {
	V         string          `json:"v"`
	NetworkID string          `json:"network_id"`
	Type      string          `json:"type"`
	ActorID   string          `json:"actor_id"`
	Nonce     string          `json:"nonce"`
	Payload   json.RawMessage `json:"payload"`
	Epoch     string          `json:"protocol_epoch,omitempty"`
}
type Auth struct {
	Scheme    string `json:"scheme"`
	Signature string `json:"signature,omitempty"`
}
type Transaction struct {
	Body Body `json:"body"`
	Auth Auth `json:"auth"`
}
type Account struct {
	SigningPK        string   `json:"signing_pk"`
	RecoveryPK       string   `json:"recovery_pk"`
	Commitment       string   `json:"commitment"`
	NextNonce        string   `json:"next_nonce"`
	Status           string   `json:"status"`
	RegisteredHeight string   `json:"registered_height"`
	UsedKeys         []string `json:"used_keys"`
	UsedCommitments  []string `json:"used_commitments"`
	EligibilityIndex *string  `json:"eligibility_index,omitempty"`
}
type Block struct {
	Height         string            `json:"height"`
	TimeMS         string            `json:"time_ms"`
	Transactions   []Transaction     `json:"transactions"`
	SecurityOrders []recovery.Bundle `json:"security_orders,omitempty"`
}
type Event struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Subject string `json:"subject"`
}
type State struct {
	Height  string        `json:"height"`
	TimeMS  string        `json:"time_ms"`
	Entries []state.Entry `json:"entries"`
}
type UpgradeState struct {
	Plan     upgrade.Plan     `json:"plan"`
	Instance upgrade.Instance `json:"instance"`
	Status   string           `json:"status"`
}
type Engine struct {
	Config         Config
	Accounts       map[string]Account
	Height         uint64
	Time           uint64
	SupportedEpoch uint64
	upgrades       map[string]UpgradeState
	Governance     *governance.State
	adapters       Adapters
	Eligibility    *eligibility.Tree
	Security       *recovery.LedgerState
}

type Adapters struct {
	Eligibility ports.EligibilityProvider
	Voting      ports.VotingCrypto
	Effects     ports.EffectAdapter
	Membership  ports.MembershipVerifier
	Profile     string
}

func New(raw []byte, supportedEpoch uint64) (*Engine, error) {
	return NewWithAdapters(raw, supportedEpoch, Adapters{})
}
func NewWithAdapters(raw []byte, supportedEpoch uint64, adapters Adapters) (*Engine, error) {
	var c Config
	if e := codec.Decode(raw, "config", &c); e != nil {
		return nil, e
	}
	chain, e := codec.ChainID(c.NetworkID)
	if e != nil || chain != c.ChainID {
		return nil, fmt.Errorf("invalid chain_id")
	}
	if e = c.Registry.Validate(); e != nil {
		return nil, e
	}
	if (c.VotingProfile == "transport_dev_v0_5" || c.VotingProfile == "transport_dev_v0_6") && (c.Governance == nil || c.Governance.CryptoProfile != "electionguard_v0_4" || c.Eligibility == nil || c.Eligibility.CircuitDepth != "26") {
		return nil, fmt.Errorf("ERR_PARTITION_PROFILE")
	}
	epoch, _ := codec.Uint(c.Registry.Epoch)
	if epoch > supportedEpoch {
		return nil, fmt.Errorf("ERR_UNSUPPORTED_EPOCH")
	}
	n := &Engine{Config: c, Accounts: map[string]Account{}, SupportedEpoch: supportedEpoch, upgrades: map[string]UpgradeState{}}
	if c.Recovery != nil {
		if c.Mode != "development" {
			return nil, fmt.Errorf("ERR_RECOVERY_DEVELOPMENT_ONLY")
		}
		if err := c.Recovery.ValidateGenesis(c.NetworkID, c.VotingProfile); err != nil {
			return nil, err
		}
		n.Security = recovery.NewLedger()
	}
	if adapters.Effects == nil {
		adapters.Effects = effects.LedgerOnly{}
	}
	n.adapters = adapters
	if c.Eligibility != nil {
		depth, err := codec.Uint(c.Eligibility.CircuitDepth)
		if err != nil || depth != 16 && depth != 26 {
			return nil, fmt.Errorf("ERR_CIRCUIT_DEPTH")
		}
		if c.Eligibility.SuiteID != "semaphore-v4.14.5-depth-"+c.Eligibility.CircuitDepth {
			return nil, fmt.Errorf("ERR_SUITE")
		}
		if err = c.Registry.Authorize(c.Registry.Epoch, "eligibility.prove", c.Eligibility.SuiteID, false); err != nil {
			return nil, err
		}
		n.Eligibility, err = eligibility.New(uint8(depth))
		if err != nil {
			return nil, err
		}
		if n.adapters.Eligibility == nil {
			n.adapters.Eligibility = eligibility.InlineProvider{}
		}
		if n.adapters.Membership == nil {
			n.adapters.Membership, err = semaphore.New()
			if err != nil {
				return nil, err
			}
		}
	}
	if c.Governance != nil {
		if c.Governance.CryptoProfile == "electionguard_v0_4" {
			implementation, artifacts, err := electionguard.PinnedModule()
			if err != nil {
				return nil, err
			}
			found := false
			for _, module := range c.Registry.Modules {
				if module.ID == electionguard.Suite {
					found = true
					if module.ImplementationSHA256 != implementation || !equalStrings(module.Artifacts, artifacts) {
						return nil, fmt.Errorf("ERR_EG_MODULE_PIN")
					}
				}
			}
			if !found {
				return nil, fmt.Errorf("ERR_EG_MODULE_PIN")
			}
			if c.Eligibility == nil || c.Governance.EligibilitySuite != c.Eligibility.SuiteID || c.Governance.BallotSuite != electionguard.Suite || c.Governance.TallySuite != electionguard.Suite {
				return nil, fmt.Errorf("ERR_CRYPTO_PROFILE")
			}
			if err := c.Registry.Authorize(c.Registry.Epoch, "ballot.prove", electionguard.Suite, false); err != nil {
				return nil, err
			}
			if err := c.Registry.Authorize(c.Registry.Epoch, "tally.prove", electionguard.Suite, false); err != nil {
				return nil, err
			}
			if adapters.Voting != nil {
				return nil, fmt.Errorf("ERR_PINNED_VOTING_ADAPTER")
			}
			n.adapters.Voting, err = electionguard.NewAdapter()
			if err != nil {
				return nil, err
			}
		}
		if c.Governance.CryptoProfile == "simulation_v0_2" && adapters.Profile != "simulation_v0_2" {
			return nil, fmt.Errorf("ERR_SIMULATION_DISABLED")
		}
		if adapters.Profile != "" && adapters.Profile != c.Governance.CryptoProfile {
			return nil, fmt.Errorf("ERR_CRYPTO_PROFILE")
		}
		var err error
		n.Governance, err = governance.New(*c.Governance)
		if err != nil {
			return nil, err
		}
	}
	r := c.Registry
	last := uint64(0)
	for _, a := range c.Activations {
		if e = a.CheckFixture(c.NetworkID, c.ChainID); e != nil {
			return nil, e
		}
		h, _ := codec.Uint(a.Instance.Heights[0].Height)
		if h <= last {
			return nil, fmt.Errorf("activation order")
		}
		last = h
		if r, e = r.Next(a.Plan); e != nil {
			return nil, e
		}
		if _, ok := n.upgrades[a.PlanID]; ok {
			return nil, fmt.Errorf("duplicate plan")
		}
		n.upgrades[a.PlanID] = UpgradeState{a.Plan, a.Instance, "armed"}
	}
	return n, nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func (e *Engine) Snapshot() State {
	height, tm := strconv.FormatUint(e.Height, 10), strconv.FormatUint(e.Time, 10)
	network := struct {
		Config      Config                   `json:"config"`
		Height      string                   `json:"height"`
		TimeMS      string                   `json:"time_ms"`
		Governance  *governance.NetworkState `json:"governance,omitempty"`
		Eligibility *eligibility.Metadata    `json:"eligibility,omitempty"`
	}{Config: e.Config, Height: height, TimeMS: tm}
	if e.Governance != nil {
		network.Governance = &e.Governance.Network
	}
	if e.Eligibility != nil {
		meta := e.Eligibility.Metadata(e.Config.NetworkID, e.Config.Registry.Epoch, "accounts", e.Config.Eligibility.SuiteID, height)
		network.Eligibility = &meta
	}
	entries := []state.Entry{{Table: "network", Key: "config", Value: network}}
	entries = append(entries, e.Security.Entries()...)
	for id, a := range e.Accounts {
		entries = append(entries, state.Entry{Table: "accounts", Key: id, Value: a})
	}
	for id, u := range e.upgrades {
		entries = append(entries, state.Entry{Table: "upgrades", Key: id, Value: u})
	}
	if e.Governance != nil {
		entries = append(entries, e.Governance.Entries()...)
	}
	if e.Eligibility != nil {
		for i, node := range e.Eligibility.Levels[0] {
			entries = append(entries, state.Entry{Table: "eligibility", Key: strconv.Itoa(i), Value: node.String()})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		return string(codec.Must(a.Key)) < string(codec.Must(b.Key))
	})
	return State{height, tm, entries}
}
func (e *Engine) Root() string { return state.Root(e.Snapshot().Entries) }
func (e *Engine) Clone() *Engine {
	n := *e
	n.Security = e.Security.Clone()
	if e.Eligibility != nil {
		n.Eligibility = e.Eligibility.Clone()
	}
	n.Accounts = make(map[string]Account, len(e.Accounts))
	for id, a := range e.Accounts {
		a.UsedKeys = append([]string{}, a.UsedKeys...)
		a.UsedCommitments = append([]string{}, a.UsedCommitments...)
		n.Accounts[id] = a
	}
	n.upgrades = make(map[string]UpgradeState, len(e.upgrades))
	if e.Governance != nil {
		n.Governance = e.Governance.Clone()
	}
	for id, u := range e.upgrades {
		n.upgrades[id] = u
	}
	return &n
}
func TxID(b Body) string {
	tag := "tx"
	if b.V == "2" {
		tag = "tx/2"
	}
	return codec.Hash(tag, b)
}
func (e *Engine) BeforeBlock(height string) error {
	h, err := codec.Uint(height)
	if err != nil {
		return err
	}
	for _, a := range e.Config.Activations {
		if a.At(h) {
			to, _ := codec.Uint(a.Plan.ToEpoch)
			if to > e.SupportedEpoch {
				return fmt.Errorf("ERR_UNSUPPORTED_EPOCH")
			}
		}
	}
	return nil
}
func (e *Engine) Authorize(epoch, op, moduleID string, historical bool) error {
	if !historical && e.Security.ModuleBlocked(moduleID) {
		return fmt.Errorf("ERR_SECURITY_MODULE_REVIEW")
	}
	return e.Config.Registry.Authorize(epoch, op, moduleID, historical)
}

// Apply is transactional: any invalid transaction rejects the whole proposed
// block. Neither events nor nonces escape the discarded candidate state.
func (e *Engine) Apply(raw []byte) (*Engine, []Event, error) {
	if len(raw) > MaxBlockBytes {
		return nil, nil, fmt.Errorf("ERR_SIZE")
	}
	var b Block
	if err := codec.Decode(raw, "block", &b); err != nil {
		return nil, nil, err
	}
	votes, ceremonies := 0, 0
	for _, tx := range b.Transactions {
		switch tx.Body.Type {
		case "vote.cast":
			votes++
		case "poll.configure", "poll.tally":
			ceremonies++
		}
	}
	if votes > 32 || ceremonies > 4 {
		return nil, nil, fmt.Errorf("ERR_BLOCK_BUDGET")
	}
	h, err := codec.Uint(b.Height)
	if err != nil || e.Height == ^uint64(0) || h != e.Height+1 {
		return nil, nil, fmt.Errorf("ERR_HEIGHT")
	}
	tm, err := codec.Uint(b.TimeMS)
	if err != nil {
		return nil, nil, err
	}
	if err = e.BeforeBlock(b.Height); err != nil {
		return nil, nil, err
	}
	n := e.Clone()
	n.Height = h
	if tm > n.Time {
		n.Time = tm
	}
	events := []Event{}
	emit := func(kind, subject string) {
		events = append(events, Event{codec.Hash("event", []any{n.Config.NetworkID, b.Height, strconv.Itoa(len(events))}), kind, subject})
	}
	for _, a := range n.Config.Activations {
		if !a.At(h) {
			continue
		}
		if n.Governance != nil && n.Governance.LivePolls() > 0 {
			return nil, nil, fmt.Errorf("ERR_UPGRADE_BARRIER")
		}
		lo, _ := codec.Uint(a.Plan.Activation.NotBeforeMS)
		hi, _ := codec.Uint(a.Instance.ExpiresAtMS)
		armEnd, _ := codec.Uint(a.Instance.ArmWindowEndMS)
		notice, _ := codec.Uint(a.Plan.Activation.NoticeMS)
		if n.Time < lo || n.Time >= hi || armEnd > ^uint64(0)-notice || n.Time < armEnd+notice {
			return nil, nil, fmt.Errorf("activation outside fixed window")
		}
		registry, err := n.Config.Registry.Next(a.Plan)
		if err != nil {
			return nil, nil, err
		}
		n.Config.Registry = registry
		u := n.upgrades[a.PlanID]
		u.Status = "activated"
		n.upgrades[a.PlanID] = u
		emit("protocol.activate", a.PlanID)
	}
	if err := n.AcceptSecurity(b.SecurityOrders, n.Time, emit); err != nil {
		return nil, nil, err
	}
	if n.Governance != nil {
		if err := n.Governance.Advance(n.execution(e, emit)); err != nil {
			return nil, nil, err
		}
	}
	for i, tx := range b.Transactions {
		if err := n.applyTransaction(tx, emit, strconv.Itoa(i)); err != nil {
			return nil, nil, err
		}
		subject := tx.Body.ActorID
		if isProof(tx.Body.Type) {
			var p struct {
				PollID string `json:"poll_id"`
			}
			_ = json.Unmarshal(tx.Body.Payload, &p)
			subject = p.PollID
		}
		emit(tx.Body.Type, subject)
	}
	if n.Governance != nil {
		n.InterruptAffected(n.Time, emit)
		if err := n.Governance.Qualify(n.execution(e, emit)); err != nil {
			return nil, nil, err
		}
	}
	return n, events, nil
}
func (e *Engine) CheckTx(raw []byte) error {
	if len(raw) > MaxTxBytes {
		return fmt.Errorf("ERR_SIZE")
	}
	var tx Transaction
	if err := codec.Decode(raw, "transaction", &tx); err != nil {
		return err
	}
	return e.Clone().applyTransaction(tx, func(string, string) {}, "0")
}
func (e *Engine) applyAccount(tx Transaction) error {
	if len(codec.Must(tx)) > MaxTxBytes {
		return fmt.Errorf("ERR_SIZE")
	}
	b := tx.Body
	if b.NetworkID != e.Config.NetworkID {
		return fmt.Errorf("ERR_NETWORK")
	}
	if b.V != e.Config.Registry.TransactionVersion || b.V == "1" && e.Config.Registry.Epoch != "1" || b.V == "2" && b.Epoch != e.Config.Registry.Epoch {
		return fmt.Errorf("ERR_EPOCH")
	}
	if _, err := e.Config.Registry.AccountSuite(); err != nil {
		return err
	}
	nonce, err := codec.Uint(b.Nonce)
	if err != nil {
		return err
	}
	if nonce == ^uint64(0) {
		return fmt.Errorf("ERR_NONCE_OVERFLOW")
	}
	var p Payload
	if b.Type != "account.retire" {
		if err = json.Unmarshal(b.Payload, &p); err != nil {
			return err
		}
		q, _ := new(big.Int).SetString("21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)
		v, ok := new(big.Int).SetString(p.Commitment, 10)
		if !ok || v.Sign() <= 0 || v.Cmp(q) >= 0 {
			return fmt.Errorf("ERR_COMMITMENT")
		}
		if p.SigningPK == p.RecoveryPK {
			return fmt.Errorf("ERR_KEY_SEPARATION")
		}
	}
	a, exists := e.Accounts[b.ActorID]
	pk := a.RecoveryPK
	if b.Type == "account.register" {
		if exists || nonce != 0 || b.ActorID != codec.Hash("account", []any{b.NetworkID, p.SigningPK}) {
			return fmt.Errorf("ERR_REGISTER")
		}
		active := 0
		for _, a := range e.Accounts {
			if a.Status == "active" {
				active++
			}
		}
		if len(e.Accounts) >= 131072 || active >= 65536 {
			return fmt.Errorf("ERR_CAPACITY")
		}
		pk = p.SigningPK
	} else {
		if !exists || a.Status != "active" {
			return fmt.Errorf("ERR_ACCOUNT")
		}
		if b.Nonce != a.NextNonce {
			return fmt.Errorf("ERR_NONCE")
		}
	}
	if b.Type != "account.retire" {
		for id, other := range e.Accounts {
			for _, k := range other.UsedKeys {
				if id != b.ActorID && (k == p.SigningPK || k == p.RecoveryPK) {
					return fmt.Errorf("ERR_KEY_IN_USE")
				}
			}
			for _, c := range other.UsedCommitments {
				if c == p.Commitment && (id != b.ActorID || c != a.Commitment) {
					return fmt.Errorf("ERR_COMMITMENT_USED")
				}
			}
		}
	}
	key, _ := hex.DecodeString(pk)
	sig, _ := hex.DecodeString(tx.Auth.Signature)
	digest, _ := hex.DecodeString(TxID(b))
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, digest, sig) {
		return fmt.Errorf("ERR_SIGNATURE")
	}
	if b.Type == "account.retire" {
		a.Status = "retired"
	} else {
		if !exists {
			a = Account{Status: "active", RegisteredHeight: strconv.FormatUint(e.Height, 10), UsedKeys: []string{}, UsedCommitments: []string{}}
		}
		a.SigningPK = p.SigningPK
		a.RecoveryPK = p.RecoveryPK
		a.Commitment = p.Commitment
		for _, k := range []string{p.SigningPK, p.RecoveryPK} {
			if !contains(a.UsedKeys, k) {
				a.UsedKeys = append(a.UsedKeys, k)
			}
		}
		if !contains(a.UsedCommitments, p.Commitment) {
			a.UsedCommitments = append(a.UsedCommitments, p.Commitment)
		}
	}
	a.NextNonce = strconv.FormatUint(nonce+1, 10)
	if len(a.UsedKeys) > 262144 || len(a.UsedCommitments) > 262144 {
		return fmt.Errorf("ERR_CAPACITY")
	}
	e.Accounts[b.ActorID] = a
	return nil
}
func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
