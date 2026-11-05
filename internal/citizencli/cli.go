package citizencli

import (
	"civora/internal/citizen"
	"civora/internal/citizendev"
	"civora/internal/codec"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/cometbft/cometbft/libs/log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: civnode init | start | status | verify | election | replay")
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	home := f.String("home", ".dev-host", "public host directory")
	switch args[0] {
	case "fixtures":
		check := f.Bool("check", false, "verify reproducible archive fixtures")
		certified := f.Bool("certified", false, "complete fixture using the frozen independent TypeScript attestation")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		fixture, e := citizendev.Build()
		if e != nil {
			return e
		}
		if *certified {
			work, e := os.MkdirTemp(".tools", "certify-fixture-")
			if e != nil {
				return e
			}
			fixture, e = citizendev.Complete(fixture, work)
			if e != nil {
				return e
			}
		}
		gen, _ := codec.Blob(fixture.Runs[0].Genesis)
		files := map[string][]byte{"archive.json": codec.Must(fixture), "network.json": codec.Must(fixture.Manifest), "genesis.json": gen}
		for name, raw := range citizendev.Keys(fixture.Manifest.Main.ChainID) {
			files[name] = raw
		}
		if e = os.MkdirAll("spec/fixtures/v0.6", 0700); e != nil {
			return e
		}
		for name, data := range files {
			path := filepath.Join("spec/fixtures/v0.6", name)
			if *check {
				old, e := os.ReadFile(path)
				if e != nil || string(old) != string(data) {
					return fmt.Errorf("stale v0.6 fixture: %s", name)
				}
			} else {
				if e = os.WriteFile(path, data, 0600); e != nil {
					return e
				}
			}
		}
		work, e := os.MkdirTemp(".tools", "citizen-fixture-")
		if e != nil {
			return e
		}
		host, e := citizendev.Serve(fixture, work)
		if e != nil {
			return e
		}
		defer host.Close()
		var id string
		for _, entry := range host.Readers[fixture.Manifest.Main.ChainID].Latest().Entries {
			if entry.Table == "transport_polls" {
				id = fmt.Sprint(entry.Key)
				break
			}
		}
		record, e := host.Election(id)
		if e != nil {
			return e
		}
		if *check {
			raw, e := os.ReadFile("spec/fixtures/v0.6/record/record.json")
			if e != nil || string(raw) != string(codec.Must(record)) {
				return fmt.Errorf("stale selective public record")
			}
		} else {
			if e = citizen.ExportRecord("spec/fixtures/v0.6/record", record, host.API.Objects); e != nil {
				return e
			}
		}
		return nil
	case "init":
		manifest := f.String("manifest", "", "authenticated public manifest")
		genesis := f.String("genesis", "", "exact native main genesis")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		raw, e := os.ReadFile(*manifest)
		if e != nil {
			return e
		}
		g, e := os.ReadFile(*genesis)
		if e != nil {
			return e
		}
		return citizen.Init(*home, raw, g)
	case "start", "serve":
		mode := f.String("mode", "archive", "archive only")
		lanes := f.String("lanes", "", "comma-separated lane indices")
		transport := f.String("transport", "p2p", "p2p or authenticated archive HTTP")
		port := f.Int("p2p-port", 28650, "first loopback P2P port; RPC is next port")
		listen := f.String("listen", "127.0.0.1:8786", "public API listen address")
		fixture := f.String("fixture", "", "explicit TEST_ONLY linked archive fixture")
		archiveKey := f.String("archive-key", "", "optional PUBLIC_TEST_ONLY archive key; never required by an observer")
		archives := f.String("archives", "", "comma-separated independent archive API origins")
		workers := f.Bool("workers", true, "retry durable relay queue and publish authenticated checkpoints")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		if *mode != "archive" {
			return fmt.Errorf("only archive history is implemented; pruning cannot be claimed")
		}
		var h *citizen.Host
		var e error
		if *fixture != "" {
			raw, e := os.ReadFile(*fixture)
			if e != nil {
				return e
			}
			var fixture citizendev.Fixture
			if _, e = codec.Parse(raw); e != nil {
				return e
			}
			if e = json.Unmarshal(raw, &fixture); e != nil {
				return e
			}
			h, e = citizendev.Serve(fixture, *home)
			if e != nil {
				return e
			}
			fmt.Println("PUBLIC TEST_ONLY fixture source; synthetic linked commits")
		} else {
			h, e = citizen.Load(*home, *lanes, *transport, *port, log.NewNopLogger())
			if e != nil {
				return e
			}
		}
		defer h.Close()
		h.Services.Workers = *workers && *fixture == ""
		if *archives != "" {
			h.Services.Archives = strings.Split(*archives, ",")
		}
		if *archiveKey != "" {
			raw, e := os.ReadFile(*archiveKey)
			if e != nil {
				return e
			}
			var key struct {
				Label  string `json:"label"`
				Seed   string `json:"seed"`
				Public string `json:"public_key"`
			}
			if e = codec.Decode(raw, "citizen_test_key", &key); e != nil {
				return e
			}
			seed, _ := hex.DecodeString(key.Seed)
			h.Services.Key = ed25519.NewKeyFromSeed(seed)
			if hex.EncodeToString(h.Services.Key.Public().(ed25519.PublicKey)) != key.Public {
				return fmt.Errorf("test archive key mismatch")
			}
		}
		return serve(h, *listen)

	case "status", "verify":
		from := f.String("from", "genesis", "verification start")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		if *from != "genesis" {
			return fmt.Errorf("only full genesis verification is supported")
		}
		raw, e := os.ReadFile(filepath.Join(*home, "listen.json"))
		if e != nil {
			return offline(*home, args[0])
		}
		var local struct {
			URL string `json:"url"`
		}
		if e = codec.Decode(raw, "citizen_listener", &local); e != nil {
			return e
		}
		path := "/v1/network"
		if args[0] == "verify" {
			path = "/v1/verify"
		}
		b, e := citizen.GetBounded(citizen.Client(), local.URL+path, 1024*1024)
		if e != nil {
			return offline(*home, args[0])
		}
		fmt.Println(string(b))
		if args[0] == "verify" {
			networkRaw, e := citizen.GetBounded(citizen.Client(), local.URL+"/v1/network", 1024*1024)
			if e != nil {
				return e
			}
			var network struct {
				Chains []struct {
					ID string `json:"chain_id"`
				} `json:"chains"`
			}
			if e = json.Unmarshal(networkRaw, &network); e != nil {
				return e
			}
			for i, c := range network.Chains {
				if i == 0 {
					continue
				} // The main chain was verified above.
				raw, e := citizen.GetBounded(citizen.Client(), local.URL+"/v1/chains/"+c.ID+"/verify", 1024*1024)
				if e != nil {
					return e
				}
				fmt.Println(string(raw))
			}
		}
		return nil
	case "election":
		return election(args[1:])
	}
	return fmt.Errorf("unknown citizen command")
}
func serve(h *citizen.Host, address string) error {
	listener, e := net.Listen("tcp", address)
	if e != nil {
		return e
	}
	server := &http.Server{Handler: h.API, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	if e = os.MkdirAll(h.Home, 0700); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(h.Home, "listen.json"), codec.Must(map[string]any{"url": "http://" + listener.Addr().String()}), 0600); e != nil {
		return e
	}
	fmt.Println("Citizen archive API:", listener.Addr().String())
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	defer server.Close()
	for {
		select {
		case <-stop:
			return nil
		case e := <-done:
			if e == http.ErrServerClosed {
				return nil
			}
			return e
		case <-tick.C:
			if e = h.Refresh(); e != nil {
				fmt.Fprintln(os.Stderr, "archive verification waiting:", e)
			}
			if e = h.Tick(); e != nil {
				fmt.Fprintln(os.Stderr, "relay waiting:", e)
			}
		}
	}
}
