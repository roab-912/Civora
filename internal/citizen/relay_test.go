package citizen_test

import (
	"bytes"
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/citizen"
	"civora/internal/citizendev"
	"civora/internal/codec"
	"civora/internal/network"
	"civora/internal/partition"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTwoRelaysRestartArchiveQuorumAndOriginalReceipt(t *testing.T) {
	f := fixture(t)
	var laneIndex int
	var envelope, id string
	for i := 1; i < len(f.Runs); i++ {
		block, _ := chain.Decode(f.Runs[i].Blocks[2])
		for _, raw := range block.Txs {
			var c partition.Command
			if codec.Decode(raw, "partition_tx", &c) == nil && c.Type == "partition.admit" {
				var batch partition.Batch
				_ = codec.Decode(c.Value, "partition_batch", &batch)
				laneIndex = i
				envelope = batch.Envelopes[0]
				id = batch.PollID
				break
			}
		}
		if envelope != "" {
			break
		}
	}
	if envelope == "" {
		t.Fatal("missing ballot")
	}
	raw, _ := codec.Blob(envelope)
	lane := fmt.Sprint(laneIndex - 1)
	f.Runs[0].Blocks = f.Runs[0].Blocks[:6]
	for i := 1; i < len(f.Runs); i++ {
		f.Runs[i].Blocks = f.Runs[i].Blocks[:2]
	}
	firstHome, secondHome := t.TempDir(), t.TempDir()
	a, e := citizendev.Serve(f, firstHome)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { a.Close() }()
	b, e := citizendev.Serve(f, secondHome)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	archives := []string{}
	for i := 0; i < 3; i++ {
		h := &citizen.Host{Home: t.TempDir(), Manifest: a.Manifest, Readers: a.Readers}
		h.API = &citizen.API{Manifest: a.Manifest, Readers: a.Readers, Objects: archive.Store{Directory: h.Home}, Extra: h.Extra}
		h.Services.Key = ed25519.PrivateKey(network.TestKey(f.Manifest.Main.ChainID, "archive", i))
		s := httptest.NewServer(h.API)
		defer s.Close()
		archives = append(archives, s.URL)
	}
	post := func(h *citizen.Host) {
		res := httptest.NewRecorder()
		h.API.ServeHTTP(res, httptest.NewRequest("POST", "/v1/lanes/"+lane+"/ballots", bytes.NewReader(raw)))
		if res.Code != 202 {
			t.Fatal(res.Code, res.Body.String())
		}
	}
	post(a)
	post(a)
	post(b)
	files, _ := filepath.Glob(filepath.Join(firstHome, "relay", "*.json"))
	if len(files) != 1 {
		t.Fatal("non idempotent durable queue")
	}
	submitted := [][]byte{}
	submit := func(chain string, body []byte) error {
		var c partition.Command
		_ = json.Unmarshal(body, &c)
		if c.Type == "partition.admit" {
			submitted = append(submitted, append([]byte{}, body...))
		}
		return nil
	}
	a.Services.Workers = true
	a.Services.Archives = archives[:2]
	a.API.Submit = submit
	if e = a.Tick(); e != nil {
		t.Fatal(e)
	}
	if len(submitted) != 0 {
		t.Fatal("relay bypassed archive quorum")
	}
	a.Close()
	a, e = citizendev.Serve(f, firstHome)
	if e != nil {
		t.Fatal(e)
	}
	a.Services.Workers = true
	a.Services.Archives = archives
	a.API.Submit = submit
	b.Services.Workers = true
	b.Services.Archives = archives
	b.API.Submit = submit
	if e = a.Tick(); e != nil {
		t.Fatal(e)
	}
	if e = b.Tick(); e != nil {
		t.Fatal(e)
	}
	if len(submitted) != 2 || !bytes.Equal(submitted[0], submitted[1]) {
		t.Fatal("independent relays changed the canonical batch", len(submitted))
	}
	genRaw, _ := codec.Blob(f.Runs[laneIndex].Genesis)
	g, e := chain.ParseGenesis(genRaw)
	if e != nil {
		t.Fatal(e)
	}
	builder, e := citizendev.New(g.Application)
	if e != nil {
		t.Fatal(e)
	}
	for _, bundle := range f.Runs[laneIndex].Blocks {
		block, _ := chain.Decode(bundle)
		cmds := [][]byte{}
		for _, tx := range block.Txs {
			cmds = append(cmds, tx)
		}
		if e = builder.Add(uint64(block.Time.UnixMilli()), cmds...); e != nil {
			t.Fatal(e)
		}
	}
	if e = builder.Add(builder.Machine.Time()+1, submitted...); e != nil {
		t.Fatal(e)
	}
	check := func(h *citizen.Host, want string) {
		h.Readers[f.Runs[laneIndex].ChainID].Source = citizendev.Provider{Blocks: builder.Run.Blocks}
		if e = h.Refresh(); e != nil {
			t.Fatal(e)
		}
		res := httptest.NewRecorder()
		h.API.ServeHTTP(res, httptest.NewRequest("GET", "/v1/lanes/"+lane+"/receipts/"+archive.Digest(raw), nil))
		if res.Code != 200 || !strings.Contains(res.Body.String(), "\"status\":\"replicated\"") || !strings.Contains(res.Body.String(), "\"counting_status\":\""+want+"\"") {
			t.Fatal(res.Code, res.Body.String())
		}
	}
	check(a, "pending")
	check(b, "pending")
	if e = builder.Add(builder.Machine.Time()+1, codec.Must(partition.Command{Type: "partition.process", Value: codec.Must(partition.Process{PollID: id, Count: "1"})})); e != nil {
		t.Fatal(e)
	}
	check(a, "counted")
	check(b, "counted")
	p := builder.Machine.(*partition.Machine).Polls[id]
	if len(p.Deposits) != 1 || p.Deposits[0].Height != "3" {
		t.Fatal("retries changed original inclusion")
	}
}
