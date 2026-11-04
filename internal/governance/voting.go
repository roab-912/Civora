package governance

import (
	"civora/internal/codec"
	"civora/internal/ports"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type GuardianAck struct {
	AccountID string `json:"account_id"`
	Signature string `json:"signature"`
}
type Configuration struct {
	PollID         string        `json:"poll_id"`
	CeremonyBundle string        `json:"ceremony_bundle"`
	GuardianAcks   []GuardianAck `json:"guardian_acks"`
}
type Membership struct {
	Depth     string   `json:"merkleTreeDepth"`
	Root      string   `json:"merkleTreeRoot"`
	Nullifier string   `json:"nullifier"`
	Message   string   `json:"message"`
	Scope     string   `json:"scope"`
	Points    []string `json:"points"`
}
type Vote struct {
	PollID          string     `json:"poll_id"`
	EncryptedBallot string     `json:"encrypted_ballot"`
	Membership      Membership `json:"membership"`
}
type Tally struct {
	PollID           string `json:"poll_id"`
	Yes              string `json:"yes"`
	No               string `json:"no"`
	AggregateDigest  string `json:"aggregate_digest"`
	DecryptionRecord string `json:"decryption_record"`
}

func (s *State) context(x Execution, id string, poll *Poll) ports.PollContext {
	return ports.PollContext{NetworkID: x.NetworkID, PollID: id, ProposalID: poll.ProposalID, Epoch: poll.ProtocolEpoch, EligibilitySuite: poll.EligibilitySuite, BallotSuite: poll.BallotSuite, TallySuite: poll.TallySuite, GroupRoot: poll.GroupRoot, Commitments: append([]string{}, poll.Commitments...), CeremonyDigest: val(poll.CeremonyDigest)}
}
func (s *State) poll(id, phase string) (*Poll, *Proposal, error) {
	poll, ok := s.Polls[id]
	if !ok {
		return nil, nil, fmt.Errorf("ERR_POLL")
	}
	p := s.Proposals[poll.ProposalID]
	if p.State != phase {
		return nil, nil, fmt.Errorf("ERR_PHASE")
	}
	return poll, p, nil
}
func (s *State) Configure(x Execution, raw []byte) error {
	var req Configuration
	if e := json.Unmarshal(raw, &req); e != nil {
		return e
	}
	poll, p, e := s.poll(req.PollID, "preparing")
	if e != nil {
		return e
	}
	if x.Time >= num(*p.SetupDueMS) {
		return fmt.Errorf("ERR_PHASE")
	}
	for _, check := range []struct{ op, id string }{{"eligibility.prove", poll.EligibilitySuite}, {"ballot.prove", poll.BallotSuite}, {"tally.prove", poll.TallySuite}} {
		if e = x.Authorize(poll.ProtocolEpoch, check.op, check.id, false); e != nil {
			return e
		}
	}
	if x.Crypto == nil {
		return fmt.Errorf("ERR_VOTING_ADAPTER")
	}
	bundle, e := codec.Blob(req.CeremonyBundle)
	if e != nil {
		return e
	}
	if _, e = codec.Parse(bundle); e != nil {
		return e
	}
	digest := sha256.Sum256(bundle)
	digestHex := hex.EncodeToString(digest[:])
	ackDigest, _ := hex.DecodeString(codec.Hash("poll/ceremony", []any{x.NetworkID, req.PollID, digestHex}))
	if len(req.GuardianAcks) != len(p.Guardians) {
		return fmt.Errorf("ERR_GUARDIAN_ACKS")
	}
	keys := map[string]string{}
	for _, g := range p.Guardians {
		keys[g.AccountID] = g.SigningPK
	}
	previous := ""
	for _, ack := range req.GuardianAcks {
		pk, ok := keys[ack.AccountID]
		if !ok || ack.AccountID <= previous {
			return fmt.Errorf("ERR_GUARDIAN_ACKS")
		}
		previous = ack.AccountID
		key, _ := hex.DecodeString(pk)
		sig, _ := hex.DecodeString(ack.Signature)
		if !ed25519.Verify(key, ackDigest, sig) {
			return fmt.Errorf("ERR_GUARDIAN_ACKS")
		}
	}
	poll.CeremonyDigest = ptr(digestHex)
	context := s.context(x, req.PollID, poll)
	aggregate, e := x.Crypto.Configure(context, bundle)
	if e != nil {
		return e
	}
	opens, e := add(x.Time, num(p.Policy.NoticeMS))
	if e != nil {
		return e
	}
	closes, e := add(opens, num(p.Policy.VotingMS))
	if e != nil {
		return e
	}
	tally, e := add(closes, num(p.Policy.TallyMS))
	if e != nil {
		return e
	}
	poll.Aggregate = aggregate
	poll.CeremonyBundle = ptr(req.CeremonyBundle)
	poll.OpensAtMS = ptr(str(opens))
	poll.ClosesAtMS = ptr(str(closes))
	poll.TallyDueAtMS = ptr(str(tally))
	s.setState(x, poll.ProposalID, "scheduled")
	return nil
}
func (s *State) Cast(x Execution, raw []byte, txID, position string) error {
	var req Vote
	if e := json.Unmarshal(raw, &req); e != nil {
		return e
	}
	poll, _, e := s.poll(req.PollID, "open")
	if e != nil {
		return e
	}
	if x.Time < num(*poll.OpensAtMS) || x.Time >= num(*poll.ClosesAtMS) || poll.GroupRoot == nil || num(poll.Count) >= num(poll.EligibleCount) {
		return fmt.Errorf("ERR_VOTE_WINDOW")
	}
	for _, check := range []struct{ op, id string }{{"eligibility.prove", poll.EligibilitySuite}, {"ballot.prove", poll.BallotSuite}} {
		if e = x.Authorize(poll.ProtocolEpoch, check.op, check.id, false); e != nil {
			return e
		}
	}
	if req.Membership.Root != *poll.GroupRoot {
		return fmt.Errorf("ERR_GROUP_ROOT")
	}
	if _, ok := s.Spent[tuple(req.PollID, req.Membership.Nullifier)]; ok {
		return fmt.Errorf("ERR_NULLIFIER_SPENT")
	}
	if x.Crypto == nil {
		return fmt.Errorf("ERR_VOTING_ADAPTER")
	}
	ballotBytes, e := codec.Blob(req.EncryptedBallot)
	if e != nil {
		return e
	}
	if x.Membership != nil {
		if e = x.Membership.VerifyMembership(ports.BulletinContext{NetworkID: x.NetworkID, PollID: req.PollID, Epoch: poll.ProtocolEpoch, EligibilitySuite: poll.EligibilitySuite, BallotSuite: poll.BallotSuite, TallySuite: poll.TallySuite}, *poll.GroupRoot, req.Membership.Depth, ballotBytes, codec.Must(req.Membership)); e != nil {
			return e
		}
	}
	accepted, e := x.Crypto.Accept(s.context(x, req.PollID, poll), poll.Aggregate, raw)
	if e != nil {
		return e
	}
	if accepted.Nullifier != req.Membership.Nullifier || accepted.BallotID == "" || accepted.CiphertextDigest == "" {
		return fmt.Errorf("ERR_BALLOT_BINDING")
	}
	if _, ok := s.Ballots[tuple(req.PollID, accepted.BallotID)]; ok {
		return fmt.Errorf("ERR_BALLOT_DUPLICATE")
	}
	if _, ok := s.Ciphertexts[tuple(req.PollID, accepted.CiphertextDigest)]; ok {
		return fmt.Errorf("ERR_CIPHERTEXT_DUPLICATE")
	}
	count, e := add(num(poll.Count), 1)
	if e != nil {
		return e
	}
	poll.Count = str(count)
	poll.Aggregate = accepted.Aggregate
	s.Spent[tuple(req.PollID, accepted.Nullifier)] = Spent{txID, x.Height, position}
	s.Ballots[tuple(req.PollID, accepted.BallotID)] = Ballot{codec.Hash("vote/envelope", json.RawMessage(raw)), accepted.ConfirmationCode, x.Height, txID}
	s.Ciphertexts[tuple(req.PollID, accepted.CiphertextDigest)] = txID
	return nil
}
func (s *State) Tally(x Execution, raw []byte) error {
	var req Tally
	if e := json.Unmarshal(raw, &req); e != nil {
		return e
	}
	poll, p, e := s.poll(req.PollID, "tallying")
	if e != nil {
		return e
	}
	if poll.Count == "0" || x.Time >= num(*poll.TallyDueAtMS) || val(poll.AggregateDigest) != req.AggregateDigest {
		return fmt.Errorf("ERR_AGGREGATE")
	}
	if e = x.Authorize(poll.ProtocolEpoch, "tally.prove", poll.TallySuite, false); e != nil {
		return e
	}
	if x.Crypto == nil {
		return fmt.Errorf("ERR_VOTING_ADAPTER")
	}
	if _, e = codec.Blob(req.DecryptionRecord); e != nil {
		return e
	}
	result, e := x.Crypto.Tally(s.context(x, req.PollID, poll), poll.Aggregate, poll.Count, raw)
	if e != nil {
		return e
	}
	if result.Yes != req.Yes || result.No != req.No {
		return fmt.Errorf("ERR_TALLY_RESULT")
	}
	yes, e := codec.Uint(result.Yes)
	if e != nil {
		return e
	}
	no, e := codec.Uint(result.No)
	if e != nil {
		return e
	}
	if yes > ^uint64(0)-no || yes+no != num(poll.Count) {
		return fmt.Errorf("ERR_TALLY_COUNT")
	}
	adopted, e := Adopted(result.Yes, result.No, poll.EligibleCount, p.Policy, p.Action == "policy.update")
	if e != nil {
		return e
	}
	poll.Result = &Result{result.Yes, result.No, false, x.Height, str(x.Time)}
	if !adopted {
		s.setState(x, poll.ProposalID, "rejected")
		return nil
	}
	delay := num(p.Policy.OrdinaryExecutionDelayMS)
	if p.Action == "policy.update" {
		delay = num(p.Policy.ConstitutionalExecutionDelayMS)
	}
	effect, e := add(x.Time, delay)
	if e != nil {
		return e
	}
	if p.RequestedEffectMS != nil && num(*p.RequestedEffectMS) > effect {
		effect = num(*p.RequestedEffectMS)
	}
	p.EffectiveAtMS = ptr(str(effect))
	s.setState(x, poll.ProposalID, "adopted")
	return nil
}
