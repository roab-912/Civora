package participation

import (
	"archive/zip"
	"bytes"
	"civora/internal/archive"
	"civora/internal/citizendev"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/network"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func testSource(t *testing.T) (Config, *atomic.Uint64, string) {
	t.Helper()
	raw, err := os.ReadFile("../../spec/fixtures/v0.4/replay-source.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Cases []struct {
			Genesis core.Config `json:"genesis"`
		} `json:"cases"`
	}
	if err = json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	builder, err := citizendev.New(network.Genesis{Kind: "ledger", Data: codec.Must(source.Cases[0].Genesis)})
	if err != nil {
		t.Fatal(err)
	}
	for height := uint64(1); height <= 3; height++ {
		if err = builder.Add(height * 1000); err != nil {
			t.Fatal(err)
		}
	}
	height := &atomic.Uint64{}
	height.Store(3)
	challenge := strings.Repeat("ab", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/network/challenge" {
			_, _ = w.Write(codec.Must(map[string]string{"network_id": source.Cases[0].Genesis.NetworkID, "challenge": challenge}))
			return
		}
		if r.URL.Path == "/network/presence" {
			var envelope struct {
				Body      PresenceBody `json:"body"`
				Signature string       `json:"signature"`
			}
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
			if json.Unmarshal(raw, &envelope) != nil {
				http.Error(w, "bad report", 400)
				return
			}
			pk, _ := hex.DecodeString(envelope.Body.Node)
			sig, _ := hex.DecodeString(envelope.Signature)
			digest, _ := hex.DecodeString(codec.Hash("development/participant-presence", envelope.Body))
			if len(pk) != 32 || !ed25519.Verify(pk, digest, sig) || envelope.Body.Challenge != challenge || envelope.Body.Block != archive.Digest(codec.Must(builder.Run.Blocks[2])) {
				http.Error(w, "bad proof", 400)
				return
			}
			_, _ = w.Write([]byte(`{"accepted":true}`))
			return
		}
		base := "/api/v1/chains/" + builder.Genesis.Doc.ChainID
		if r.URL.Path == base+"/blocks" {
			_, _ = w.Write(codec.Must(map[string]string{"chain_id": builder.Genesis.Doc.ChainID, "height": fmt.Sprint(height.Load())}))
			return
		}
		for n, block := range builder.Run.Blocks {
			if r.URL.Path == fmt.Sprintf("%s/blocks/%d", base, n+1) {
				_, _ = w.Write(codec.Must(block))
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	config := Config{"civora-observer-1", source.Cases[0].Genesis.NetworkID, base64.RawURLEncoding.EncodeToString(builder.Genesis.Raw), archive.Digest(builder.Genesis.Raw), server.URL + "/api", server.URL + "/network/presence", "", true}
	return config, height, builder.Machine.Root()
}

func TestObserverReplayResumeAndRollback(t *testing.T) {
	c, remoteHeight, root := testSource(t)
	home := filepath.Join(t.TempDir(), "node")
	first, err := Open(codec.Must(c), home)
	if err != nil {
		t.Fatal(err)
	}
	view, err := first.Sync()
	if err != nil || view.Root != root || view.Height != "3" {
		t.Fatalf("sync: %v, %+v", err, view)
	}
	id := first.NodeID()
	if err = first.Announce(context.Background(), view, "online"); err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(codec.Must(c), home)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.NodeID() != id {
		t.Fatal("identity changed on restart")
	}
	view, err = second.Sync()
	if err != nil || view.Root != root {
		t.Fatalf("resume: %v", err)
	}
	remoteHeight.Store(2)
	if _, err = second.Sync(); err == nil || !strings.Contains(err.Error(), "ERR_ROLLBACK") {
		t.Fatalf("accepted rollback: %v", err)
	}
	if second.Reader.Latest().Height != "3" {
		t.Fatal("saved floor lowered")
	}
	remoteHeight.Store(0)
	if _, err = second.Sync(); err == nil {
		t.Fatal("accepted unavailable source")
	}
	remoteHeight.Store(3)
	if _, err = second.Sync(); err != nil {
		t.Fatal(err)
	}
	changed := c
	changed.API += "-other"
	if other, err := Open(codec.Must(changed), home); err == nil {
		_ = other.Close()
		t.Fatal("silently changed existing bootstrap")
	}
}

func TestObserverAbruptChild(t *testing.T) {
	if os.Getenv("CIVORA_OBSERVER_CRASH_TEST") != "1" {
		return
	}
	raw, err := os.ReadFile(os.Getenv("CIVORA_OBSERVER_CONFIG"))
	if err != nil {
		os.Exit(78)
	}
	observer, err := Open(raw, os.Getenv("CIVORA_OBSERVER_HOME"))
	if err != nil {
		os.Exit(79)
	}
	if _, err = observer.Sync(); err != nil {
		os.Exit(80)
	}
	os.Exit(77) // Deliberately bypass Close and all deferred cleanup.
}

func TestObserverAbruptExitKeepsArchiveAndIdentity(t *testing.T) {
	c, _, root := testSource(t)
	directory := t.TempDir()
	file, home := filepath.Join(directory, "bootstrap.json"), filepath.Join(directory, "node")
	if err := os.WriteFile(file, codec.Must(c), 0600); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestObserverAbruptChild$")
	child.Env = append(os.Environ(), "CIVORA_OBSERVER_CRASH_TEST=1", "CIVORA_OBSERVER_CONFIG="+file, "CIVORA_OBSERVER_HOME="+home)
	err := child.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 77 {
		t.Fatalf("child did not crash at expected point: %v", err)
	}
	key, err := os.ReadFile(filepath.Join(home, "presence-key.PRIVATE.json"))
	if err != nil {
		t.Fatal(err)
	}
	observer, err := Open(codec.Must(c), home)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	view, err := observer.Sync()
	if err != nil || view.Root != root || view.Height != "3" {
		t.Fatalf("abrupt resume failed: %v", err)
	}
	again, _ := os.ReadFile(filepath.Join(home, "presence-key.PRIVATE.json"))
	if !bytes.Equal(key, again) {
		t.Fatal("identity replaced after crash")
	}
}

func TestBootstrapAndBundleStrictness(t *testing.T) {
	c, _, _ := testSource(t)
	caseAlias := strings.Replace(string(codec.Must(c)), `"ca_pem":`, `"CA_PEM":`, 1)
	if _, _, err := DecodeConfig([]byte(caseAlias)); err == nil {
		t.Fatal("accepted a case-insensitive field alias")
	}
	for _, change := range []func(*Config){func(c *Config) { c.NetworkID = strings.Repeat("ff", 32) }, func(c *Config) { c.GenesisSHA256 = strings.Repeat("00", 32) }, func(c *Config) { c.Presence = "https://example.org/network/presence" }, func(c *Config) { c.PublicTestOnly = false }, func(c *Config) { c.API = "https://example.org/api" }} {
		bad := c
		change(&bad)
		if _, _, err := DecodeConfig(codec.Must(bad)); err == nil {
			t.Fatal("accepted invalid bootstrap")
		}
	}
	for _, bad := range [][]byte{[]byte(strings.Replace(string(codec.Must(c)), `"ca_pem":"",`, "", 1)), []byte(strings.TrimSuffix(string(codec.Must(c)), "}") + `,"extra":true}`), []byte(strings.TrimSuffix(string(codec.Must(c)), "}") + `,"public_test_only":true}`)} {
		if _, _, err := DecodeConfig(bad); err == nil {
			t.Fatal("accepted missing, extra or duplicate field")
		}
	}
	dir := t.TempDir()
	binary, config := filepath.Join(dir, "test-binary"), filepath.Join(dir, "config.json")
	_ = os.WriteFile(binary, []byte("PUBLIC_TEST_ONLY_BINARY"), 0600)
	_ = os.WriteFile(config, codec.Must(c), 0600)
	var archives [][]byte
	for _, name := range []string{"first.zip", "second.zip"} {
		file := filepath.Join(dir, name)
		if err := Bundle([]string{"--binary", binary, "--config", config, "--output", file, "--target", "windows-amd64"}); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(file)
		archives = append(archives, raw)
		z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			t.Fatal(err)
		}
		if len(z.File) != 4 {
			t.Fatalf("unexpected archive contents: %d", len(z.File))
		}
		for _, file := range z.File {
			if strings.Contains(file.Name, "PRIVATE") || strings.Contains(file.Name, "..") {
				t.Fatal("private or unsafe archive entry")
			}
		}
	}
	if !bytes.Equal(archives[0], archives[1]) {
		t.Fatal("ZIP is not reproducible")
	}
	if err := Bundle([]string{"--binary", binary, "--config", config, "--output", filepath.Join(dir, "first.zip"), "--target", "windows-amd64"}); err == nil {
		t.Fatal("replaced existing ZIP")
	}
}

func TestLocalCertificateTrustStillVerifiesTLSHostname(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("public test server")) }))
	defer server.Close()
	config := Config{}
	client, err := config.Client()
	if err != nil {
		t.Fatal(err)
	}
	if response, err := client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("accepted an untrusted TLS certificate")
	}
	config.CAPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	client, err = config.Client()
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	transport := client.Transport.(*http.Transport)
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS verification disabled")
	}
	transport.CloseIdleConnections()
	transport.TLSClientConfig.ServerName = "wrong-host.invalid"
	if response, err = client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("accepted the wrong certificate hostname")
	}
}
