package electionguard

import (
	"civora/internal/codec"
	"civora/internal/eligibility"
	"civora/internal/semaphore"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
)

type ScaleContext struct {
	NetworkID  string `json:"network_id"`
	PollID     string `json:"poll_id"`
	Epoch      string `json:"protocol_epoch"`
	Root       string `json:"group_root"`
	Depth      string `json:"circuit_depth"`
	Partitions string `json:"partition_count"`
	Checkpoint string `json:"checkpoint_id"`
}

const MaxScaleEnvelopeBytes = 256 * 1024

type Independence struct {
	Commitment string `json:"commitment"`
	Response   string `json:"response"`
}
type ScaleEnvelope struct {
	NetworkID    string               `json:"network_id"`
	PollID       string               `json:"poll_id"`
	Epoch        string               `json:"protocol_epoch"`
	Lane         string               `json:"lane_id"`
	Checkpoint   string               `json:"checkpoint_id"`
	Ballot       string               `json:"encrypted_ballot"`
	Independence Independence         `json:"independence"`
	Membership   semaphore.Membership `json:"membership"`
}

func Lane(network, poll, nullifier string, partitions uint64) (uint64, error) {
	if _, e := codec.ChainID(network); e != nil {
		return 0, e
	}
	if b, e := hex.DecodeString(poll); e != nil || len(b) != 32 || hex.EncodeToString(b) != poll {
		return 0, fmt.Errorf("ERR_SCALE_CONTEXT")
	}
	if partitions == 0 || partitions > 256 || partitions&(partitions-1) != 0 {
		return 0, fmt.Errorf("ERR_SCALE_PARTITIONS")
	}
	if _, e := eligibility.ParseField(nullifier, false); e != nil {
		return 0, e
	}
	h, _ := hex.DecodeString(codec.Hash("scale/lane", []any{network, poll, nullifier}))
	bits := uint(0)
	for p := partitions; p > 1; p >>= 1 {
		bits++
	}
	return uint64(h[0] >> (8 - bits)), nil
}
func ScaleBinding(c ScaleContext, ballotDigest, independenceDigest string) (string, string) {
	read := func(tag string, values []any) string {
		b, _ := hex.DecodeString(codec.Hash(tag, values))
		return new(big.Int).SetBytes(b[:31]).String()
	}
	return read("scale/scope", []any{c.NetworkID, c.PollID}), read("scale/message", []any{c.NetworkID, c.PollID, ballotDigest, independenceDigest})
}
func independenceContext(c ScaleContext, nullifier, digest string) []byte {
	b, _ := hex.DecodeString(codec.Hash("scale/independence", []any{c.NetworkID, c.PollID, c.Root, nullifier, digest}))
	return b
}
func (v *Verifier) VerifyIndependence(c ScaleContext, nullifier, digest, alpha string, p Independence) error {
	a, e := v.group(alpha, false)
	if e != nil {
		return e
	}
	commitment, e := v.group(p.Commitment, false)
	if e != nil {
		return e
	}
	response, e := v.scalar(p.Response)
	if e != nil {
		return e
	}
	h := sha256.New()
	// RFC 8235: UserID is the anonymous prover's poll-scoped nullifier,
	// namespaced by suite. OtherInfo is the full Civic context digest.
	for _, part := range [][]byte{fixed(v.g, 512), fixed(commitment, 512), fixed(a, 512), []byte(Suite + ":" + nullifier), independenceContext(c, nullifier, digest)} {
		h.Write(index(uint32(len(part))))
		h.Write(part)
	}
	challenge := v.modq(new(big.Int).SetBytes(h.Sum(nil)))
	if v.mul(v.exp(v.g, response), v.exp(a, challenge)).Cmp(commitment) != 0 {
		return fmt.Errorf("ERR_SCALE_INDEPENDENCE")
	}
	return nil
}

// VerifyScale validates an envelope against an ALREADY AUTHENTICATED frozen
// poll/checkpoint context. It does not authenticate BFT checkpoints, reserve a
// nullifier or count a ballot: those state transitions belong to v0.5.
func (v *Verifier) VerifyScale(election *Election, c ScaleContext, raw []byte) (*EncryptedBallot, error) {
	if len(raw) > MaxScaleEnvelopeBytes {
		return nil, fmt.Errorf("ERR_EG_SIZE")
	}
	if election.NetworkID != c.NetworkID || election.PollID != c.PollID || election.Epoch != c.Epoch {
		return nil, fmt.Errorf("ERR_SCALE_CONTEXT")
	}
	var checked ScaleContext
	if e := codec.Decode(codec.Must(c), "scale_context", &checked); e != nil {
		return nil, e
	}
	if c.NetworkID == "" || c.PollID == "" || c.Epoch == "0" {
		return nil, fmt.Errorf("ERR_SCALE_CONTEXT")
	}
	var envelope ScaleEnvelope
	if e := codec.Decode(raw, "scale_envelope", &envelope); e != nil {
		return nil, e
	}
	if envelope.NetworkID != c.NetworkID || envelope.PollID != c.PollID || envelope.Epoch != c.Epoch || envelope.Checkpoint != c.Checkpoint {
		return nil, fmt.Errorf("ERR_SCALE_CONTEXT")
	}
	partitions, _ := codec.Uint(c.Partitions)
	lane, e := Lane(c.NetworkID, c.PollID, envelope.Membership.Nullifier, partitions)
	if e != nil {
		return nil, e
	}
	if envelope.Lane != fmt.Sprint(lane) {
		return nil, fmt.Errorf("ERR_SCALE_LANE")
	}
	d, e := Decode(envelope.Ballot, Ballot)
	if e != nil {
		return nil, e
	}
	ballot, e := v.Ballot(election, c.PollID, d.Files["ballot.json"])
	if e != nil {
		return nil, e
	}
	bh := sha256.Sum256(d.Files["ballot.json"])
	ih := sha256.Sum256(codec.Must(envelope.Independence))
	digest := hex.EncodeToString(bh[:])
	if e = v.VerifyIndependence(c, envelope.Membership.Nullifier, digest, ballot.Contests[0].Selections[0].Cipher.Alpha, envelope.Independence); e != nil {
		return nil, e
	}
	scope, message := ScaleBinding(c, digest, hex.EncodeToString(ih[:]))
	s, e := semaphore.New()
	if e != nil {
		return nil, e
	}
	if e = s.VerifyBound(semaphore.Context{NetworkID: c.NetworkID, PollID: c.PollID, Epoch: c.Epoch, SuiteID: "semaphore-v4.14.5-depth-" + c.Depth, Depth: c.Depth, Root: c.Root}, envelope.Membership, scope, message); e != nil {
		return nil, e
	}
	return ballot, nil
}
