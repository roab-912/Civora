// Package eligibilityindex rebuilds a path index from the verified local ledger.
// The index is derived and never supplies authority to the state transition.
package eligibilityindex

import (
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/eligibility"
	"civora/internal/state/store"
	"encoding/json"
	"fmt"
	"strconv"
)

func Metadata(state core.State) (eligibility.Metadata, error) {
	for _, entry := range state.Entries {
		if entry.Table == "network" && entry.Key == "config" {
			var network struct {
				Eligibility *eligibility.Metadata `json:"eligibility"`
			}
			if e := json.Unmarshal(codec.Must(entry.Value), &network); e != nil {
				return eligibility.Metadata{}, e
			}
			if network.Eligibility == nil {
				return eligibility.Metadata{}, fmt.Errorf("ERR_ELIGIBILITY_DISABLED")
			}
			return *network.Eligibility, network.Eligibility.Validate()
		}
	}
	return eligibility.Metadata{}, fmt.Errorf("ERR_NETWORK_STATE")
}

type position struct {
	index      uint64
	commitment string
}

func Sync(dir string, source *store.Store) (*eligibility.Store, error) {
	current, e := Metadata(source.Snapshot())
	if e != nil {
		return nil, e
	}
	initial := current
	initial.Height = "0"
	initial.Size = "0"
	initial.Active = "0"
	initial.Root = nil
	initial.SnapshotID = initial.ID()
	index, e := eligibility.OpenStore(dir, initial)
	if e != nil {
		return nil, e
	}
	success := false
	defer func() {
		if !success {
			_ = index.Close()
		}
	}()
	latest, e := index.Latest()
	if e != nil {
		return nil, e
	}
	if latest.NetworkID != current.NetworkID || latest.Epoch != current.Epoch || latest.SuiteID != current.SuiteID || latest.TreeID != current.TreeID || latest.CircuitDepth != current.CircuitDepth {
		return nil, fmt.Errorf("ERR_INDEX_DOMAIN_CHANGE")
	}
	last, _ := codec.Uint(latest.Height)
	currentHeight, _ := codec.Uint(current.Height)
	if last > currentHeight {
		return nil, fmt.Errorf("ERR_INDEX_AHEAD")
	}
	positions := map[string]position{}
	size := uint64(0)
	records, e := source.Export()
	if e != nil {
		return nil, e
	}
	for _, raw := range records {
		var record store.Record
		if e = codec.Decode(raw, "commit", &record); e != nil {
			return nil, e
		}
		expected, e := Metadata(record.State)
		if e != nil {
			return nil, e
		}
		if expected.NetworkID != initial.NetworkID || expected.Epoch != initial.Epoch || expected.SuiteID != initial.SuiteID || expected.CircuitDepth != initial.CircuitDepth {
			return nil, fmt.Errorf("ERR_INDEX_DOMAIN_CHANGE")
		}
		changes := []eligibility.Change{}
		for _, tx := range record.Block.Transactions {
			body := tx.Body
			switch body.Type {
			case "account.register":
				var payload core.Payload
				_ = json.Unmarshal(body.Payload, &payload)
				positions[body.ActorID] = position{size, payload.Commitment}
				changes = append(changes, eligibility.Change{Operation: "append", Index: fmt.Sprint(size), Commitment: payload.Commitment})
				size++
			case "account.rotate":
				p, ok := positions[body.ActorID]
				if !ok {
					return nil, fmt.Errorf("ERR_INDEX_ACCOUNT")
				}
				var payload core.Payload
				_ = json.Unmarshal(body.Payload, &payload)
				if p.commitment != payload.Commitment {
					changes = append(changes, eligibility.Change{Operation: "update", Index: fmt.Sprint(p.index), Commitment: "0"}, eligibility.Change{Operation: "append", Index: fmt.Sprint(size), Commitment: payload.Commitment})
					positions[body.ActorID] = position{size, payload.Commitment}
					size++
				}
			case "account.retire":
				p, ok := positions[body.ActorID]
				if !ok {
					return nil, fmt.Errorf("ERR_INDEX_ACCOUNT")
				}
				changes = append(changes, eligibility.Change{Operation: "update", Index: fmt.Sprint(p.index), Commitment: "0"})
				delete(positions, body.ActorID)
			}
		}
		height, e := strconv.ParseUint(record.Block.Height, 10, 64)
		if e != nil {
			return nil, e
		}
		if height <= last {
			m, e := index.Snapshot(height)
			if e != nil {
				return nil, e
			}
			if m.SnapshotID != expected.SnapshotID {
				return nil, fmt.Errorf("ERR_LEDGER_ROOT_MISMATCH")
			}
		} else {
			if _, e = index.CommitExpected(height, changes, &expected); e != nil {
				return nil, e
			}
		}
	}
	success = true
	return index, nil
}
