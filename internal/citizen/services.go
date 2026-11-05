package citizen

import (
	"bytes"
	"civora/internal/archive"
	"civora/internal/codec"
	"civora/internal/electionguard"
	"civora/internal/partition"
	"civora/internal/replica"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Optional archive authority. Public observers and relays have no such key.
type Services struct {
	Key      ed25519.PrivateKey
	Archives []string
	Workers  bool
	mu       sync.Mutex
	next     uint64
}
type ArchiveRequest struct {
	Poll      string   `json:"poll_id"`
	Lane      string   `json:"lane_id"`
	Start     string   `json:"start"`
	Envelopes []string `json:"envelopes"`
}
type Job struct {
	Chain string `json:"chain_id"`
	Body  string `json:"body"`
	Kind  string `json:"kind"`
}

func (h *Host) main() (*Reader, *partition.Main, error) {
	r := h.Readers[h.Manifest.Main.ChainID]
	if r == nil || r.Latest() == nil {
		return nil, nil, fmt.Errorf("main unavailable")
	}
	m, ok := application(r.Latest().Machine).(*partition.Main)
	if !ok {
		return nil, nil, fmt.Errorf("partition coordinator required")
	}
	return r, m, nil
}
func (h *Host) lane(id string) (*Reader, *partition.Machine, error) {
	n, e := codec.Uint(id)
	if e != nil || n >= uint64(len(h.Manifest.Lanes)) {
		return nil, nil, fmt.Errorf("lane")
	}
	r := h.Readers[h.Manifest.Lanes[n].ChainID]
	if r == nil || r.Latest() == nil {
		return nil, nil, fmt.Errorf("lane unavailable")
	}
	m, ok := application(r.Latest().Machine).(*partition.Machine)
	if !ok {
		return nil, nil, fmt.Errorf("partition required")
	}
	return r, m, nil
}
func (h *Host) persistJob(id string, j Job) error {
	if _, e := h.API.Objects.Path(id); e != nil {
		return e
	}
	dir := filepath.Join(h.Home, "relay")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	p := filepath.Join(dir, id+".json")
	if old, e := os.ReadFile(p); e == nil {
		if string(old) != string(codec.Must(j)) {
			return fmt.Errorf("job collision")
		}
		return nil
	}
	files, e := filepath.Glob(filepath.Join(dir, "*.json"))
	if e != nil || len(files) >= 1024 {
		return fmt.Errorf("relay queue capacity")
	}
	return replica.Persist(dir, p, codec.Must(j), nil)
}
func (h *Host) archive(a ArchiveRequest) (partition.Signature, error) {
	_, main, e := h.main()
	if e != nil {
		return partition.Signature{}, e
	}
	f, ok := main.Polls[a.Poll]
	if !ok || len(h.Services.Key) != ed25519.PrivateKeySize {
		return partition.Signature{}, fmt.Errorf("archive unavailable")
	}
	pk := hex.EncodeToString(h.Services.Key.Public().(ed25519.PublicKey))
	authorized := false
	for _, k := range f.ArchiveKeys {
		authorized = authorized || k == pk
	}
	if !authorized {
		return partition.Signature{}, fmt.Errorf("archive authority")
	}
	r, e := partition.Reference(f.Context, a.Lane, a.Start, a.Envelopes)
	if e != nil {
		return partition.Signature{}, e
	}
	refs := []archive.Ref{}
	for _, encoded := range a.Envelopes {
		raw, e := codec.Blob(encoded)
		if e != nil {
			return partition.Signature{}, e
		}
		if e = partition.VerifyAdmission(f, a.Lane, raw); e != nil {
			return partition.Signature{}, e
		}
		ref, e := h.API.Objects.Put(raw)
		if e != nil {
			return partition.Signature{}, e
		}
		refs = append(refs, ref)
	}
	ref, e := h.API.Objects.Put(codec.Must(refs))
	if e != nil || ref.SHA256 != r.Manifest {
		return partition.Signature{}, fmt.Errorf("archive manifest persistence")
	}
	if _, e = h.API.Objects.Get(ref); e != nil {
		return partition.Signature{}, e
	}
	msg, _ := hex.DecodeString(codec.Hash("scale/archive", []any{r.NetworkID, r.PollID, r.Lane, r.Epoch, r.Start, r.Count, r.Root, r.Manifest, r.Retention}))
	return partition.Signature{Key: pk, Signature: hex.EncodeToString(ed25519.Sign(h.Services.Key, msg))}, nil
}
func (h *Host) certify(a ArchiveRequest, f partition.Frozen) ([]partition.Signature, error) {
	ref, e := partition.Reference(f.Context, a.Lane, a.Start, a.Envelopes)
	if e != nil {
		return nil, e
	}
	cert := []partition.Signature{}
	seen := map[string]bool{}
	for _, url := range h.Services.Archives {
		req, e := http.NewRequest("POST", strings.TrimRight(url, "/")+"/v1/archive/batches", bytes.NewReader(codec.Must(a)))
		if e != nil {
			continue
		}
		res, e := Client().Do(req)
		if e != nil {
			continue
		}
		raw, e := io.ReadAll(io.LimitReader(res.Body, 4097))
		res.Body.Close()
		var sig partition.Signature
		if res.StatusCode != 200 || e != nil || len(raw) > 4096 || codec.Decode(raw, "archive_signature", &sig) != nil || seen[sig.Key] {
			continue
		}
		trial := append(append([]partition.Signature{}, cert...), sig)
		authorized := false
		for _, key := range f.ArchiveKeys {
			authorized = authorized || key == sig.Key
		}
		pk, _ := hex.DecodeString(sig.Key)
		signature, _ := hex.DecodeString(sig.Signature)
		msg, _ := hex.DecodeString(codec.Hash("scale/archive", []any{ref.NetworkID, ref.PollID, ref.Lane, ref.Epoch, ref.Start, ref.Count, ref.Root, ref.Manifest, ref.Retention}))
		if !authorized || !ed25519.Verify(pk, msg, signature) {
			continue
		}
		sort.Slice(trial, func(i, j int) bool { return trial[i].Key < trial[j].Key })
		if len(trial) >= 3 && partition.VerifyCertificate(ref, trial, f.ArchiveKeys) == nil {
			return trial, nil
		}
		cert = trial
		seen[sig.Key] = true
	}
	return nil, fmt.Errorf("archive quorum unavailable")
}
func (h *Host) relayBallot(lane string, raw []byte) error {
	var env electionguard.ScaleEnvelope
	if e := codec.Decode(raw, "scale_envelope", &env); e != nil {
		return e
	}
	_, main, e := h.main()
	if e != nil {
		return e
	}
	f, ok := main.Polls[env.PollID]
	if !ok {
		return fmt.Errorf("poll unknown")
	}
	if e = partition.VerifyAdmission(f, lane, raw); e != nil {
		return e
	}
	_, lm, e := h.lane(lane)
	if e != nil {
		return e
	}
	p := lm.Polls[env.PollID]
	if p == nil {
		return fmt.Errorf("lane not opened")
	}
	exists := false
	for _, d := range p.Deposits {
		exists = exists || d.Digest == archive.Digest(raw)
	}
	if !exists && (lm.Time() < mustUint(f.Opens) || lm.Time() >= mustUint(f.Closes)) {
		return fmt.Errorf("admission phase")
	}
	return h.persistJob(archive.Digest(raw), Job{h.Manifest.Lanes[mustUint(lane)].ChainID, base64.RawURLEncoding.EncodeToString(raw), "ballot"})
}
func mustUint(s string) uint64 { n, _ := codec.Uint(s); return n }
func (h *Host) receipt(lane, id string) (map[string]any, error) {
	r, m, e := h.lane(lane)
	if e != nil {
		return nil, e
	}
	for poll, p := range m.Polls {
		for i, d := range p.Deposits {
			if d.Digest != id {
				continue
			}
			var nativeID string
			v, e := r.At(mustUint(d.Height))
			if e != nil {
				return nil, e
			}
			for _, tx := range v.Transactions {
				raw, _ := codec.Blob(tx.Bytes)
				var cmd struct {
					Type  string          `json:"type"`
					Value partition.Batch `json:"value"`
				}
				if json.Unmarshal(raw, &cmd) == nil && cmd.Type == "partition.admit" {
					for _, enc := range cmd.Value.Envelopes {
						if enc == d.Envelope {
							nativeID = tx.ID
						}
					}
				}
			}
			proof, e := r.TransactionProof(nativeID)
			if e != nil {
				return nil, e
			}
			result := map[string]any{"envelope_sha256": id, "poll_id": poll, "lane_id": lane, "index": fmt.Sprint(i), "height": d.Height, "status": "replicated", "counting_status": d.Status, "proof": proof, "envelope": d.Envelope, "context": p.Frozen, "validation_height": fmt.Sprint(m.Height())}
			for _, batch := range p.Batches {
				if batch.Reference.ID != d.Batch {
					continue
				}
				start, count := mustUint(batch.Reference.Start), mustUint(batch.Reference.Count)
				items := [][]byte{}
				for _, deposit := range p.Deposits[start : start+count] {
					raw, _ := codec.Blob(deposit.Envelope)
					items = append(items, raw)
				}
				path, e := partition.Inclusion(items, i-int(start))
				if e != nil {
					return nil, e
				}
				result["reference"] = batch.Reference
				result["archive_certificate"] = batch.Certificate
				result["batch_index"] = fmt.Sprint(uint64(i) - start)
				result["inclusion"] = path
			}
			if mainReader, main, e := h.main(); e == nil && mainReader.Latest() != nil && mainReader.Latest().Height != "1" {
				checkpoint := main.Checkpoints[poll][lane]
				if checkpoint != nil {
					height := mustUint(mainReader.Latest().Height) - 1
					anchor, e := mainReader.StateProof(height, "transport_checkpoints", []any{poll, lane})
					if e == nil && anchor.Path.Exists {
						result["accepted_checkpoint"] = anchor.Path.Entry.Value
						result["anchor"] = anchor
					}
				}
			}
			return result, nil
		}
	}
	_, e = os.Stat(filepath.Join(h.Home, "relay", id+".json"))
	status := "unknown"
	if e == nil {
		status = "seen"
	}
	return map[string]any{"envelope_sha256": id, "lane_id": lane, "status": status, "counting_status": "pending"}, nil
}

// Tick can be run by any observer. Commands carry evidence, never a worker's
// verdict; the replicated machines recompute every transition.
func (h *Host) Tick() error {
	if !h.Services.Workers {
		return nil
	}
	h.Services.mu.Lock()
	defer h.Services.mu.Unlock()
	if e := h.Refresh(); e != nil {
		return e
	}
	files, e := filepath.Glob(filepath.Join(h.Home, "relay", "*.json"))
	if e != nil {
		return e
	}
	for i := 0; i < 1 && len(files) > 0; i++ {
		p := files[h.Services.next%uint64(len(files))]
		h.Services.next++
		raw, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		var j Job
		if e = codec.Decode(raw, "citizen_relay_job", &j); e != nil {
			return e
		}
		body, _ := codec.Blob(j.Body)
		reader := h.Readers[j.Chain]
		if reader == nil || reader.Latest() == nil {
			continue
		}
		if j.Kind == "transaction" {
			if _, ok := reader.FindTransaction(TransactionID(body)); ok {
				_ = os.Remove(p)
				continue
			}
			if reader.Latest().Machine.Check(body) == nil {
				_ = h.API.Submit(j.Chain, body)
			}
			continue
		}
		m, ok := application(reader.Latest().Machine).(*partition.Machine)
		if !ok {
			continue
		}
		var env electionguard.ScaleEnvelope
		if e = codec.Decode(body, "scale_envelope", &env); e != nil {
			return e
		}
		poll := m.Polls[env.PollID]
		if poll == nil {
			continue
		}
		found := false
		for _, d := range poll.Deposits {
			found = found || d.Digest == archive.Digest(body)
		}
		if found {
			_ = os.Remove(p)
			continue
		}
		a := ArchiveRequest{env.PollID, m.Config.Lane, fmt.Sprint(len(poll.Deposits)), []string{j.Body}}
		cert, e := h.certify(a, poll.Frozen)
		if e != nil {
			continue
		}
		cmd := codec.Must(partition.Command{Type: "partition.admit", Value: codec.Must(partition.Batch{PollID: env.PollID, Envelopes: a.Envelopes, Certificate: cert})})
		if m.Check(cmd) == nil {
			_ = h.API.Submit(j.Chain, cmd)
		}
	}
	mainReader, main, e := h.main()
	if e != nil {
		return nil
	}
	ids := []string{}
	for id := range main.Polls {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, desc := range h.Manifest.Lanes {
		r := h.Readers[desc.ChainID]
		if r == nil || r.Latest() == nil {
			continue
		}
		m, ok := application(r.Latest().Machine).(*partition.Machine)
		if !ok {
			continue
		}
		for _, id := range ids {
			p := m.Polls[id]
			if p == nil {
				proof, e := mainReader.Membership(mustUint(main.Polls[id].SourceHeight), "transport_polls", id)
				if e == nil {
					cmd := codec.Must(partition.Command{Type: "partition.open", Value: codec.Must(proof)})
					_ = h.API.Submit(desc.ChainID, cmd)
				}
				continue
			}
			remaining := uint64(len(p.Deposits)) - mustUint(p.Processed)
			if remaining > 4 {
				remaining = 4
			}
			if remaining > 0 {
				cmd := codec.Must(partition.Command{Type: "partition.process", Value: codec.Must(partition.Process{PollID: id, Count: fmt.Sprint(remaining)})})
				_ = h.API.Submit(desc.ChainID, cmd)
			}
			previous := main.Checkpoints[id][m.Config.Lane]
			height := m.Height()
			if height < 2 {
				continue
			}
			v, e := r.At(height - 1)
			if e != nil {
				continue
			}
			at, ok := application(v.Machine).(*partition.Machine)
			if !ok || at.Polls[id] == nil {
				continue
			}
			oldBatches := uint64(0)
			if previous != nil {
				oldBatches = mustUint(previous.Summary.Batches)
			}
			for uint64(len(at.Polls[id].Batches)) > oldBatches+64 && height > 2 {
				height--
				v, e = r.At(height - 1)
				if e != nil {
					break
				}
				at, ok = application(v.Machine).(*partition.Machine)
				if !ok || at.Polls[id] == nil {
					break
				}
			}
			if !ok || at.Polls[id] == nil {
				continue
			}
			s := at.Summary(id)
			if previous != nil && (mustUint(s.Height) <= mustUint(previous.Summary.Height) || (s.ProcessingSealed && previous.Summary.ProcessingSealed)) {
				continue
			}
			if previous != nil && !s.ProcessingSealed && mustUint(s.Batches)-oldBatches < 64 && mustUint(s.Time) < mustUint(previous.Summary.Time)+60000 {
				continue
			}
			proof, e := r.Membership(height-1, "lane_checkpoint", id)
			if e != nil {
				continue
			}
			cp, e := at.Checkpoint(id, proof, previous)
			if e != nil || len(cp.Extension) > 64 {
				continue
			}
			cmd := codec.Must(partition.Command{Type: "lane.checkpoint", Value: codec.Must(cp)})
			if main.Check(cmd) == nil {
				_ = h.API.Submit(h.Manifest.Main.ChainID, cmd)
			}
		}
	}
	return nil
}

func (h *Host) servicesHTTP(w http.ResponseWriter, r *http.Request) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if r.URL.Path == "/v1/archive/batches" && r.Method == "POST" {
		raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024))
		var a ArchiveRequest
		if e != nil || codec.Decode(raw, "citizen_archive_request", &a) != nil {
			failure(w, 400, "ERR_ENCODING", false)
			return true
		}
		sig, e := h.archive(a)
		if e != nil {
			failure(w, 503, "ERR_ARCHIVE_UNAVAILABLE", true)
		} else {
			WriteJSON(w, 200, sig)
		}
		return true
	}
	if len(parts) >= 4 && parts[0] == "v1" && parts[1] == "lanes" {
		lane := parts[2]
		if _, _, e := h.lane(lane); e != nil {
			failure(w, 503, "ERR_CHAIN_UNAVAILABLE", true)
			return true
		}
		if len(parts) == 4 && parts[3] == "ballots" && r.Method == "POST" {
			raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024))
			h.Services.mu.Lock()
			if e == nil {
				e = h.relayBallot(lane, raw)
			}
			h.Services.mu.Unlock()
			if e != nil {
				if strings.Contains(e.Error(), "admission phase") {
					failure(w, 409, "ERR_PHASE", false)
				} else {
					failure(w, 422, "ERR_PROOF", false)
				}
			} else {
				WriteJSON(w, 202, map[string]any{"envelope_sha256": archive.Digest(raw), "status": "seen", "counting_status": "pending"})
			}
			return true
		}
		if len(parts) == 5 && parts[3] == "receipts" && r.Method == "GET" {
			if _, e := h.API.Objects.Path(parts[4]); e != nil {
				failure(w, 400, "ERR_ENCODING", false)
				return true
			}
			result, e := h.receipt(lane, parts[4])
			if e != nil {
				failure(w, 503, "ERR_PROOF_PENDING", true)
			} else {
				WriteJSON(w, 200, result)
			}
			return true
		}
	}
	return false
}
