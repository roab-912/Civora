// civnode is a local development replay CLI, not a consensus node.
package main

import (
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/state/store"
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

type fixture struct {
	Name           string         `json:"name"`
	Genesis        core.Config    `json:"genesis"`
	Blocks         []core.Block   `json:"blocks"`
	ExpectedRoots  []string       `json:"expected_roots"`
	ExpectedEvents [][]core.Event `json:"expected_events"`
}

func run() error {
	if len(os.Args) < 2 || os.Args[1] != "replay" {
		return fmt.Errorf("usage: civnode replay -fixture spec/fixtures/accounts.json [-store .dev-store] [-supported-epoch 1]")
	}
	f := flag.NewFlagSet("replay", flag.ContinueOnError)
	path := f.String("fixture", "spec/fixtures/accounts.json", "canonical development fixture")
	dir := f.String("store", ".dev-store", "local single-writer store")
	supported := f.Uint64("supported-epoch", 1, "maximum supported development epoch")
	if e := f.Parse(os.Args[2:]); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	raw, e := os.ReadFile(*path)
	if e != nil {
		return e
	}
	var v fixture
	if e = codec.Decode(raw, "fixture", &v); e != nil {
		return e
	}
	if len(v.ExpectedRoots) != len(v.Blocks) || len(v.ExpectedEvents) != len(v.Blocks) {
		return fmt.Errorf("fixture roots missing")
	}
	s, e := store.Open(*dir, codec.Must(v.Genesis), *supported)
	if e != nil {
		return e
	}
	// Verify the complete fixture independently, including already durable
	// heights, so resume never silently accepts a different block history.
	engine, e := core.New(codec.Must(v.Genesis), *supported)
	if e != nil {
		return e
	}
	records, e := s.Export()
	if e != nil {
		return e
	}
	if len(records) > len(v.Blocks) {
		return fmt.Errorf("store longer than fixture")
	}
	for i, b := range v.Blocks {
		block := codec.Must(b)
		next, events, e := engine.Apply(block)
		if e != nil {
			return e
		}
		if next.Root() != v.ExpectedRoots[i] {
			return fmt.Errorf("fixture root mismatch at %s", b.Height)
		}
		if string(codec.Must(events)) != string(codec.Must(v.ExpectedEvents[i])) {
			return fmt.Errorf("fixture events mismatch at %s", b.Height)
		}
		engine = next
		if i < len(records) {
			var r store.Record
			if e = json.Unmarshal(records[i], &r); e != nil {
				return e
			}
			if string(codec.Must(r.Block)) != string(block) {
				return fmt.Errorf("fixture history mismatch")
			}
		} else {
			if e = s.Commit(block); e != nil {
				return e
			}
		}
	}
	fmt.Println(string(codec.Must(map[string]any{"height": s.Height(), "app_hash": s.AppHash()})))
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
