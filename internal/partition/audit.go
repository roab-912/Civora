package partition

import (
	"civora/internal/codec"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	abci "github.com/cometbft/cometbft/abci/types"
	"sort"
)

const GoAudit = "civic-go-v0.6"

func (m *Main) publish(f Frozen, r *Provisional) error {
	if f.Audit == nil {
		return nil
	}
	id, e := Dossier(f, *r, m.Checkpoints[f.Context.PollID])
	if e != nil {
		return e
	}
	r.Dossier = id
	r.PublishedMS = fmt.Sprint(m.Time())
	r.PublishedHeight = fmt.Sprint(m.Height())
	return nil
}

const TSAudit = "civic-typescript-v0.6"
const ChallengeMS uint64 = 30 * 60 * 1000

type AuditConfig struct {
	Keys            []string `json:"keys"`
	Implementations []string `json:"implementations"`
}
type Attestation struct {
	PollID         string `json:"poll_id"`
	Dossier        string `json:"dossier_id"`
	Implementation string `json:"implementation"`
	Signer         string `json:"signer"`
	Signature      string `json:"signature"`
}
type Certification struct {
	PollID       string        `json:"poll_id"`
	Dossier      string        `json:"dossier_id"`
	PublishedMS  string        `json:"published_ms"`
	Height       string        `json:"certified_height"`
	Time         string        `json:"certified_ms"`
	Attestations []Attestation `json:"attestations"`
}

func (a *AuditConfig) Validate(forbidden []string) error {
	if a == nil {
		return nil
	}
	if e := codec.Decode(codec.Must(a), "audit_config", new(AuditConfig)); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, s := range forbidden {
		seen[s] = true
	}
	for _, k := range a.Keys {
		if seen[k] {
			return fmt.Errorf("audit key role overlap")
		}
		seen[k] = true
	}
	return nil
}
func Dossier(f Frozen, result Provisional, checkpoints map[string]*Verified) (string, error) {
	items := []any{}
	for lane := range f.Lanes {
		v := checkpoints[fmt.Sprint(lane)]
		if v == nil || !v.Summary.ProcessingSealed {
			return "", fmt.Errorf("missing dossier lane")
		}
		items = append(items, v)
	}
	tally, e := codec.Blob(result.Tally)
	if e != nil {
		return "", e
	}
	return codec.Hash("audit/dossier", []any{f.Context.NetworkID, f.Context.PollID, f.Context.Epoch, f.Context.Checkpoint, items, result.Aggregate, result.Count, result.Yes, result.No, digest(tally)}), nil
}
func AttestationMessage(f Frozen, a Attestation) []byte {
	b, _ := hex.DecodeString(codec.Hash("audit/attest", []any{f.Context.NetworkID, f.Context.PollID, f.Context.Epoch, a.Dossier, a.Implementation}))
	return b
}
func VerifyAttestation(f Frozen, a Attestation) error {
	if f.Audit == nil || a.PollID != f.Context.PollID {
		return fmt.Errorf("audit disabled/poll")
	}
	if e := codec.Decode(codec.Must(a), "audit_attestation", new(Attestation)); e != nil {
		return e
	}
	known, implementation := false, false
	for _, k := range f.Audit.Keys {
		known = known || k == a.Signer
	}
	for _, id := range f.Audit.Implementations {
		implementation = implementation || id == a.Implementation
	}
	if !known || !implementation {
		return fmt.Errorf("audit authority/implementation")
	}
	pk, _ := hex.DecodeString(a.Signer)
	sig, _ := hex.DecodeString(a.Signature)
	if !ed25519.Verify(pk, AttestationMessage(f, a), sig) {
		return fmt.Errorf("audit signature")
	}
	return nil
}
func SortedAttestations(m map[string]Attestation) []Attestation {
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []Attestation{}
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}
func (m *Main) attest(a Attestation) error {
	f, ok := m.Polls[a.PollID]
	if !ok || f.Audit == nil {
		return fmt.Errorf("audit poll")
	}
	r, ok := m.Results[a.PollID]
	if !ok || r.State != "provisional" || r.Dossier != a.Dossier {
		return fmt.Errorf("audit dossier/phase")
	}
	close, _ := codec.Uint(f.Closes)
	if m.Time() >= close+48*60*60*1000 {
		return fmt.Errorf("audit deadline")
	}
	if e := VerifyAttestation(f, a); e != nil {
		return e
	}
	if old, ok := m.Attestations[a.PollID][a.Signer]; ok {
		if string(codec.Must(old)) == string(codec.Must(a)) {
			return nil
		}
		return fmt.Errorf("conflicting auditor statement")
	}
	if m.Attestations[a.PollID] == nil {
		m.Attestations[a.PollID] = map[string]Attestation{}
	}
	m.Attestations[a.PollID][a.Signer] = a
	m.emit("audit.attested", a.PollID)
	return nil
}
func (m *Main) advanceCertification() error {
	ids := []string{}
	for id := range m.Results {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := m.Results[id]
		f := m.Polls[id]
		if f.Audit == nil || r.State != "provisional" {
			continue
		}
		published, _ := codec.Uint(r.PublishedMS)
		if published > ^uint64(0)-ChallengeMS || m.Time() < published+ChallengeMS {
			continue
		}
		close, _ := codec.Uint(f.Closes)
		if m.Time() >= close+48*60*60*1000 {
			continue
		}
		attestations := SortedAttestations(m.Attestations[id])
		implementations := map[string]bool{}
		for _, a := range attestations {
			implementations[a.Implementation] = true
		}
		if len(attestations) < 3 || len(implementations) < 2 {
			continue
		}
		if e := m.Engine.Governance.CertifyPartitionResult(id, r.Yes, r.No, fmt.Sprint(m.Height()), m.Time()); e != nil {
			return e
		}
		r.State = "certified"
		m.Results[id] = r
		m.Certificates[id] = Certification{id, r.Dossier, r.PublishedMS, fmt.Sprint(m.Height()), fmt.Sprint(m.Time()), attestations}
		m.emit("poll.certified", id)
		poll := m.Engine.Governance.Polls[id]
		p := m.Engine.Governance.Proposals[poll.ProposalID]
		at := &m.events[len(m.events)-1]
		for _, kv := range [][2]string{{"yes", r.Yes}, {"no", r.No}, {"eligible_count", poll.EligibleCount}, {"decision", p.State}, {"dossier_id", r.Dossier}, {"proof", "/v1/polls/" + id + "/record"}} {
			at.Attributes = append(at.Attributes, abci.EventAttribute{Key: kv[0], Value: kv[1]})
		}
	}
	return nil
}
