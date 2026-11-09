// Package participation joins the local TEST_ONLY workshop as a verifying
// observer. Its identity signs presence reports, never consensus or civic votes.
package participation

import (
	"bytes"
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/citizen"
	"civora/internal/codec"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Format         string `json:"format"`
	NetworkID      string `json:"network_id"`
	Genesis        string `json:"genesis"`
	GenesisSHA256  string `json:"genesis_sha256"`
	API            string `json:"api_url"`
	Presence       string `json:"presence_url"`
	CAPEM          string `json:"ca_pem"`
	PublicTestOnly bool   `json:"public_test_only"`
}

func DecodeConfig(raw []byte) (Config, *chain.Genesis, error) {
	var c Config
	if len(raw) > 2*1024*1024 {
		return c, nil, fmt.Errorf("bootstrap capacity")
	}
	if _, err := codec.Parse(raw); err != nil {
		return c, nil, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, nil, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 8 {
		return c, nil, fmt.Errorf("bootstrap requires every field, including ca_pem")
	}
	for _, name := range []string{"format", "network_id", "genesis", "genesis_sha256", "api_url", "presence_url", "ca_pem", "public_test_only"} {
		if _, ok := fields[name]; !ok {
			return c, nil, fmt.Errorf("missing bootstrap field %s", name)
		}
	}
	if c.Format != "civora-observer-1" || !c.PublicTestOnly {
		return c, nil, fmt.Errorf("only the explicit experimental TEST_ONLY profile is supported")
	}
	api, err := url.Parse(c.API)
	if err != nil || !citizen.ValidURL(c.API) || api.RawPath != "" || strings.HasSuffix(c.API, "/") {
		return c, nil, fmt.Errorf("invalid bootstrap API")
	}
	ip := net.ParseIP(api.Hostname())
	if api.Hostname() != "localhost" && (ip == nil || !(ip.IsLoopback() || ip.IsPrivate())) {
		return c, nil, fmt.Errorf("only a loopback or private LAN bootstrap is supported")
	}
	presence, err := url.Parse(c.Presence)
	if err != nil || !citizen.ValidURL(c.Presence) || presence.Scheme != api.Scheme || presence.Host != api.Host || presence.Path != "/network/presence" {
		return c, nil, fmt.Errorf("presence must belong to the bootstrap origin")
	}
	// HTTP is an explicit LAN/loopback development transport. HTTPS always
	// verifies the server certificate, including when a local CA is supplied.
	genesisRaw, err := codec.Blob(c.Genesis)
	if err != nil || archive.Digest(genesisRaw) != c.GenesisSHA256 {
		return c, nil, fmt.Errorf("bootstrap genesis fingerprint mismatch")
	}
	g, err := chain.ParseGenesis(genesisRaw)
	if err != nil {
		return c, nil, err
	}
	chainID, err := codec.ChainID(c.NetworkID)
	var app struct {
		Network string `json:"network_id"`
		Profile string `json:"voting_profile"`
	}
	if err != nil || json.Unmarshal(g.Application.Data, &app) != nil || g.Doc.ChainID != chainID || app.Network != c.NetworkID || app.Profile != "inline_dev" {
		return c, nil, fmt.Errorf("bootstrap network/profile mismatch")
	}
	return c, g, nil
}

func (c Config) Client() (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if c.CAPEM != "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(c.CAPEM)) {
			return nil, fmt.Errorf("invalid local CA certificate")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("bootstrap redirect refused") }}, nil
}

type Observer struct {
	Config Config
	Reader *citizen.Reader
	Remote *citizen.HTTPProvider
	Client *http.Client
	key    ed25519.PrivateKey
}

// exclusiveWrite never silently replaces an identity or an existing network pin.
func exclusiveWrite(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func Open(raw []byte, home string) (*Observer, error) {
	c, g, err := DecodeConfig(raw)
	if err != nil {
		return nil, err
	}
	client, err := c.Client()
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(home, 0700); err != nil {
		return nil, err
	}
	pin := filepath.Join(home, "bootstrap.json")
	if old, readErr := os.ReadFile(pin); readErr == nil {
		previous, _, decodeErr := DecodeConfig(old)
		if decodeErr != nil || previous != c {
			return nil, fmt.Errorf("existing observer belongs to another bootstrap; keep its data and choose a separate --home")
		}
	} else if !os.IsNotExist(readErr) {
		return nil, readErr
	} else if err = exclusiveWrite(pin, codec.Must(c)); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(home, "presence-key.PRIVATE.json")
	seedRaw, err := os.ReadFile(keyPath)
	if os.IsNotExist(err) {
		var seed [ed25519.SeedSize]byte
		if _, err = rand.Read(seed[:]); err != nil {
			return nil, err
		}
		seedRaw = codec.Must(map[string]string{"purpose": "local-observer-presence-only", "seed": hex.EncodeToString(seed[:])})
		if err = exclusiveWrite(keyPath, seedRaw); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	var stored struct {
		Purpose string `json:"purpose"`
		Seed    string `json:"seed"`
	}
	if json.Unmarshal(seedRaw, &stored) != nil || stored.Purpose != "local-observer-presence-only" {
		return nil, fmt.Errorf("invalid local presence identity; no automatic reset")
	}
	seed, err := hex.DecodeString(stored.Seed)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("invalid local presence key")
	}
	descriptor := citizen.Chain{ChainID: g.Doc.ChainID, Origins: []citizen.Origin{{API: c.API}}}
	remote := &citizen.HTTPProvider{Genesis: g, Descriptor: descriptor, Client: client}
	reader, err := citizen.NewReader(g, remote, "", filepath.Join(home, "archive"))
	if err != nil {
		return nil, err
	}
	return &Observer{Config: c, Reader: reader, Remote: remote, Client: client, key: ed25519.NewKeyFromSeed(seed)}, nil
}

func (o *Observer) Close() error   { return o.Reader.Close() }
func (o *Observer) NodeID() string { return hex.EncodeToString(o.key.Public().(ed25519.PublicKey)) }

// Sync replays saved blocks as well as new blocks through the existing verifier.
// A source that goes backwards or disappears never lowers the saved archive.
func (o *Observer) Sync() (*citizen.View, error) {
	remote := o.Remote.Height()
	if remote < 2 {
		return nil, fmt.Errorf("source unavailable or missing authenticating header")
	}
	if err := o.Reader.Refresh(); err != nil {
		return nil, err
	}
	v := o.Reader.Latest()
	if v == nil {
		return nil, fmt.Errorf("archive not ready")
	}
	height, err := codec.Uint(v.Height)
	if height > remote {
		remote = o.Remote.Height()
	}
	if err != nil || height > remote {
		return nil, fmt.Errorf("ERR_ROLLBACK: source height below saved verified archive")
	}
	return v, nil
}

type PresenceBody struct {
	Format    string `json:"format"`
	Network   string `json:"network_id"`
	Node      string `json:"node_id"`
	Height    string `json:"height"`
	Block     string `json:"block_sha256"`
	Challenge string `json:"challenge"`
	Action    string `json:"action"`
}

func (o *Observer) Announce(ctx context.Context, v *citizen.View, action string) error {
	challengeURL := strings.TrimSuffix(o.Config.Presence, "/presence") + "/challenge?node=" + o.NodeID()
	raw, err := citizen.GetBounded(o.Client, challengeURL, 4096)
	if err != nil {
		return err
	}
	var challenge struct {
		Network string `json:"network_id"`
		Value   string `json:"challenge"`
	}
	if _, err = codec.Parse(raw); err != nil || json.Unmarshal(raw, &challenge) != nil || challenge.Network != o.Config.NetworkID || len(challenge.Value) != 64 {
		return fmt.Errorf("invalid presence challenge")
	}
	body := PresenceBody{"civora-presence-1", o.Config.NetworkID, o.NodeID(), v.Height, v.Bundle.SHA256, challenge.Value, action}
	digest, _ := hex.DecodeString(codec.Hash("development/participant-presence", body))
	envelope := struct {
		Body      PresenceBody `json:"body"`
		Signature string       `json:"signature"`
	}{body, hex.EncodeToString(ed25519.Sign(o.key, digest))}
	req, err := http.NewRequestWithContext(ctx, "POST", o.Config.Presence, bytes.NewReader(codec.Must(envelope)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := o.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode != 200 {
		return fmt.Errorf("presence refused (HTTP %d)", res.StatusCode)
	}
	return nil
}
