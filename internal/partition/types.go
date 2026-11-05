package partition

import (
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/electionguard"
	"civora/internal/ports"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type Frozen struct {
	Context       electionguard.ScaleContext `json:"context"`
	ProposalID    string                     `json:"proposal_id"`
	GuardianIDs   []string                   `json:"guardian_ids"`
	Ceremony      string                     `json:"ceremony_bundle"`
	Opens         string                     `json:"opens_at_ms"`
	Closes        string                     `json:"closes_at_ms"`
	SourceHeight  string                     `json:"source_height"`
	Lanes         []consensus.Trust          `json:"lanes"`
	ArchiveKeys   []string                   `json:"archive_keys"`
	EligibleCount string                     `json:"eligible_count"`
}

func (f Frozen) ID() string {
	return codec.Hash("scale/open", []any{f.Context.NetworkID, f.Context.PollID, f.Context.Epoch, f.Context.Root, f.Context.Depth, f.Context.Partitions, f.ProposalID, f.GuardianIDs, f.Ceremony, f.Opens, f.Closes, f.SourceHeight, f.Lanes, f.ArchiveKeys, f.EligibleCount})
}
func (f Frozen) Election() (*electionguard.Verifier, *electionguard.Election, error) {
	v, e := electionguard.NewVerifier()
	if e != nil {
		return nil, nil, e
	}
	b, e := electionguard.Decode(f.Ceremony, electionguard.Ceremony)
	if e != nil {
		return nil, nil, e
	}
	ctx := ports.PollContext{NetworkID: f.Context.NetworkID, PollID: f.Context.PollID, ProposalID: f.ProposalID, Epoch: f.Context.Epoch, GuardianIDs: f.GuardianIDs}
	c, e := v.Ceremony(ctx, b.Raw)
	return v, c, e
}
func (f Frozen) Validate(mainChain string) error {
	if e := codec.Decode(codec.Must(f), "partition_frozen", new(Frozen)); e != nil {
		return e
	}
	p, _ := codec.Uint(f.Context.Partitions)
	o, _ := codec.Uint(f.Opens)
	c, _ := codec.Uint(f.Closes)
	h, _ := codec.Uint(f.SourceHeight)
	if p == 0 || p > 256 || p&(p-1) != 0 || len(f.Lanes) != int(p) || o >= c || h == 0 || f.Context.Epoch == "0" || f.Context.Checkpoint != f.ID() {
		return fmt.Errorf("frozen context bounds/identity")
	}
	keys := map[string]bool{}
	for i, l := range f.Lanes {
		if l.ChainID != fmt.Sprintf("%s-v%02x", mainChain, i) {
			return fmt.Errorf("lane chain ID")
		}
		if _, e := l.Set(); e != nil {
			return e
		}
		for _, k := range l.Keys {
			keys[k] = true
		}
	}
	seen := map[string]bool{}
	for _, k := range f.ArchiveKeys {
		b, e := hex.DecodeString(k)
		if e != nil || len(b) != 32 || seen[k] || keys[k] {
			return fmt.Errorf("archive role/key separation")
		}
		seen[k] = true
	}
	_, _, e := f.Election()
	return e
}

type Signature struct {
	Key       string `json:"key"`
	Signature string `json:"signature"`
}
type BatchRef struct {
	ID        string `json:"id"`
	NetworkID string `json:"network_id"`
	PollID    string `json:"poll_id"`
	Lane      string `json:"lane_id"`
	Epoch     string `json:"protocol_epoch"`
	Start     string `json:"start"`
	Count     string `json:"count"`
	Root      string `json:"root"`
	Manifest  string `json:"manifest_sha256"`
	Retention string `json:"retention"`
}
type Batch struct {
	PollID      string      `json:"poll_id"`
	Envelopes   []string    `json:"envelopes"`
	Certificate []Signature `json:"certificate"`
}

func digest(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func Reference(c electionguard.ScaleContext, lane, start string, envelopes []string) (BatchRef, error) {
	items := [][]byte{}
	objects := []any{}
	for _, s := range envelopes {
		b, e := codec.Blob(s)
		if e != nil {
			return BatchRef{}, e
		}
		items = append(items, b)
		objects = append(objects, map[string]any{"sha256": digest(b), "size": fmt.Sprint(len(b))})
	}
	r := BatchRef{NetworkID: c.NetworkID, PollID: c.PollID, Lane: lane, Epoch: c.Epoch, Start: start, Count: fmt.Sprint(len(items)), Root: LogRoot(items), Manifest: digest(codec.Must(objects)), Retention: "persisted-development-v0.5"}
	r.ID = codec.Hash("scale/batch", []any{r.NetworkID, r.PollID, r.Lane, r.Epoch, r.Start, r.Count, r.Root, r.Manifest, r.Retention})
	return r, nil
}
func VerifyCertificate(r BatchRef, cert []Signature, keys []string) error {
	if len(keys) != 5 || len(cert) < 3 || len(cert) > 5 {
		return fmt.Errorf("archive quorum 3/5")
	}
	if e := codec.Decode(codec.Must(r), "partition_batch_ref", new(BatchRef)); e != nil {
		return e
	}
	if r.ID != codec.Hash("scale/batch", []any{r.NetworkID, r.PollID, r.Lane, r.Epoch, r.Start, r.Count, r.Root, r.Manifest, r.Retention}) {
		return fmt.Errorf("batch identity")
	}
	body := codec.Hash("scale/archive", []any{r.NetworkID, r.PollID, r.Lane, r.Epoch, r.Start, r.Count, r.Root, r.Manifest, r.Retention})
	message, _ := hex.DecodeString(body)
	known := map[string]bool{}
	for _, k := range keys {
		b, e := hex.DecodeString(k)
		if e != nil || len(b) != 32 || hex.EncodeToString(b) != k || known[k] {
			return fmt.Errorf("archive key set")
		}
		known[k] = true
	}
	previous := ""
	for _, s := range cert {
		if !known[s.Key] || s.Key <= previous {
			return fmt.Errorf("archive key/order")
		}
		previous = s.Key
		k, _ := hex.DecodeString(s.Key)
		sig, e := hex.DecodeString(s.Signature)
		if e != nil || len(sig) != 64 || !ed25519.Verify(k, message, sig) {
			return fmt.Errorf("archive signature")
		}
	}
	return nil
}
func ArchiveMessage(r BatchRef) []byte {
	b, _ := hex.DecodeString(codec.Hash("scale/archive", []any{r.NetworkID, r.PollID, r.Lane, r.Epoch, r.Start, r.Count, r.Root, r.Manifest, r.Retention}))
	return b
}

type Summary struct {
	NetworkID        string `json:"network_id"`
	PollID           string `json:"poll_id"`
	Lane             string `json:"lane_id"`
	Epoch            string `json:"protocol_epoch"`
	ContextID        string `json:"context_id"`
	Height           string `json:"height"`
	Time             string `json:"time_ms"`
	Admitted         string `json:"admitted_count"`
	AdmissionRoot    string `json:"admission_root"`
	Processed        string `json:"processed_count"`
	StatusRoot       string `json:"status_root"`
	Counted          string `json:"counted_count"`
	SpentRoot        string `json:"spent_root"`
	Aggregate        string `json:"aggregate"`
	Batches          string `json:"batch_count"`
	BatchRoot        string `json:"batch_root"`
	AdmissionSealed  bool   `json:"admission_sealed"`
	ProcessingSealed bool   `json:"processing_sealed"`
}
