// civpwadev is an explicit TEST_ONLY interactive harness. It executes the real
// core and cryptography, and labels its synthetic signed commits. No simulator
// or guardian secret is linked into civnode.
package main

import (
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/citizen"
	"civora/internal/citizendev"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/governance"
	"civora/internal/network"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type provider struct{ blocks atomic.Value }

// Fresh workshops are separate TEST_ONLY networks, never rewinds of a network
// a browser has already verified. An explicit nonce makes tests reproducible.
func developmentNetworkID(session string) (string, error) {
	if session == "" {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return "", err
		}
		session = hex.EncodeToString(nonce[:])
	}
	nonce, err := hex.DecodeString(session)
	if err != nil || len(nonce) != 16 || hex.EncodeToString(nonce) != session {
		return "", fmt.Errorf("session-id must be 16 bytes of lowercase hexadecimal TEST_ONLY session material")
	}
	return codec.Hash("development/pwa/session", []any{"PUBLIC_TEST_ONLY", session}), nil
}

func (p *provider) Height() uint64 { return uint64(len(p.blocks.Load().([]chain.Bundle))) }
func (p *provider) Bundle(h uint64) (chain.Bundle, error) {
	blocks := p.blocks.Load().([]chain.Bundle)
	if h == 0 || h > uint64(len(blocks)) {
		return chain.Bundle{}, fmt.Errorf("missing block")
	}
	return blocks[h-1], nil
}
func run() error {
	address := flag.String("listen", "127.0.0.1:0", "loopback TEST_ONLY API")
	directory := flag.String("home", "", "new development session")
	session := flag.String("session-id", "", "TEST_ONLY session nonce (16 lowercase hexadecimal bytes; random by default)")
	flag.Parse()
	networkID, e := developmentNetworkID(*session)
	if e != nil {
		return e
	}
	if *directory == "" {
		return fmt.Errorf("explicit new --home required")
	}
	if e := os.Mkdir(*directory, 0700); e != nil {
		return e
	}
	raw, e := os.ReadFile("spec/fixtures/v0.4/replay-source.json")
	if e != nil {
		return e
	}
	var source struct {
		Cases []struct {
			Genesis core.Config `json:"genesis"`
		} `json:"cases"`
	}
	if e = json.Unmarshal(raw, &source); e != nil {
		return e
	}
	cfg := source.Cases[0].Genesis
	cfg.NetworkID = networkID
	cfg.ChainID, _ = codec.ChainID(cfg.NetworkID)
	cfg.Activations = cfg.Activations[:0]
	raw, e = os.ReadFile("spec/fixtures/v0.4/actors.TEST_ONLY.json")
	if e != nil {
		return e
	}
	var keys struct {
		Contexts []struct {
			Actors []struct {
				Commitment string `json:"commitment"`
				Signing    struct {
					Seed   string `json:"seed"`
					Public string `json:"publicKey"`
				} `json:"signing"`
				Recovery struct {
					Public string `json:"publicKey"`
				} `json:"recovery"`
			} `json:"actors"`
		} `json:"contexts"`
	}
	if e = json.Unmarshal(raw, &keys); e != nil {
		return e
	}
	actors := keys.Contexts[0].Actors
	cfg.Governance.Guardians = nil
	for _, a := range actors[:5] {
		cfg.Governance.Guardians = append(cfg.Governance.Guardians, governance.Guardian{AccountID: codec.Hash("account", []any{cfg.NetworkID, a.Signing.Public}), SigningPK: a.Signing.Public})
	}
	policy := governance.DefaultPolicy()
	policy.NoticeMS = "3600000"
	policy.VotingMS = "3600000"
	policy.OrdinaryExecutionDelayMS = "3600000"
	cfg.Governance.Policy = policy
	builder, e := citizendev.New(network.Genesis{Kind: "ledger", Data: codec.Must(cfg)})
	if e != nil {
		return e
	}
	if e = builder.Add(1000); e != nil {
		return e
	}
	txs := [][]byte{}
	for _, a := range actors[:5] {
		body := core.Body{V: "1", NetworkID: cfg.NetworkID, Type: "account.register", ActorID: codec.Hash("account", []any{cfg.NetworkID, a.Signing.Public}), Nonce: "0", Payload: codec.Must(core.Payload{SigningPK: a.Signing.Public, RecoveryPK: a.Recovery.Public, Commitment: a.Commitment})}
		tx, e := sign(body, a.Signing.Seed)
		if e != nil {
			return e
		}
		txs = append(txs, codec.Must(tx))
	}
	tm := uint64(time.Now().UnixMilli())
	if e = builder.Add(tm, txs...); e != nil {
		return e
	}
	if e = builder.Add(tm + 1); e != nil {
		return e
	}
	sourceBlocks := &provider{}
	sourceBlocks.blocks.Store(append([]chain.Bundle{}, builder.Run.Blocks...))
	objects := archive.Store{Directory: *directory}
	reader, e := citizen.NewReader(builder.Genesis, sourceBlocks, "", filepath.Join(*directory, "index"))
	if e != nil {
		return e
	}
	defer reader.Close()
	reader.Objects = objects
	reader.ArchiveDirectory = filepath.Join(*directory, "archive")
	if _, e = objects.Put(builder.Genesis.Raw); e != nil {
		return e
	}
	if e = reader.Refresh(); e != nil {
		return e
	}
	ref := archive.Ref{SHA256: archive.Digest(builder.Genesis.Raw), Size: fmt.Sprint(len(builder.Genesis.Raw))}
	manifest := citizen.Manifest{Format: "civic-network-1", Name: "Civora · atelier TEST_ONLY", Mode: "development", NetworkID: cfg.NetworkID, Profile: "inline_dev", Main: citizen.Chain{ChainID: cfg.ChainID, Genesis: ref, Origins: []citizen.Origin{{API: "http://127.0.0.1:8790", RPC: "http://127.0.0.1:8790"}, {API: "http://127.0.0.1:8791", RPC: "http://127.0.0.1:8791"}}, Peers: []string{"TEST_ONLY@127.0.0.1:27650"}}, Lanes: []citizen.Chain{}}
	mu := sync.Mutex{}
	commit := func(raw []byte, at uint64) error {
		mu.Lock()
		defer mu.Unlock()
		if at == 0 {
			at = builder.Machine.Time() + 1
		}
		if len(raw) > 0 {
			if e := builder.Machine.Check(raw); e != nil {
				return e
			}
			if e := builder.Add(at, raw); e != nil {
				return e
			}
		} else {
			if at < builder.Machine.Time() {
				return fmt.Errorf("clock rollback")
			}
			if e := builder.Add(at); e != nil {
				return e
			}
		}
		if e := builder.Add(at + 1); e != nil {
			return e
		}
		sourceBlocks.blocks.Store(append([]chain.Bundle{}, builder.Run.Blocks...))
		data := codec.Must(builder.Run)
		return os.WriteFile(filepath.Join(*directory, "PUBLIC_TEST_ONLY-archive.json"), data, 0600)
	}
	api := &citizen.API{Manifest: manifest, Readers: map[string]*citizen.Reader{cfg.ChainID: reader}, Objects: objects, Submit: func(_ string, raw []byte) error { return commit(raw, 0) }}
	api.Extra = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/__dev/state" && r.Method == "GET" {
			mu.Lock()
			state := builder.Machine.State()
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(state)
			return true
		}
		if r.URL.Path == "/__dev/advance" && r.Method == "POST" {
			raw, e := io.ReadAll(io.LimitReader(r.Body, 4096))
			var v struct {
				At string `json:"at_ms"`
			}
			if e == nil {
				e = json.Unmarshal(raw, &v)
			}
			var at uint64
			if e == nil {
				at, e = codec.Uint(v.At)
			}
			if e == nil {
				e = commit(nil, at)
			}
			if e != nil {
				citizen.WriteJSON(w, 422, map[string]any{"code": "ERR_DEVELOPMENT_COMMAND", "message": "Development command refused", "retryable": false})
			} else {
				citizen.WriteJSON(w, 200, map[string]any{"advanced": true})
			}
			return true
		}
		if r.URL.Path == "/v1/polls/" {
			return false
		}
		return citizen.InlinePollHTTP(w, r, reader)
	}
	listener, e := net.Listen("tcp", *address)
	if e != nil {
		return e
	}
	if host, _, e := net.SplitHostPort(listener.Addr().String()); e != nil || host != "127.0.0.1" {
		return fmt.Errorf("loopback only")
	}
	fmt.Println("PUBLIC_TEST_ONLY_PWA_API http://" + listener.Addr().String())
	server := http.Server{Handler: api, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, MaxHeaderBytes: 8192}
	return server.Serve(listener)
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func sign(body core.Body, seed string) (core.Transaction, error) {
	b, e := hex.DecodeString(seed)
	if e != nil || len(b) != 32 {
		return core.Transaction{}, fmt.Errorf("test key")
	}
	digest, _ := hex.DecodeString(codec.Hash("tx", body))
	return core.Transaction{Body: body, Auth: core.Auth{Scheme: "ed25519", Signature: hex.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(b), digest))}}, nil
}
