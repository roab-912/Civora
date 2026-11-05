// Package replica supplies deterministic blocks and a durable single-writer
// journal shared by ledger and partition ABCI applications.
package replica

import (
	"bytes"
	"civora/internal/codec"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const MaxBlockBytes = 2 * 1024 * 1024

type Block struct {
	Height       string   `json:"height"`
	TimeMS       string   `json:"time_ms"`
	Transactions []string `json:"transactions"`
}
type Machine interface {
	Height() uint64
	Time() uint64
	State() []byte
	Root() string
	Check([]byte) error
	Apply(Block) (Machine, error)
	Active() bool
}
type Identity struct {
	Hash string `json:"hash"`
	Time string `json:"time"`
}
type Record struct {
	Block    Block           `json:"block"`
	Identity Identity        `json:"identity"`
	State    json.RawMessage `json:"state"`
	AppHash  string          `json:"app_hash"`
}
type Journal struct {
	directory string
	Machine   Machine
	Last      *Record
	Hook      func(string)
}

func Open(directory string, genesis []byte, factory func() (Machine, error)) (*Journal, error) {
	m, e := factory()
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(directory, 0700); e != nil {
		return nil, e
	}
	p := filepath.Join(directory, "genesis.json")
	saved, e := os.ReadFile(p)
	if os.IsNotExist(e) {
		if e = write(directory, p, genesis, nil); e != nil {
			return nil, e
		}
	} else if e != nil {
		return nil, e
	} else if !bytes.Equal(saved, genesis) {
		return nil, fmt.Errorf("consensus genesis mismatch")
	}
	j := &Journal{directory: directory, Machine: m}
	paths, e := filepath.Glob(filepath.Join(directory, "*.record.json"))
	if e != nil {
		return nil, e
	}
	sort.Strings(paths)
	for _, p := range paths {
		raw, e := os.ReadFile(p)
		if e != nil {
			return nil, e
		}
		var r Record
		if e = codec.Decode(raw, "replica_record", &r); e != nil {
			return nil, e
		}
		h, _ := codec.Uint(r.Block.Height)
		if filepath.Base(p) != fmt.Sprintf("%020d.record.json", h) {
			return nil, fmt.Errorf("record filename/height mismatch")
		}
		next, e := j.Machine.Apply(r.Block)
		if e != nil {
			return nil, e
		}
		if next.Root() != r.AppHash || !bytes.Equal(next.State(), r.State) {
			return nil, fmt.Errorf("record replay mismatch at %s", r.Block.Height)
		}
		j.Machine = next
		j.Last = &r
	}
	return j, nil
}
func (j *Journal) Commit(r Record, next Machine) error {
	h, e := codec.Uint(r.Block.Height)
	if e != nil || next == nil || h != next.Height() || next.Height() != j.Machine.Height()+1 || r.AppHash != next.Root() || !bytes.Equal(r.State, next.State()) {
		return fmt.Errorf("invalid commit candidate")
	}
	raw := codec.Must(r)
	if e := codec.Validate("replica_record", mustParse(raw)); e != nil {
		return e
	}
	path := filepath.Join(j.directory, fmt.Sprintf("%020d.record.json", next.Height()))
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		return fmt.Errorf("record already exists or inaccessible")
	}
	if e := write(j.directory, path, raw, j.Hook); e != nil {
		return e
	}
	j.Machine = next
	j.Last = &r
	return nil
}
func mustParse(raw []byte) any { v, _ := codec.Parse(raw); return v }
func write(directory, path string, data []byte, hook func(string)) error {
	f, e := os.CreateTemp(directory, ".pending-")
	if e != nil {
		return e
	}
	defer f.Close()
	defer os.Remove(f.Name())
	call := func(s string) {
		if hook != nil {
			hook(s)
		}
	}
	if _, e = f.Write(data[:len(data)/2]); e != nil {
		return e
	}
	call("partial_write")
	if _, e = f.Write(data[len(data)/2:]); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	call("after_sync")
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	call("after_rename")
	return syncDirectory(directory)
}
