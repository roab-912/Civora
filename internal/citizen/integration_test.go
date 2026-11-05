package citizen_test

import (
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/citizen"
	"civora/internal/citizendev"
	"civora/internal/codec"
	"encoding/base64"
	"encoding/json"
	"github.com/cometbft/cometbft/libs/log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) citizendev.Fixture {
	t.Helper()
	raw, e := os.ReadFile("../../spec/fixtures/v0.6/archive.json")
	if e != nil {
		t.Fatal(e)
	}
	var f citizendev.Fixture
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	return f
}
func TestTwoIndependentPathsAnonymousReplayAndHistoricalProofs(t *testing.T) {
	f := fixture(t)
	a, e := citizendev.Serve(f, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := citizendev.Serve(f, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	first := httptest.NewServer(a.API)
	defer first.Close()
	second := httptest.NewServer(b.API)
	defer second.Close()
	m := f.Manifest
	for _, c := range append([]*citizen.Chain{&m.Main}, func() []*citizen.Chain {
		out := []*citizen.Chain{}
		for i := range m.Lanes {
			out = append(out, &m.Lanes[i])
		}
		return out
	}()...) {
		c.Origins = []citizen.Origin{{API: first.URL, RPC: first.URL}, {API: second.URL, RPC: second.URL}}
	}
	raw, _ := codec.Blob(f.Runs[0].Genesis)
	home := filepath.Join(t.TempDir(), "outside")
	if e = citizen.Init(home, codec.Must(m), raw); e != nil {
		t.Fatal(e)
	}
	host, e := citizen.Load(home, "0", "http", 28650, log.NewNopLogger())
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if host != nil {
			host.Close()
		}
	}()
	if e = host.Refresh(); e != nil {
		t.Fatal(e)
	}
	for _, r := range host.Readers {
		v := r.Latest()
		if v == nil {
			t.Fatal("not synchronized")
		}
		p, e := r.StateProof(2, "accounts", "absent-account")
		if e != nil {
			t.Fatal(e)
		}
		if e = citizen.VerifyState(r.Genesis, p); e != nil || p.Path.Exists {
			t.Fatal("absence proof", e)
		}
		bad := p
		bad.Height = "1"
		if citizen.VerifyState(r.Genesis, bad) == nil {
			t.Fatal("H+1 ignored")
		}
	}
	main := host.Readers[m.Main.ChainID]
	original := main.Latest().Root
	api := httptest.NewServer(host.API)
	defer api.Close()
	key := base64.RawURLEncoding.EncodeToString(codec.Must("absent-account"))
	res, e := http.Get(api.URL + "/v1/proofs/state?height=2&table=accounts&key=" + key)
	if e != nil {
		t.Fatal(e)
	}
	var proof citizen.StateProof
	if e = json.NewDecoder(res.Body).Decode(&proof); e != nil {
		t.Fatal(e)
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 || citizen.VerifyState(main.Genesis, proof) != nil {
		t.Fatal("HTTP historical proof")
	}
	tx := main.Latest()
	_ = tx
	v, _ := main.At(1)
	p, e := main.TransactionProof(v.Transactions[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = citizen.VerifyTransaction(main.Genesis.TrustAt(1), p["signed_header"].(string), p["transaction_proof"].(string)); e != nil {
		t.Fatal(e)
	}
	first.Close()
	if e = host.Refresh(); e != nil {
		t.Fatal("fallback failed", e)
	}
	for _, r := range host.Readers {
		desc := m.Main
		for _, candidate := range m.Lanes {
			if candidate.ChainID == r.Genesis.Doc.ChainID {
				desc = candidate
			}
		}
		source := &citizen.HTTPProvider{Genesis: r.Genesis, Descriptor: desc, Client: citizen.Client()}
		if _, e = source.Bundle(1); e != nil {
			t.Fatal("second origin failed", e)
		}
	}
	host.Close()
	host, e = citizen.Load(home, "0", "http", 28650, log.NewNopLogger())
	if e != nil {
		t.Fatal(e)
	}
	defer host.Close()
	if e = host.Refresh(); e != nil || host.Readers[m.Main.ChainID].Latest().Root != original {
		t.Fatal("resumed replay diverged", e)
	}
	host.Close()
	host = nil
	// Rebuild only the derived index, with both remote paths disconnected.
	indexFile := filepath.Join(home, "index", m.Main.ChainID, "index.db")
	if e = os.Remove(indexFile); e != nil {
		t.Fatal(e)
	}
	host, e = citizen.Load(home, "0", "offline", 28650, log.NewNopLogger())
	if e != nil {
		t.Fatal(e)
	}
	if e = host.Refresh(); e != nil || host.Readers[m.Main.ChainID].Latest().Root != original {
		t.Fatal("offline archive rebuild", e)
	}
	if e = host.Verify(); e != nil {
		t.Fatal("offline genesis replay", e)
	}
	matches, _ := filepath.Glob(filepath.Join(home, "**", "priv_validator*"))
	if len(matches) != 0 {
		t.Fatal("consensus key distributed")
	}
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, e error) error {
		if e == nil && strings.Contains(d.Name(), "priv_validator") {
			t.Errorf("observer has signer file: %s", path)
		}
		return e
	})
}
func TestArchiveRangeAndCorruptPrimary(t *testing.T) {
	f := fixture(t)
	host, e := citizendev.Serve(f, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer host.Close()
	good := httptest.NewServer(host.API)
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer bad.Close()
	c := f.Manifest.Main
	c.Origins = []citizen.Origin{{API: bad.URL, RPC: bad.URL}, {API: good.URL, RPC: good.URL}}
	graw, _ := codec.Blob(f.Runs[0].Genesis)
	g, e := chain.ParseGenesis(graw)
	if e != nil {
		t.Fatal(e)
	}
	provider := &citizen.HTTPProvider{Genesis: g, Descriptor: c, Client: citizen.Client()}
	bundle, e := provider.Bundle(1)
	if e != nil {
		t.Fatal("corrupt source did not fall back", e)
	}
	ref, e := host.API.Objects.Put(codec.Must(bundle))
	if e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest("GET", good.URL+"/v1/objects/"+ref.SHA256, nil)
	req.Header.Set("Range", "bytes=0-15")
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	if res.StatusCode != 206 || res.Header.Get("ETag") != "\""+ref.SHA256+"\"" {
		t.Fatal("range/etag")
	}
	store := archive.Store{Directory: t.TempDir()}
	if e = citizen.FetchObject(c, store, ref); e != nil {
		t.Fatal(e)
	}
	if _, e = store.Get(ref); e != nil {
		t.Fatal(e)
	}
}
func TestNoSignerCannotSign(t *testing.T) {
	s := citizen.NoSigner{}
	if s.SignVote("", nil) == nil || s.SignProposal("", nil) == nil {
		t.Fatal("observer obtained signing authority")
	}
}
