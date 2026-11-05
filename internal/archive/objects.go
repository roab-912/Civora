// Package archive persists public objects by their SHA-256 content address.
package archive

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/replica"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const MaxObjectBytes = 32 * 1024 * 1024

type Ref struct {
	SHA256 string `json:"sha256"`
	Size   string `json:"size"`
}
type Store struct {
	Directory string
	Hook      func(string)
}

func Digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (s Store) Path(id string) (string, error) {
	b, e := hex.DecodeString(id)
	if e != nil || len(b) != 32 || hex.EncodeToString(b) != id {
		return "", fmt.Errorf("invalid object digest")
	}
	return filepath.Join(s.Directory, "objects", id[:2], id), nil
}
func (s Store) Put(b []byte) (Ref, error) {
	r := Ref{Digest(b), fmt.Sprint(len(b))}
	if len(b) > MaxObjectBytes {
		return r, fmt.Errorf("object capacity")
	}
	p, _ := s.Path(r.SHA256)
	if old, e := s.Get(r); e == nil {
		if !bytes.Equal(old, b) {
			return r, fmt.Errorf("object collision")
		}
		return r, nil
	} else if !os.IsNotExist(e) {
		return r, e
	}
	dir := filepath.Dir(p)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return r, e
	}
	if e := replica.Persist(dir, p, b, s.Hook); e != nil {
		if old, readErr := s.Get(r); readErr == nil && bytes.Equal(old, b) {
			return r, nil
		}
		return r, e
	}
	return r, nil
}
func (s Store) Get(r Ref) ([]byte, error) {
	size, e := codec.Uint(r.Size)
	if e != nil || size > MaxObjectBytes {
		return nil, fmt.Errorf("object size")
	}
	p, e := s.Path(r.SHA256)
	if e != nil {
		return nil, e
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, int64(size)+1))
	if e != nil {
		return nil, e
	}
	if uint64(len(b)) != size || Digest(b) != r.SHA256 {
		return nil, fmt.Errorf("object integrity")
	}
	return b, nil
}
