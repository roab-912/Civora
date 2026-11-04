// Package store is a single-writer development ledger. Each immutable commit
// file contains the block, state, deterministic events and replay marker.
package store

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Record struct {
	Block   core.Block   `json:"block"`
	State   core.State   `json:"state"`
	Events  []core.Event `json:"events"`
	AppHash string       `json:"app_hash"`
}
type Store struct {
	dir    string
	engine *core.Engine
	mu     sync.Mutex
	hook   func(string)
	txs    map[string]string
}

func Open(dir string, genesis []byte, supportedEpoch uint64) (*Store, error) {
	return OpenWithFactory(dir, genesis, supportedEpoch, core.New)
}

// OpenWithFactory reuses exactly the same adapters during recovery. A normal
// civnode always passes core.New; test simulators use a separate binary.
func OpenWithFactory(dir string, genesis []byte, supportedEpoch uint64, newEngine func([]byte, uint64) (*core.Engine, error)) (*Store, error) {
	e, err := newEngine(genesis, supportedEpoch)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	gp := filepath.Join(dir, "genesis.json")
	saved, err := os.ReadFile(gp)
	if os.IsNotExist(err) {
		if err = atomicWrite(dir, gp, genesis, nil); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else if !bytes.Equal(saved, genesis) {
		return nil, fmt.Errorf("genesis mismatch")
	}
	s := &Store{dir: dir, engine: e, txs: map[string]string{}}
	files, err := filepath.Glob(filepath.Join(dir, "*.commit.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var record Record
		if err = codec.Decode(raw, "commit", &record); err != nil {
			return nil, fmt.Errorf("corrupt commit %s: %w", file, err)
		}
		if filepath.Base(file) != commitName(record.Block.Height) {
			return nil, fmt.Errorf("commit filename/height mismatch")
		}
		next, events, err := s.engine.Apply(codec.Must(record.Block))
		if err != nil {
			return nil, fmt.Errorf("replay: %w", err)
		}
		if record.AppHash != next.Root() || !bytes.Equal(codec.Must(record.State), codec.Must(next.Snapshot())) || !bytes.Equal(codec.Must(record.Events), codec.Must(events)) {
			return nil, fmt.Errorf("commit verification failed")
		}
		s.engine = next
		s.index(record.Block)
	}
	return s, nil
}
func commitName(height string) string {
	h, _ := strconv.ParseUint(height, 10, 64)
	return fmt.Sprintf("%020d.commit.json", h)
}
func (s *Store) index(b core.Block) {
	for _, tx := range b.Transactions {
		s.txs[core.TxID(tx.Body)] = b.Height
	}
}
func (s *Store) Height() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strconv.FormatUint(s.engine.Height, 10)
}
func (s *Store) AppHash() string { s.mu.Lock(); defer s.mu.Unlock(); return s.engine.Root() }
func (s *Store) Snapshot() core.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.engine.Clone().Snapshot()
}
func (s *Store) Status(txID string) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h, ok := s.txs[txID]; ok {
		return "finalized", h
	}
	return "unknown", ""
}

// Commit makes no state visible until its complete commit record is durable.
// A caller must reopen after an I/O error: a rename may already be durable.
func (s *Store) Commit(raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, events, err := s.engine.Apply(raw)
	if err != nil {
		return err
	}
	var b core.Block
	if err = codec.Decode(raw, "block", &b); err != nil {
		return err
	}
	r := Record{b, next.Snapshot(), events, next.Root()}
	data := codec.Must(r)
	path := filepath.Join(s.dir, commitName(b.Height))
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		return fmt.Errorf("commit already exists or inaccessible")
	}
	if err = atomicWrite(s.dir, path, data, s.hook); err != nil {
		return err
	}
	s.engine = next
	s.index(b)
	return nil
}
func atomicWrite(dir, path string, data []byte, hook func(string)) error {
	f, err := os.CreateTemp(dir, ".pending-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	call := func(point string) {
		if hook != nil {
			hook(point)
		}
	}
	// A real partial write is exposed to the process-kill acceptance test.
	half := len(data) / 2
	if _, err = f.Write(data[:half]); err != nil {
		return err
	}
	call("partial_write")
	if _, err = f.Write(data[half:]); err != nil {
		return err
	}
	call("after_write")
	if err = f.Sync(); err != nil {
		return err
	}
	call("after_sync")
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(temp, path); err != nil {
		return err
	}
	call("after_rename")
	if err = syncDir(dir); err != nil {
		return err
	}
	call("after_directory_sync")
	return nil
}

// Export returns exact committed records for independent replay. Temporary
// files left by a killed process never count as commits.
func (s *Store) Export() ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	files, e := filepath.Glob(filepath.Join(s.dir, "*.commit.json"))
	if e != nil {
		return nil, e
	}
	sort.Strings(files)
	out := [][]byte{}
	for _, p := range files {
		if strings.HasPrefix(filepath.Base(p), ".") {
			continue
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, nil
}
