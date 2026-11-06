package citizen_test

import (
	"bytes"
	"civora/internal/archive"
	"civora/internal/citizen"
	"civora/internal/citizendev"
	"civora/internal/codec"
	"civora/internal/electionaudit"
	"civora/internal/eligibility"
	"civora/internal/network"
	"civora/internal/partition"
	"civora/internal/partitiondev"
	"civora/internal/state"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCitizenRecordReceiptsArchivesAndChallengePriority(t *testing.T) {
	f := fixture(t)
	h, e := citizendev.Serve(f, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	main := h.Readers[f.Manifest.Main.ChainID]
	m := main.Latest().Machine.(*partition.Main)
	var id string
	for p := range m.Polls {
		id = p
	}
	record, e := h.Election(id)
	if e != nil {
		t.Fatal(e)
	}
	t.Run("full-independent-replay-and-incomplete-refusal", func(t *testing.T) {
		report, e := electionaudit.Verify(record, h.API.Objects, f.Manifest.Main.Genesis.SHA256)
		if e != nil || report.State != "certified" || report.Yes != "3" || report.No != "2" || report.Admitted != "6" || report.Counted != "5" {
			t.Fatal(report, e)
		}
		for _, mutate := range []func(*electionaudit.Record){func(r *electionaudit.Record) { r.Lanes = r.Lanes[:len(r.Lanes)-1] }, func(r *electionaudit.Record) {
			for i := range r.Lanes {
				if len(r.Lanes[i].Batches) > 0 {
					r.Lanes[i].Batches = nil
					break
				}
			}
		}, func(r *electionaudit.Record) { r.Result.Height = "1" }, func(r *electionaudit.Record) { r.Status = r.Result }} {
			var bad electionaudit.Record
			_ = json.Unmarshal(codec.Must(record), &bad)
			mutate(&bad)
			if _, e = electionaudit.Verify(bad, h.API.Objects, f.Manifest.Main.Genesis.SHA256); e == nil {
				t.Fatal("incomplete/tampered record accepted")
			}
		}
		if _, e = electionaudit.Verify(record, h.API.Objects, strings.Repeat("0", 64)); e == nil {
			t.Fatal("self asserted genesis")
		}
	})
	var lane string
	t.Run("audit-quorum-window-and-expiry", func(t *testing.T) {
		view, _ := main.At(10)
		pending := view.Machine.(*partition.Main)
		frozen := pending.Polls[id]
		r := pending.Results[id]
		published, _ := codec.Uint(r.PublishedMS)
		for _, tc := range []struct {
			name string
			n    int
			both bool
			tm   uint64
			want string
		}{{"two-auditors", 2, true, published + partition.ChallengeMS, "provisional"}, {"one-implementation", 3, false, published + partition.ChallengeMS, "provisional"}, {"early", 3, true, published + partition.ChallengeMS - 1, "provisional"}, {"certified", 3, true, published + partition.ChallengeMS, "certified"}, {"expired", 3, true, mustNumber(frozen.Closes) + 48*60*60*1000, "failed_certification"}} {
			t.Run(tc.name, func(t *testing.T) {
				cmds := [][]byte{}
				for i := 0; i < tc.n; i++ {
					impl := partition.GoAudit
					if tc.both && i == 2 {
						impl = partition.TSAudit
					}
					key := network.TestKey(f.Manifest.Main.ChainID, "audit", i)
					a := partition.Attestation{PollID: id, Dossier: r.Dossier, Implementation: impl, Signer: hex.EncodeToString(key.PubKey().Bytes())}
					sig, _ := key.Sign(partition.AttestationMessage(frozen, a))
					a.Signature = hex.EncodeToString(sig)
					cmds = append(cmds, codec.Must(partition.Command{Type: "audit.attest", Value: codec.Must(a)}))
				}
				with, e := pending.Apply(partitiondev.Block("11", fmt.Sprint(pending.Time()+1), cmds...))
				if e != nil {
					t.Fatal(e)
				}
				after, e := with.Apply(partitiondev.Block("12", fmt.Sprint(tc.tm)))
				if e != nil {
					t.Fatal(e)
				}
				if got := after.(*partition.Main).Results[id].State; got != tc.want {
					t.Fatal(got, tc.want)
				}
			})
		}
	})
	var poll *partition.Poll
	for _, desc := range f.Manifest.Lanes {
		p := h.Readers[desc.ChainID].Latest().Machine.(*partition.Machine).Polls[id]
		if len(p.Deposits) > 0 {
			lane = *desc.Lane
			poll = p
			break
		}
	}
	t.Run("durable-receipts-distinguish-copy-and-count", func(t *testing.T) {
		for _, d := range poll.Deposits {
			req := httptest.NewRequest("GET", "/v1/lanes/"+lane+"/receipts/"+d.Digest, nil)
			res := httptest.NewRecorder()
			h.API.ServeHTTP(res, req)
			if res.Code != 200 || !strings.Contains(res.Body.String(), "\"status\":\"replicated\"") || !strings.Contains(res.Body.String(), "\"counting_status\":\""+d.Status+"\"") {
				t.Fatal(res.Code, res.Body.String())
			}
			var receipt struct {
				Anchor  citizen.StateProof `json:"anchor"`
				Witness state.Witness      `json:"status_witness"`
			}
			if e := json.Unmarshal(res.Body.Bytes(), &receipt); e != nil {
				t.Fatal(e)
			}
			var anchor partition.Verified
			if e := json.Unmarshal(codec.Must(receipt.Anchor.Path.Entry.Value), &anchor); e != nil {
				t.Fatal(e)
			}
			if e := state.VerifyWitness(anchor.Summary.StatusRoot, receipt.Witness); e != nil {
				t.Fatal("unverifiable counting status", e)
			}
			if !receipt.Witness.Exists {
				t.Fatal("missing counting status")
			}
		}
	})
	t.Run("membership-path-and-locally-applied-law", func(t *testing.T) {
		res := httptest.NewRecorder()
		h.API.ServeHTTP(res, httptest.NewRequest("GET", "/v1/polls/"+id+"/membership-path?leaf_index=0", nil))
		var p eligibility.Path
		if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &p) != nil || p.Verify() != nil {
			t.Fatal(res.Code, res.Body.String())
		}
		res = httptest.NewRecorder()
		h.API.ServeHTTP(res, httptest.NewRequest("GET", "/v1/laws", nil))
		if res.Code != 200 || strings.Contains(res.Body.String(), "\"items\":[]") {
			t.Fatal(res.Code, res.Body.String())
		}
	})
	t.Run("three-real-archives-persist-before-signature", func(t *testing.T) {
		sigs := []partition.Signature{}
		envelopes := []string{poll.Deposits[0].Envelope}
		ref, _ := partition.Reference(poll.Frozen.Context, lane, "0", envelopes)
		for i := 0; i < 3; i++ {
			store := archive.Store{Directory: t.TempDir()}
			a := &citizen.Host{Home: store.Directory, Manifest: h.Manifest, Readers: h.Readers}
			a.API = &citizen.API{Manifest: h.Manifest, Readers: h.Readers, Objects: store, Extra: a.Extra}
			k := network.TestKey(f.Manifest.Main.ChainID, "archive", i)
			a.Services.Key = ed25519.PrivateKey(k)
			req := citizen.ArchiveRequest{Poll: id, Lane: lane, Start: "0", Envelopes: envelopes}
			res := httptest.NewRecorder()
			a.API.ServeHTTP(res, httptest.NewRequest("POST", "/v1/archive/batches", bytes.NewReader(codec.Must(req))))
			var sig partition.Signature
			if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &sig) != nil {
				t.Fatal(res.Code, res.Body.String())
			}
			sigs = append(sigs, sig)
			raw, _ := codec.Blob(envelopes[0])
			if _, e = store.Get(archive.Ref{SHA256: archive.Digest(raw), Size: fmt.Sprint(len(raw))}); e != nil {
				t.Fatal("signature without bytes", e)
			}
		}
		sortSignatures(sigs)
		if e = partition.VerifyCertificate(ref, sigs, poll.Frozen.ArchiveKeys); e != nil {
			t.Fatal(e)
		}
		if partition.VerifyCertificate(ref, sigs[:2], poll.Frozen.ArchiveKeys) == nil {
			t.Fatal("two archives accepted")
		}
	})
	t.Run("valid-challenge-precedes-due-law-false-challenge-is-atomic", func(t *testing.T) {
		v, _ := main.At(13)
		base := v.Machine.(*partition.Main)
		candidate, e := base.Apply(partitiondev.Block("14", fmt.Sprint(base.Time()+1)))
		if e != nil {
			t.Fatal(e)
		}
		b := candidate.(*partition.Main)
		items := [][]byte{}
		statuses := []state.Entry{}
		bad := -1
		for i, d := range poll.Deposits {
			raw, _ := codec.Blob(d.Envelope)
			items = append(items, raw)
			status := d.Status
			if status == "invalid" {
				bad = i
				status = "counted"
			}
			statuses = append(statuses, state.Entry{Table: "status", Key: fmt.Sprint(i), Value: map[string]any{"digest": d.Digest, "status": status}})
		}
		if bad < 0 { // the first populated lane can contain only valid ballots
			for _, desc := range f.Manifest.Lanes {
				p := h.Readers[desc.ChainID].Latest().Machine.(*partition.Machine).Polls[id]
				for i, d := range p.Deposits {
					if d.Status == "invalid" {
						lane = *desc.Lane
						poll = p
						bad = i
					}
				}
			}
			items = nil
			statuses = nil
			for i, d := range poll.Deposits {
				raw, _ := codec.Blob(d.Envelope)
				items = append(items, raw)
				status := d.Status
				if i == bad {
					status = "counted"
				}
				statuses = append(statuses, state.Entry{Table: "status", Key: fmt.Sprint(i), Value: map[string]any{"digest": d.Digest, "status": status}})
			}
		}
		if bad < 0 {
			t.Fatal("invalid fixture missing")
		}
		cp := *b.Checkpoints[id][lane]
		cp.Summary.StatusRoot = state.Root(statuses)
		b.Checkpoints[id][lane] = &cp
		w, _ := state.WitnessFor(statuses, "status", fmt.Sprint(bad))
		path, _ := partition.Inclusion(items, bad)
		c := partition.Challenge{PollID: id, Kind: "counted_invalid", Items: []partition.Evidence{{Lane: lane, Index: fmt.Sprint(bad), Envelope: poll.Deposits[bad].Envelope, Admission: path, Status: w}}}
		if e = partition.VerifyChallenge(poll.Frozen, b.Checkpoints[id], c); e != nil {
			t.Fatal(e)
		}
		raw := codec.Must(partition.Command{Type: "audit.challenge", Value: codec.Must(c)})
		due := *b.Engine.Governance.Proposals[poll.Frozen.ProposalID].EffectiveAtMS
		original := b.Root()
		falseProof := c
		falseProof.Kind = "double_counted"
		if _, e = b.Apply(partitiondev.Block("15", due, codec.Must(partition.Command{Type: "audit.challenge", Value: codec.Must(falseProof)}))); e == nil || original != b.Root() {
			t.Fatal("false challenge affected state")
		}
		next, e := b.Apply(partitiondev.Block("15", due, raw))
		if e != nil {
			t.Fatal(e)
		}
		n := next.(*partition.Main)
		if n.Results[id].State != "contested" || n.Engine.Governance.Proposals[poll.Frozen.ProposalID].State != "contested" {
			t.Fatal("law applied despite preceding challenge")
		}
	})
}
func sortSignatures(s []partition.Signature) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j].Key < s[i].Key {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}
func mustNumber(s string) uint64 { n, _ := codec.Uint(s); return n }
func TestResumableObjectUsesRangeAndChecksFullDigest(t *testing.T) {
	data := bytes.Repeat([]byte("reproducible encrypted bytes"), 1000)
	ref := archive.Ref{SHA256: archive.Digest(data), Size: fmt.Sprint(len(data))}
	store := archive.Store{Directory: t.TempDir()}
	path, _ := store.Path(ref.SHA256)
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	_ = os.WriteFile(path+".partial", data[:1234], 0600)
	ranges := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=1234-" {
			ranges++
		}
		w.Header().Set("ETag", "\""+ref.SHA256+"\"")
		http.ServeContent(w, r, "object", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()
	c := citizen.Chain{Origins: []citizen.Origin{{API: server.URL}, {API: server.URL}}}
	if e := citizen.FetchObject(c, store, ref); e != nil {
		t.Fatal(e)
	}
	if ranges != 1 {
		t.Fatal("download did not resume")
	}
	if _, e := store.Get(ref); e != nil {
		t.Fatal(e)
	}
	// A corrupt prefix of the advertised full length must be discarded rather
	// than repeatedly issuing an unsatisfiable range to every mirror.
	other := archive.Store{Directory: t.TempDir()}
	badPath, _ := other.Path(ref.SHA256)
	_ = os.MkdirAll(filepath.Dir(badPath), 0700)
	_ = os.WriteFile(badPath+".partial", bytes.Repeat([]byte("x"), len(data)), 0600)
	if e := citizen.FetchObject(c, other, ref); e != nil {
		t.Fatal("corrupt complete prefix blocked recovery", e)
	}
}
