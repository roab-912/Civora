package citizen

import (
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/replica"
	"encoding/base64"
	"fmt"
	"github.com/cometbft/cometbft/libs/log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

func Init(home string, manifestRaw, genesisRaw []byte) error {
	m, e := ParseManifest(manifestRaw)
	if e != nil {
		return e
	}
	if _, e = m.CheckGenesis(m.Main, genesisRaw); e != nil {
		return e
	}
	if e = os.Mkdir(home, 0700); e != nil {
		return e
	}
	if e = replica.Persist(home, filepath.Join(home, "network.json"), manifestRaw, nil); e != nil {
		return e
	}
	if _, e = (archive.Store{Directory: home}).Put(genesisRaw); e != nil {
		return e
	}
	return nil
}

type Host struct {
	Home      string
	Manifest  Manifest
	API       *API
	Readers   map[string]*Reader
	Native    []*Native
	Services  Services
	closeOnce sync.Once
}

// Scheduled validator rotations decorate the machine but preserve its public
// application state. Keep their entries in proofs; unwrap only for reading it.
func application(m replica.Machine) replica.Machine {
	for {
		if scheduled, ok := m.(*consensus.Scheduled); ok {
			m = scheduled.Base
		} else {
			return m
		}
	}
}

func Load(home, lanes, transport string, port int, logger log.Logger) (*Host, error) {
	raw, e := os.ReadFile(filepath.Join(home, "network.json"))
	if e != nil {
		return nil, e
	}
	m, e := ParseManifest(raw)
	if e != nil {
		return nil, e
	}
	h := &Host{Home: home, Manifest: m, Readers: map[string]*Reader{}, Native: []*Native{}}
	selected := []Chain{m.Main}
	seen := map[uint64]bool{}
	if lanes != "" {
		for _, s := range strings.Split(lanes, ",") {
			n, e := codec.Uint(s)
			if e != nil || n >= uint64(len(m.Lanes)) || seen[n] {
				return nil, fmt.Errorf("selected lane")
			}
			seen[n] = true
			selected = append(selected, m.Lanes[n])
		}
	}
	objects := archive.Store{Directory: home}
	for i, c := range selected {
		raw, e := objects.Get(c.Genesis)
		if e != nil {
			if transport == "offline" {
				h.Close()
				return nil, e
			}
			raw, e = FetchGenesis(c)
			if e != nil {
				h.Close()
				return nil, e
			}
			if _, e = objects.Put(raw); e != nil {
				h.Close()
				return nil, e
			}
		}
		g, e := m.CheckGenesis(c, raw)
		if e != nil {
			h.Close()
			return nil, e
		}
		var source chain.Provider
		var journal string
		switch transport {
		case "offline":
			source = nil
		case "http":
			source = &HTTPProvider{Genesis: g, Descriptor: c, Client: Client()}
		case "p2p":
			native, e := StartNative(filepath.Join(home, "chains", c.ChainID), g, c.Peers, port+2*i, logger)
			if e != nil {
				h.Close()
				return nil, e
			}
			h.Native = append(h.Native, native)
			source = native
			journal = filepath.Join(home, "chains", c.ChainID, "data", "application")
		default:
			h.Close()
			return nil, fmt.Errorf("transport must be p2p or http")
		}
		reader, e := NewReader(g, source, journal, filepath.Join(home, "index", c.ChainID))
		if e != nil {
			h.Close()
			return nil, e
		}
		reader.Objects = objects
		reader.ArchiveDirectory = filepath.Join(home, "archives", c.ChainID)
		h.Readers[c.ChainID] = reader
	}
	h.API = &API{Manifest: m, Readers: h.Readers, Objects: objects, Submit: func(id string, b []byte) error {
		for _, c := range selected {
			if c.ChainID == id {
				return Broadcast(c, b)
			}
		}
		return fmt.Errorf("chain unavailable")
	}}
	h.API.Extra = h.Extra
	h.API.Enqueue = func(id string, raw []byte) error {
		h.Services.mu.Lock()
		defer h.Services.mu.Unlock()
		return h.persistJob(id, Job{m.Main.ChainID, base64.RawURLEncoding.EncodeToString(raw), "transaction"})
	}
	h.API.Pending = func(id string) bool {
		if _, e := objects.Path(id); e != nil {
			return false
		}
		_, e := os.Stat(filepath.Join(home, "relay", id+".json"))
		return e == nil
	}
	return h, nil
}
func (h *Host) Refresh() error {
	for _, c := range append([]Chain{h.Manifest.Main}, h.Manifest.Lanes...) {
		if r := h.Readers[c.ChainID]; r != nil {
			if e := r.Refresh(); e != nil {
				return e
			}
		}
	}
	return nil
}
func (h *Host) Close() {
	h.closeOnce.Do(func() {
		for _, n := range h.Native {
			n.Close()
		}
		for _, r := range h.Readers {
			_ = r.Close()
		}
	})
}
func (h *Host) Verify() error {
	for _, r := range h.Readers {
		v, e := chain.New(r.Genesis)
		if e != nil {
			return e
		}
		for height := uint64(1); height <= r.Source.Height(); height++ {
			b, e := r.Source.Bundle(height)
			if e != nil {
				return e
			}
			if e = v.Apply(b); e != nil {
				return fmt.Errorf("chain %s height %s: %w", r.Genesis.Doc.ChainID, strconv.FormatUint(height, 10), e)
			}
		}
	}
	return nil
}
