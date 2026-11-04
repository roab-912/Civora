//go:build civora_sim

// Package simulation is a PUBLIC TEST-ONLY stand-in for voting cryptography.
// Its ballots reveal the choice and commitment. It is never linked into civnode.
package simulation

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/governance"
	"civora/internal/ports"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const Profile = "simulation_v0_2"

func NetworkID() string {
	h := sha256.Sum256([]byte("CIVORA PUBLIC SIMULATION NETWORK v0.2 — NOT A PILOT"))
	return hex.EncodeToString(h[:])
}
func New(raw []byte, supported uint64) (*core.Engine, error) {
	var c core.Config
	if e := codec.Decode(raw, "config", &c); e != nil {
		return nil, e
	}
	if c.NetworkID != NetworkID() || c.Mode != "development" || c.Governance == nil || c.Governance.CryptoProfile != Profile {
		return nil, fmt.Errorf("ERR_SIMULATION_GENESIS")
	}
	return core.NewWithAdapters(raw, supported, core.Adapters{Eligibility: Adapter{}, Voting: Adapter{}, Profile: Profile})
}

type Adapter struct{}

func (Adapter) Snapshot(r ports.EligibilityRequest) (ports.EligibilityGroup, error) {
	commitments := []string{}
	for _, a := range r.Accounts {
		commitments = append(commitments, a.Commitment)
	}
	if len(commitments) == 0 {
		return ports.EligibilityGroup{Commitments: commitments}, nil
	}
	value := uintDigest("simulation/group", []any{r.NetworkID, r.PollID, r.Height, stringsAny(commitments)})
	return ports.EligibilityGroup{Root: &value, Commitments: commitments}, nil
}
func stringsAny(ss []string) []any {
	v := make([]any, len(ss))
	for i, s := range ss {
		v[i] = s
	}
	return v
}
func uintDigest(tag string, value any) string {
	b, _ := hex.DecodeString(codec.Hash(tag, value))
	return new(big.Int).SetBytes(b[:31]).String()
}
func decode(raw []byte, out any) error {
	if _, e := codec.Parse(raw); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(out)
}

type Ceremony struct {
	Suite          string `json:"suite"`
	NetworkID      string `json:"network_id"`
	PollID         string `json:"poll_id"`
	ProposalID     string `json:"proposal_id"`
	QuestionDigest string `json:"question_digest"`
}

func (Adapter) Configure(c ports.PollContext, raw []byte) (string, error) {
	var ceremony Ceremony
	if e := decode(raw, &ceremony); e != nil {
		return "", e
	}
	if ceremony.Suite != Profile || ceremony.NetworkID != c.NetworkID || ceremony.PollID != c.PollID || ceremony.ProposalID != c.ProposalID || ceremony.QuestionDigest != codec.Hash("poll/question", []any{c.NetworkID, c.PollID, c.ProposalID}) {
		return "", fmt.Errorf("ERR_SIMULATION_CEREMONY")
	}
	return "0", nil
}

type PlainTestBallot struct {
	Suite    string `json:"suite"`
	Yes      string `json:"yes"`
	BallotID string `json:"ballot_id"`
}

func voteProof(c ports.PollContext, commitment, encoded string) string {
	return uintDigest("simulation/membership", []any{c.NetworkID, c.PollID, *c.GroupRoot, commitment, encoded, c.CeremonyDigest})
}
func Vote(c ports.PollContext, commitment, yes, id string) governance.Vote {
	b := codec.Must(PlainTestBallot{Profile, yes, id})
	encoded := base64.RawURLEncoding.EncodeToString(b)
	digest := sha256.Sum256(b)
	return governance.Vote{PollID: c.PollID, EncryptedBallot: encoded, Membership: governance.Membership{Depth: "16", Root: *c.GroupRoot, Nullifier: uintDigest("simulation/nullifier", []any{c.NetworkID, c.PollID, commitment}), Message: uintDigest("vote/message", []any{c.NetworkID, c.PollID, hex.EncodeToString(digest[:])}), Scope: uintDigest("vote/scope", []any{c.NetworkID, c.PollID}), Points: []string{commitment, voteProof(c, commitment, encoded), "0", "0", "0", "0", "0", "0"}}}
}
func (Adapter) Accept(c ports.PollContext, aggregate string, raw []byte) (ports.AcceptedBallot, error) {
	var req governance.Vote
	if e := decode(raw, &req); e != nil {
		return ports.AcceptedBallot{}, e
	}
	if c.GroupRoot == nil || len(req.Membership.Points) != 8 {
		return ports.AcceptedBallot{}, fmt.Errorf("ERR_SIMULATION_PROOF")
	}
	b, e := codec.Blob(req.EncryptedBallot)
	if e != nil {
		return ports.AcceptedBallot{}, e
	}
	var ballot PlainTestBallot
	if e = decode(b, &ballot); e != nil {
		return ports.AcceptedBallot{}, e
	}
	if ballot.Suite != Profile || ballot.Yes != "0" && ballot.Yes != "1" || len(ballot.BallotID) != 64 {
		return ports.AcceptedBallot{}, fmt.Errorf("ERR_SIMULATION_BALLOT")
	}
	if _, err := hex.DecodeString(ballot.BallotID); err != nil || strings.ToLower(ballot.BallotID) != ballot.BallotID {
		return ports.AcceptedBallot{}, fmt.Errorf("ERR_SIMULATION_BALLOT")
	}
	commitment := req.Membership.Points[0]
	member := false
	for _, s := range c.Commitments {
		if s == commitment {
			member = true
		}
	}
	if !member {
		return ports.AcceptedBallot{}, fmt.Errorf("ERR_SIMULATION_MEMBER")
	}
	expected := Vote(c, commitment, ballot.Yes, ballot.BallotID)
	if string(codec.Must(expected)) != string(codec.Must(req)) {
		return ports.AcceptedBallot{}, fmt.Errorf("ERR_SIMULATION_PROOF")
	}
	n, e := codec.Uint(aggregate)
	if e != nil {
		return ports.AcceptedBallot{}, e
	}
	if ballot.Yes == "1" {
		n++
	}
	return ports.AcceptedBallot{Nullifier: req.Membership.Nullifier, BallotID: ballot.BallotID, CiphertextDigest: codec.Hash("vote/ciphertext", ballot), ConfirmationCode: codec.Hash("simulation/confirmation", ballot), Aggregate: strconv.FormatUint(n, 10)}, nil
}

type Decryption struct {
	Suite           string `json:"suite"`
	NetworkID       string `json:"network_id"`
	PollID          string `json:"poll_id"`
	AggregateDigest string `json:"aggregate_digest"`
	Yes             string `json:"yes"`
	No              string `json:"no"`
	Proof           string `json:"proof"`
}

func Tally(c ports.PollContext, aggregate, count, digest string) governance.Tally {
	yes, _ := codec.Uint(aggregate)
	total, _ := codec.Uint(count)
	no := strconv.FormatUint(total-yes, 10)
	record := Decryption{Profile, c.NetworkID, c.PollID, digest, aggregate, no, codec.Hash("simulation/tally", []any{c.NetworkID, c.PollID, digest, aggregate, no, count, c.CeremonyDigest})}
	return governance.Tally{PollID: c.PollID, Yes: aggregate, No: no, AggregateDigest: digest, DecryptionRecord: base64.RawURLEncoding.EncodeToString(codec.Must(record))}
}
func (Adapter) Tally(c ports.PollContext, aggregate, count string, raw []byte) (ports.TallyResult, error) {
	var req governance.Tally
	if e := decode(raw, &req); e != nil {
		return ports.TallyResult{}, e
	}
	n, e := codec.Uint(aggregate)
	if e != nil {
		return ports.TallyResult{}, e
	}
	total, e := codec.Uint(count)
	if e != nil || n > total {
		return ports.TallyResult{}, fmt.Errorf("ERR_SIMULATION_AGGREGATE")
	}
	expected := Tally(c, aggregate, count, req.AggregateDigest)
	if string(codec.Must(expected)) != string(codec.Must(req)) {
		return ports.TallyResult{}, fmt.Errorf("ERR_SIMULATION_TALLY")
	}
	return ports.TallyResult{Yes: expected.Yes, No: expected.No}, nil
}
