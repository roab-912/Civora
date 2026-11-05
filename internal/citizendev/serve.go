package citizendev

import (
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/citizen"
	"civora/internal/codec"
	"fmt"
	"path/filepath"
)

type Provider struct{ Blocks []chain.Bundle }

func (p Provider) Height() uint64 { return uint64(len(p.Blocks)) }
func (p Provider) Bundle(h uint64) (chain.Bundle, error) {
	if h == 0 || h > uint64(len(p.Blocks)) {
		return chain.Bundle{}, fmt.Errorf("block unavailable")
	}
	return p.Blocks[h-1], nil
}
func Serve(f Fixture, home string) (*citizen.Host, error) {
	h := &citizen.Host{Home: home, Manifest: f.Manifest, Readers: map[string]*citizen.Reader{}}
	store := archive.Store{Directory: home}
	for _, run := range f.Runs {
		raw, e := codec.Blob(run.Genesis)
		if e != nil {
			return nil, e
		}
		g, e := chain.ParseGenesis(raw)
		if e != nil {
			return nil, e
		}
		if _, e = store.Put(raw); e != nil {
			return nil, e
		}
		reader, e := citizen.NewReader(g, Provider{run.Blocks}, "", filepath.Join(home, "index", run.ChainID))
		if e != nil {
			h.Close()
			return nil, e
		}
		reader.Objects = store
		reader.ArchiveDirectory = filepath.Join(home, "archives", run.ChainID)
		h.Readers[run.ChainID] = reader
	}
	h.API = &citizen.API{Manifest: f.Manifest, Readers: h.Readers, Objects: store}
	h.API.Extra = h.Extra
	if e := h.Refresh(); e != nil {
		h.Close()
		return nil, e
	}
	return h, nil
}
