package citizen

import (
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/codec"
	"civora/internal/replica"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The verified block catalogue is part of the archive, separate from the
// disposable bbolt projection. Every restart still replays it from genesis.
type localSource struct {
	reader *Reader
	remote chain.Provider
}

func (s localSource) path(h uint64) string {
	return filepath.Join(s.reader.ArchiveDirectory, fmt.Sprintf("%020d.ref.json", h))
}
func (s localSource) Height() uint64 {
	files, _ := filepath.Glob(filepath.Join(s.reader.ArchiveDirectory, "*.ref.json"))
	height := uint64(0)
	for _, p := range files {
		n, e := codec.Uint(strings.TrimLeft(strings.TrimSuffix(filepath.Base(p), ".ref.json"), "0"))
		if e == nil && n > height {
			height = n
		}
	}
	if s.remote != nil {
		if n := s.remote.Height(); n > height {
			height = n
		}
	}
	return height
}
func (s localSource) Bundle(h uint64) (chain.Bundle, error) {
	raw, e := os.ReadFile(s.path(h))
	if e == nil {
		var ref archive.Ref
		if e = codec.Decode(raw, "archive_ref", &ref); e != nil {
			return chain.Bundle{}, e
		}
		raw, e = s.reader.Objects.Get(ref)
		if e != nil {
			return chain.Bundle{}, e
		}
		var bundle chain.Bundle
		e = codec.Decode(raw, "archive_block", &bundle)
		return bundle, e
	}
	if !os.IsNotExist(e) {
		return chain.Bundle{}, e
	}
	if s.remote == nil {
		return chain.Bundle{}, fmt.Errorf("offline block unavailable")
	}
	return s.remote.Bundle(h)
}
func (r *Reader) saveBlock(h uint64, ref archive.Ref) error {
	dir := r.ArchiveDirectory
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	p := localSource{reader: r}.path(h)
	raw := codec.Must(ref)
	if old, e := os.ReadFile(p); e == nil {
		if string(raw) != string(old) {
			return fmt.Errorf("immutable block catalogue mismatch")
		}
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	return replica.Persist(dir, p, raw, nil)
}
