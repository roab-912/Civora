// civeligibility is a development-only path, proof and capacity harness.
package main

import (
	"civora/internal/codec"
	"civora/internal/eligibility"
	"civora/internal/eligibilityindex"
	"civora/internal/semaphore"
	"civora/internal/state/store"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"
)

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: civeligibility build-fixture|path|serve|verify-membership")
	}
	command := os.Args[1]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	dir := flags.String("store", ".tools/eligibility-demo", "development tree directory")
	count := flags.Uint64("count", 1000, "active development leaves")
	circuit := flags.Uint("depth", 26, "pinned circuit depth")
	special := flags.String("special-leaf", "", "optional last test identity commitment")
	height := flags.Uint64("height", 1, "snapshot height")
	index := flags.Uint64("index", 0, "physical leaf index")
	addr := flags.String("addr", "127.0.0.1:8783", "loopback-only development API")
	input := flags.String("input", "", "canonical membership fixture")
	ledger := flags.String("ledger", "", "verified local account ledger")
	check := flags.Bool("check", false, "check committed fixture without rewriting")
	if e := flags.Parse(os.Args[2:]); e != nil {
		return e
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if command == "fixtures" {
		return eligibilityindex.GenerateFixture(*check)
	}
	if command == "index" {
		raw, e := os.ReadFile(*ledger + string(os.PathSeparator) + "genesis.json")
		if e != nil {
			return e
		}
		source, e := store.Open(*ledger, raw, 1)
		if e != nil {
			return e
		}
		index, e := eligibilityindex.Sync(*dir, source)
		if e != nil {
			return e
		}
		defer index.Close()
		m, e := index.Latest()
		if e != nil {
			return e
		}
		fmt.Println(string(codec.Must(m)))
		return nil
	}
	if command == "verify-membership" {
		raw, e := os.ReadFile(*input)
		if e != nil {
			return e
		}
		if _, e = codec.Parse(raw); e != nil {
			return e
		}
		var f struct {
			Context    json.RawMessage `json:"context"`
			Ballot     string          `json:"ballot_base64url"`
			Membership json.RawMessage `json:"membership"`
		}
		if e = json.Unmarshal(raw, &f); e != nil {
			return e
		}
		var context semaphore.Context
		var membership semaphore.Membership
		if e = codec.Decode(f.Context, "membership_context", &context); e != nil {
			return e
		}
		if e = codec.Decode(f.Membership, "membership_v03", &membership); e != nil {
			return e
		}
		ballot, e := codec.Blob(f.Ballot)
		if e != nil {
			return e
		}
		v, e := semaphore.New()
		if e != nil {
			return e
		}
		if e = v.Verify(context, ballot, membership); e != nil {
			return e
		}
		fmt.Println(`{"valid":true}`)
		return nil
	}
	if command == "build-fixture" {
		if *circuit != 16 && *circuit != 26 {
			return fmt.Errorf("ERR_CIRCUIT_DEPTH")
		}
		tree, _ := eligibility.New(uint8(*circuit))
		m := tree.Metadata(codec.Hash("eligibility/test-network", "PUBLIC TEST ONLY v0.3"), "1", "capacity-test", "semaphore-v4.14.5-depth-"+fmt.Sprint(*circuit), fmt.Sprint(*height))
		var last eligibility.Node
		var e error
		if *special != "" {
			last, e = eligibility.ParseField(*special, true)
			if e != nil {
				return e
			}
		}
		started := time.Now()
		s, e := eligibility.BuildStore(*dir, m, *count, func(i uint64) (eligibility.Node, error) {
			if i+1 == *count && *special != "" {
				return last, nil
			}
			var n eligibility.Node
			new(big.Int).SetUint64(i + 1).FillBytes(n[:])
			return n, nil
		}, func(level int, nodes uint64) {
			fmt.Fprintln(os.Stderr, string(codec.Must(map[string]any{"level": fmt.Sprint(level), "nodes": fmt.Sprint(nodes), "elapsed_ms": fmt.Sprint(time.Since(started).Milliseconds())})))
		})
		if e != nil {
			return e
		}
		defer s.Close()
		snapshot, e := s.Latest()
		if e != nil {
			return e
		}
		fmt.Println(string(codec.Must(map[string]any{"snapshot": snapshot, "elapsed_ms": fmt.Sprint(time.Since(started).Milliseconds()), "go_version": runtime.Version(), "platform": runtime.GOOS + "/" + runtime.GOARCH})))
		return nil
	}
	meta, e := eligibility.BaseMetadata(*dir)
	if e != nil {
		return e
	}
	s, e := eligibility.OpenStore(*dir, meta)
	if e != nil {
		return e
	}
	defer s.Close()
	switch command {
	case "exercise-fixture":
		before, e := s.Snapshot(1)
		if e != nil {
			return e
		}
		if before.TreeID != "capacity-test" {
			return fmt.Errorf("fixture exercise requires a capacity-test tree")
		}
		size, e := codec.Uint(before.Size)
		if e != nil || size < 3 {
			return fmt.Errorf("ERR_CAPACITY")
		}
		latest, e := s.Latest()
		if e != nil {
			return e
		}
		if latest.Height == "1" {
			_, e = s.Commit(2, []eligibility.Change{{Operation: "update", Index: "0", Commitment: "0"}, {Operation: "append", Index: fmt.Sprint(size), Commitment: fmt.Sprint(size + 1)}, {Operation: "update", Index: "1", Commitment: "0"}})
			if e != nil {
				return e
			}
		} else if latest.Height != "2" {
			return fmt.Errorf("unexpected fixture version")
		}
		if e = s.Close(); e != nil {
			return e
		}
		s, e = eligibility.OpenStore(*dir, meta)
		if e != nil {
			return e
		}
		defer s.Close()
		after, e := s.Snapshot(2)
		if e != nil {
			return e
		}
		oldPath, e := s.Path(1, size-1)
		if e != nil {
			return e
		}
		currentPath, e := s.Path(2, size-1)
		if e != nil {
			return e
		}
		if _, e = s.Path(2, 0); e == nil {
			return fmt.Errorf("retired leaf is still active")
		}
		if after.Size != fmt.Sprint(size+1) || after.Active != fmt.Sprint(size-1) {
			return fmt.Errorf("ERR_ACTIVE_COUNT")
		}
		fmt.Println(string(codec.Must(map[string]any{"initial": before, "after": after, "historical_path": oldPath, "current_path": currentPath, "retired_leaf_refused": true, "reopened": true})))
		return nil
	case "path":
		p, e := s.Path(*height, *index)
		if e != nil {
			return e
		}
		fmt.Println(string(codec.Must(p)))
		return nil
	case "serve":
		host, _, e := net.SplitHostPort(*addr)
		if e != nil {
			return e
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("development server requires a loopback address")
		}
		server := &http.Server{Addr: *addr, Handler: eligibility.Handler(s), ReadHeaderTimeout: 5 * time.Second}
		listener, e := net.Listen("tcp", *addr)
		if e != nil {
			return e
		}
		fmt.Fprintln(os.Stderr, "Development path API: http://"+listener.Addr().String())
		return server.Serve(listener)
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
