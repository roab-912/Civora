// Package electionguarddev prepares reproducible local fixtures, never node I/O.
package electionguarddev

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/devcli"
	"encoding/json"
	"fmt"
	"os"
)

func Fixtures(check bool) error {
	raw, e := os.ReadFile("spec/fixtures/v0.4/replay-source.json")
	if e != nil {
		return e
	}
	var source struct {
		Cases []struct {
			Label   string       `json:"label"`
			Name    string       `json:"name"`
			Genesis core.Config  `json:"genesis"`
			Blocks  []core.Block `json:"blocks"`
		} `json:"cases"`
	}
	if e = json.Unmarshal(raw, &source); e != nil {
		return e
	}
	for _, c := range source.Cases {
		if c.Label != "mixed" && c.Label != "all-no" {
			return fmt.Errorf("unknown fixture label")
		}
		engine, e := core.New(codec.Must(c.Genesis), 1)
		if e != nil {
			return e
		}
		f := devcli.Fixture{Name: c.Name, Genesis: c.Genesis, Blocks: c.Blocks, ExpectedRoots: []string{}, ExpectedEvents: [][]core.Event{}}
		for _, b := range c.Blocks {
			next, events, e := engine.Apply(codec.Must(b))
			if e != nil {
				return fmt.Errorf("%s block %s: %w", c.Label, b.Height, e)
			}
			engine = next
			f.ExpectedRoots = append(f.ExpectedRoots, next.Root())
			f.ExpectedEvents = append(f.ExpectedEvents, events)
		}
		snapshot := engine.Snapshot()
		f.ExpectedState = &snapshot
		path := "spec/fixtures/v0.4/replay-" + c.Label + ".json"
		data := codec.Must(f)
		if check {
			saved, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			if !bytes.Equal(saved, data) {
				return fmt.Errorf("stale replay fixture %s", path)
			}
		} else if e = os.WriteFile(path, data, 0600); e != nil {
			return e
		}
	}
	return nil
}
