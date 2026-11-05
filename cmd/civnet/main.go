package main

import (
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/network"
	"civora/internal/partitiondev"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/types"
	"os"
	"os/signal"
	"syscall"
)

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: civnet init | serve")
	}
	switch os.Args[1] {
	case "verify-state":
		f := flag.NewFlagSet("verify-state", flag.ContinueOnError)
		path := f.String("in", "", "state proof and native signed header")
		chain := f.String("chain", "", "pinned TEST_ONLY development chain")
		if e := f.Parse(os.Args[2:]); e != nil {
			return e
		}
		raw, e := os.ReadFile(*path)
		if e != nil {
			return e
		}
		if len(raw) > 256*1024 {
			return fmt.Errorf("state proof bound")
		}
		var p consensus.StateProof
		if e = codec.Decode(raw, "consensus_state_proof", &p); e != nil {
			return e
		}
		if _, e = consensus.VerifyState(network.Trust(*chain), p); e != nil {
			return e
		}
		fmt.Println("Native TEST_ONLY state proof verified after height", p.Height)
		return nil
	case "fixtures":
		f := flag.NewFlagSet("fixtures", flag.ContinueOnError)
		check := f.Bool("check", false, "verify deterministic fixtures")
		if e := f.Parse(os.Args[2:]); e != nil {
			return e
		}
		fixture, e := partitiondev.Generate()
		if e != nil {
			return e
		}
		raw := codec.Must(fixture)
		files, e := partitiondev.NetworkFixtures()
		if e != nil {
			return e
		}
		files["transport.json"] = raw
		for name, bytes := range files {
			p := "spec/fixtures/v0.5/" + name
			if *check {
				old, e := os.ReadFile(p)
				if e != nil {
					return e
				}
				if string(old) != string(bytes) {
					return fmt.Errorf("stale fixture: %s", name)
				}
			} else {
				if e = os.WriteFile(p, bytes, 0600); e != nil {
					return e
				}
			}
		}
		return nil
	case "verify-commit":
		f := flag.NewFlagSet("verify-commit", flag.ContinueOnError)
		path := f.String("in", "", "native commit RPC result")
		chain := f.String("chain", "", "pinned TEST_ONLY development chain")
		appPath := f.String("app", "", "trusted development app genesis with a rotation schedule")
		if e := f.Parse(os.Args[2:]); e != nil {
			return e
		}
		raw, e := os.ReadFile(*path)
		if e != nil {
			return e
		}
		if len(raw) > 64*1024 {
			return fmt.Errorf("commit bound")
		}
		var r struct {
			Header types.SignedHeader `json:"signed_header"`
		}
		if e = cmtjson.Unmarshal(raw, &r); e != nil {
			return e
		}
		header, e := cmtjson.Marshal(r.Header)
		if e != nil {
			return e
		}
		trust := network.Trust(*chain)
		if *appPath != "" {
			raw, e := os.ReadFile(*appPath)
			if e != nil {
				return e
			}
			var g network.Genesis
			if e = codec.Decode(raw, "network_genesis", &g); e != nil {
				return e
			}
			if expected, e := network.ApplicationChain(g); e != nil || expected != *chain {
				return fmt.Errorf("trusted chain mismatch")
			}
			m, e := network.Machine(g, 2)
			if e != nil {
				return e
			}
			if scheduled, ok := m.(*consensus.Scheduled); ok {
				if r.Header.Height < 1 {
					return fmt.Errorf("header height")
				}
				trust = scheduled.TrustAt(uint64(r.Header.Height))
			}
		}
		sh, e := consensus.VerifyHeader(trust, base64.RawURLEncoding.EncodeToString(header))
		if e != nil {
			return e
		}
		fmt.Println("Native TEST_ONLY commit verified at height", sh.Height)
		return nil
	case "init":
		f := flag.NewFlagSet("init", flag.ContinueOnError)
		dir := f.String("dir", ".dev-network", "new directory")
		fixture := f.String("fixture", "spec/fixtures/accounts.json", "ledger fixture genesis")
		appPath := f.String("app", "", "explicit network_genesis file (instead of fixture)")
		chain := f.String("chain", "", "chain ID (defaults to fixture)")
		port := f.Int("port", 27650, "first loopback P2P port")
		if e := f.Parse(os.Args[2:]); e != nil {
			return e
		}
		var app network.Genesis
		if *appPath != "" {
			raw, e := os.ReadFile(*appPath)
			if e != nil {
				return e
			}
			if e = codec.Decode(raw, "network_genesis", &app); e != nil {
				return e
			}
			if *chain == "" {
				return fmt.Errorf("-chain required with -app")
			}
		} else {
			raw, e := os.ReadFile(*fixture)
			if e != nil {
				return e
			}
			var v struct {
				Genesis json.RawMessage `json:"genesis"`
			}
			if e = json.Unmarshal(raw, &v); e != nil {
				return e
			}
			var g struct {
				ChainID string `json:"chain_id"`
			}
			if e = json.Unmarshal(v.Genesis, &g); e != nil {
				return e
			}
			if *chain == "" {
				*chain = g.ChainID
			}
			app = network.Genesis{Kind: "ledger", Data: v.Genesis}
		}
		if e := network.Init(*dir, *chain, app, *port); e != nil {
			return e
		}
		fmt.Println("Seven TEST_ONLY nodes initialized in", *dir)
		return nil
	case "serve":
		f := flag.NewFlagSet("serve", flag.ContinueOnError)
		home := f.String("home", ".dev-network/node0", "existing node home")
		epoch := f.Uint64("supported-epoch", 2, "maximum implemented epoch")
		generation := f.Uint64("key-generation", 0, "preinitialized signer generation from development genesis")
		verbose := f.Bool("verbose", false, "consensus logs")
		if e := f.Parse(os.Args[2:]); e != nil {
			return e
		}
		logger := log.NewNopLogger()
		if *verbose {
			logger = log.NewTMLogger(log.NewSyncWriter(os.Stderr))
		}
		n, close, e := network.StartWithSigner(*home, *epoch, *generation, logger)
		if e != nil {
			return e
		}
		defer close()
		defer func() { _ = n.Stop(); n.Wait() }()
		fmt.Println("TEST_ONLY node running:", *home)
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(stop)
		select {
		case <-stop:
		case <-n.Quit():
		}
		return nil
	default:
		return fmt.Errorf("unknown command")
	}
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
