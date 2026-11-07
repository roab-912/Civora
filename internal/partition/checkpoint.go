package partition

import (
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/electionguard"
	"civora/internal/state"
	"civora/spec"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

type BatchProof struct {
	Reference   BatchRef    `json:"reference"`
	Certificate []Signature `json:"certificate"`
	Inclusion   []string    `json:"inclusion"`
}
type Checkpoint struct {
	Proof                consensus.StateProof `json:"proof"`
	PreviousID           string               `json:"previous_id"`
	AdmissionConsistency []string             `json:"admission_consistency"`
	BatchConsistency     []string             `json:"batch_consistency"`
	Extension            []BatchProof         `json:"extension"`
}
type Verified struct {
	ID      string  `json:"id"`
	Summary Summary `json:"summary"`
}

func (m *Machine) Checkpoint(id string, proof consensus.StateProof, previous *Verified) (Checkpoint, error) {
	p := m.Polls[id]
	if p == nil {
		return Checkpoint{}, fmt.Errorf("unknown poll")
	}
	oldAdmitted, oldBatches := uint64(0), uint64(0)
	oldID := strings.Repeat("0", 64)
	if previous != nil {
		oldAdmitted, _ = codec.Uint(previous.Summary.Admitted)
		oldBatches, _ = codec.Uint(previous.Summary.Batches)
		oldID = previous.ID
	}
	if oldAdmitted > uint64(len(p.Deposits)) || oldBatches > uint64(len(p.Batches)) {
		return Checkpoint{}, fmt.Errorf("checkpoint prefix")
	}
	ac, _ := Consistency(admissionItems(p), int(oldAdmitted))
	bc, _ := Consistency(batchItems(p), int(oldBatches))
	cp := Checkpoint{proof, oldID, ac, bc, []BatchProof{}}
	for i := int(oldBatches); i < len(p.Batches); i++ {
		b := p.Batches[i]
		path, _ := Inclusion(batchItems(p), i)
		cp.Extension = append(cp.Extension, BatchProof{b.Reference, b.Certificate, path})
	}
	return cp, nil
}
func VerifyCheckpoint(f Frozen, cp Checkpoint, previous *Verified) (*Verified, error) {
	if e := codec.Decode(codec.Must(cp), "partition_checkpoint", new(Checkpoint)); e != nil {
		return nil, e
	}
	var s Summary
	if e := codec.Decode(codec.Must(cp.Proof.Path.Entry.Value), "partition_summary", &s); e != nil {
		return nil, e
	}
	lane, _ := codec.Uint(s.Lane)
	if lane >= uint64(len(f.Lanes)) {
		return nil, fmt.Errorf("checkpoint lane")
	}
	header, e := consensus.VerifyState(f.Lanes[lane], cp.Proof)
	if e != nil {
		return nil, e
	}
	if s.Interrupted {
		return nil, fmt.Errorf("ERR_SECURITY_INTERRUPTED")
	}
	if cp.Proof.Path.Entry.Table != "lane_checkpoint" || string(codec.Must(cp.Proof.Path.Entry.Key)) != string(codec.Must(f.Context.PollID)) || cp.Proof.Height != s.Height || s.NetworkID != f.Context.NetworkID || s.PollID != f.Context.PollID || s.ContextID != f.Context.Checkpoint || s.Epoch != f.Context.Epoch {
		return nil, fmt.Errorf("checkpoint context/path")
	}
	a, _ := codec.Uint(s.Admitted)
	p, _ := codec.Uint(s.Processed)
	c, _ := codec.Uint(s.Counted)
	b, _ := codec.Uint(s.Batches)
	tm, _ := codec.Uint(s.Time)
	closes, _ := codec.Uint(f.Closes)
	eligible, _ := codec.Uint(f.EligibleCount)
	if eligible > 10_000_000 || a > 2*eligible || p > a || c > p || c > eligible || b > a || s.AdmissionSealed != (tm >= closes) || s.ProcessingSealed != (tm >= closes && p == a) || header.Time.UnixMilli() < 0 || tm > uint64(header.Time.UnixMilli()) {
		return nil, fmt.Errorf("checkpoint counters/seal/time")
	}
	if p == 0 && s.StatusRoot != state.Root(nil) || c == 0 && s.SpentRoot != state.Root(nil) {
		return nil, fmt.Errorf("empty status/spent root")
	}
	if a == 0 && s.AdmissionRoot != LogRoot(nil) || b == 0 && s.BatchRoot != LogRoot(nil) {
		return nil, fmt.Errorf("empty admission/batch root")
	}
	old := Summary{Admitted: "0", Batches: "0", AdmissionRoot: LogRoot(nil), BatchRoot: LogRoot(nil), Processed: "0", Counted: "0", Height: "0", Time: "0"}
	expectedPrevious := strings.Repeat("0", 64)
	if previous != nil {
		old = previous.Summary
		expectedPrevious = previous.ID
	}
	if cp.PreviousID != expectedPrevious {
		return nil, fmt.Errorf("stale/forked checkpoint predecessor")
	}
	oa, _ := codec.Uint(old.Admitted)
	ob, _ := codec.Uint(old.Batches)
	op, _ := codec.Uint(old.Processed)
	oc, _ := codec.Uint(old.Counted)
	oh, _ := codec.Uint(old.Height)
	ot, _ := codec.Uint(old.Time)
	h, _ := codec.Uint(s.Height)
	if h <= oh || tm < ot || p < op || c < oc || old.AdmissionSealed && !s.AdmissionSealed || old.ProcessingSealed && !s.ProcessingSealed {
		return nil, fmt.Errorf("checkpoint regression")
	}
	if e = VerifyConsistency(old.AdmissionRoot, s.AdmissionRoot, oa, a, cp.AdmissionConsistency); e != nil {
		return nil, e
	}
	if e = VerifyConsistency(old.BatchRoot, s.BatchRoot, ob, b, cp.BatchConsistency); e != nil {
		return nil, e
	}
	if b < ob || uint64(len(cp.Extension)) != b-ob || len(cp.Extension) > 64 {
		return nil, fmt.Errorf("missing batch extension")
	}
	next := oa
	for i, x := range cp.Extension {
		r := x.Reference
		start, _ := codec.Uint(r.Start)
		count, _ := codec.Uint(r.Count)
		if r.NetworkID != s.NetworkID || r.PollID != s.PollID || r.Lane != s.Lane || r.Epoch != s.Epoch || start != next || count == 0 || count > AdmissionBudget {
			return nil, fmt.Errorf("batch interval/domain")
		}
		next += count
		if e = VerifyCertificate(r, x.Certificate, f.ArchiveKeys); e != nil {
			return nil, e
		}
		if e = VerifyInclusion(s.BatchRoot, codec.Must(r), ob+uint64(i), b, x.Inclusion); e != nil {
			return nil, e
		}
	}
	if next != a {
		return nil, fmt.Errorf("admitted batches missing")
	}
	if _, e = Combine([]string{s.Aggregate}); e != nil {
		return nil, e
	}
	v, _, e := f.Election()
	if e != nil {
		return nil, e
	}
	if c == 0 && s.Aggregate != v.Zero() {
		return nil, fmt.Errorf("nonzero empty aggregate")
	}
	return &Verified{codec.Hash("scale/checkpoint", []any{cp.Proof.Height, cp.Proof.SignedHeader, s}), s}, nil
}

// Combine validates subgroup membership, including the neutral element, and
// multiplies lane ciphertexts in caller-specified lane order. No decryption.
func Combine(aggregates []string) (string, error) {
	if _, _, e := electionguard.PinnedModule(); e != nil {
		return "", e
	}
	raw, e := spec.Crypto.ReadFile("crypto/electionguard-v2.parameters.json")
	if e != nil {
		return "", e
	}
	var params struct{ P, Q string }
	if e = json.Unmarshal(raw, &params); e != nil {
		return "", e
	}
	pb, _ := codec.Blob(params.P)
	qb, _ := codec.Blob(params.Q)
	mod, q := new(big.Int).SetBytes(pb), new(big.Int).SetBytes(qb)
	a, b := big.NewInt(1), big.NewInt(1)
	for _, aggregate := range aggregates {
		var cipher electionguard.Ciphertext
		if e = codec.Decode([]byte(aggregate), "eg_ciphertext", &cipher); e != nil {
			return "", e
		}
		operands := []*big.Int{}
		for _, s := range []string{cipher.Alpha, cipher.Beta} {
			bs, err := codec.Blob(s)
			if err != nil || len(bs) != 512 {
				return "", fmt.Errorf("aggregate group encoding")
			}
			x := new(big.Int).SetBytes(bs)
			if x.Sign() <= 0 || x.Cmp(mod) >= 0 || new(big.Int).Exp(x, q, mod).Cmp(big.NewInt(1)) != 0 {
				return "", fmt.Errorf("aggregate subgroup")
			}
			operands = append(operands, x)
		}
		a.Mod(a.Mul(a, operands[0]), mod)
		b.Mod(b.Mul(b, operands[1]), mod)
	}
	enc := func(x *big.Int) string { return base64.RawURLEncoding.EncodeToString(x.FillBytes(make([]byte, 512))) }
	return string(codec.Must(electionguard.Ciphertext{Alpha: enc(a), Beta: enc(b)})), nil
}
func Global(f Frozen, checkpoints map[string]*Verified) (string, string, error) {
	aggregates := []string{}
	count := uint64(0)
	for lane := range f.Lanes {
		cp := checkpoints[fmt.Sprint(lane)]
		if cp == nil || !cp.Summary.ProcessingSealed || cp.Summary.Interrupted {
			return "", "", fmt.Errorf("missing final lane %d", lane)
		}
		n, _ := codec.Uint(cp.Summary.Counted)
		count += n
		aggregates = append(aggregates, cp.Summary.Aggregate)
	}
	eligible, _ := codec.Uint(f.EligibleCount)
	if count > eligible {
		return "", "", fmt.Errorf("global eligibility count")
	}
	aggregate, e := Combine(aggregates)
	return aggregate, fmt.Sprint(count), e
}
