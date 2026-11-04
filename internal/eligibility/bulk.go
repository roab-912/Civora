package eligibility

import (
	"bufio"
	"bytes"
	"civora/internal/codec"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
)

func (s *Store) openBase() error {
	size, _ := codec.Uint(s.base.Metadata.Size)
	if size == 0 {
		if len(s.base.Files) != 0 {
			return fmt.Errorf("ERR_BASE_FILES")
		}
		return nil
	}
	if len(s.base.Files) != depth(size)+1 {
		return fmt.Errorf("ERR_BASE_FILES")
	}
	for i, info := range s.base.Files {
		if info.Name != fmt.Sprintf("level-%02d.bin", i) || info.Size != strconv.FormatUint(width(size, i)*32, 10) {
			return fmt.Errorf("ERR_BASE_FILES")
		}
		f, e := os.Open(filepath.Join(s.dir, "base", info.Name))
		if e != nil {
			return e
		}
		s.files = append(s.files, f)
		h := sha256.New()
		n, e := io.Copy(h, f)
		if e != nil {
			return e
		}
		if strconv.FormatInt(n, 10) != info.Size || hex.EncodeToString(h.Sum(nil)) != info.SHA256 {
			return fmt.Errorf("ERR_BASE_HASH")
		}
	}
	return nil
}

func writeBaseManifest(dir string, manifest baseManifest) error {
	f, e := os.CreateTemp(dir, ".manifest-")
	if e != nil {
		return e
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	if _, e = f.Write(codec.Must(manifest)); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(temp, filepath.Join(dir, "base.json"))
}

// BuildStore initializes a large immutable base in O(n) hashes and bounded
// memory. A snapshot is published only after all level files are synchronized.
// It is for authenticated imports/test fixtures, not an HTTP write endpoint.
func BuildStore(dir string, settings Metadata, count uint64, leaf func(uint64) (Node, error), progress func(int, uint64)) (*Store, error) {
	d, e := codec.Uint(settings.CircuitDepth)
	if e != nil || d != 16 && d != 26 {
		return nil, fmt.Errorf("ERR_CIRCUIT_DEPTH")
	}
	if count == 0 || count > uint64(1)<<d {
		return nil, fmt.Errorf("ERR_CAPACITY")
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	for _, name := range []string{"base", "base.json", "versions.db"} {
		if _, e = os.Stat(filepath.Join(dir, name)); !os.IsNotExist(e) {
			return nil, fmt.Errorf("ERR_STORE_EXISTS")
		}
	}
	staging, e := os.MkdirTemp(dir, ".building-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(staging)
	manifest := baseManifest{Metadata: settings, Files: []baseFile{}}
	manifest.Metadata.Size = strconv.FormatUint(count, 10)
	manifest.Metadata.Active = manifest.Metadata.Size
	writeLevel := func(level int, produce func(io.Writer) error) error {
		path := filepath.Join(staging, fmt.Sprintf("level-%02d.bin", level))
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		defer f.Close()
		h := sha256.New()
		buf := bufio.NewWriterSize(io.MultiWriter(f, h), 1024*1024)
		if e = produce(buf); e != nil {
			return e
		}
		if e = buf.Flush(); e != nil {
			return e
		}
		if e = f.Sync(); e != nil {
			return e
		}
		manifest.Files = append(manifest.Files, baseFile{Name: filepath.Base(path), Size: strconv.FormatUint(width(count, level)*32, 10), SHA256: hex.EncodeToString(h.Sum(nil))})
		if progress != nil {
			progress(level, width(count, level))
		}
		return nil
	}
	if e = writeLevel(0, func(w io.Writer) error {
		var fieldLimit Node
		modulus.FillBytes(fieldLimit[:])
		for i := uint64(0); i < count; i++ {
			n, e := leaf(i)
			if e != nil {
				return e
			}
			if n == (Node{}) || bytes.Compare(n[:], fieldLimit[:]) >= 0 {
				return fmt.Errorf("ERR_ZERO_IMPORT")
			}
			if _, e = w.Write(n[:]); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		return nil, e
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > 8 {
		workers = 8
	}
	const chunkParents = 8192
	for level := 1; level <= depth(count); level++ {
		prev, e := os.Open(filepath.Join(staging, fmt.Sprintf("level-%02d.bin", level-1)))
		if e != nil {
			return nil, e
		}
		prevWidth := width(count, level-1)
		total := width(count, level)
		e = writeLevel(level, func(w io.Writer) error {
			for start := uint64(0); start < total; start += chunkParents {
				n := uint64(chunkParents)
				if total-start < n {
					n = total - start
				}
				children := 2 * n
				if 2*start+children > prevWidth {
					children = prevWidth - 2*start
				}
				input := make([]byte, children*32)
				if _, e := prev.ReadAt(input, int64(start*64)); e != nil {
					return e
				}
				output := make([]byte, n*32)
				var wg sync.WaitGroup
				for worker := 0; worker < workers; worker++ {
					wg.Add(1)
					go func(worker int) {
						defer wg.Done()
						for i := uint64(worker); i < n; i += uint64(workers) {
							var left, right Node
							copy(left[:], input[i*64:i*64+32])
							value := left
							if 2*i+1 < children {
								copy(right[:], input[i*64+32:i*64+64])
								value = Hash(left, right)
							}
							copy(output[i*32:i*32+32], value[:])
						}
					}(worker)
				}
				wg.Wait()
				if _, e := w.Write(output); e != nil {
					return e
				}
			}
			return nil
		})
		_ = prev.Close()
		if e != nil {
			return nil, e
		}
	}
	last, e := os.Open(filepath.Join(staging, fmt.Sprintf("level-%02d.bin", depth(count))))
	if e != nil {
		return nil, e
	}
	var root Node
	_, e = io.ReadFull(last, root[:])
	_ = last.Close()
	if e != nil {
		return nil, e
	}
	text := root.String()
	manifest.Metadata.Root = &text
	manifest.Metadata.SnapshotID = manifest.Metadata.ID()
	if e = manifest.Metadata.Validate(); e != nil {
		return nil, e
	}
	if e = os.Rename(staging, filepath.Join(dir, "base")); e != nil {
		return nil, e
	}
	// base.json is the publication marker. A crash before it leaves an
	// unreferenced base directory and never exposes a partially built tree.
	data := codec.Must(manifest)
	temp := filepath.Join(dir, "base.json.pending")
	f, e := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	if _, e = f.Write(data); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return nil, e
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if e = os.Rename(temp, filepath.Join(dir, "base.json")); e != nil {
		return nil, e
	}
	return OpenStore(dir, manifest.Metadata)
}

// BaseMetadata reads the committed domain reference without modifying a store.
func BaseMetadata(dir string) (Metadata, error) {
	raw, e := os.ReadFile(filepath.Join(dir, "base.json"))
	if e != nil {
		return Metadata{}, e
	}
	if _, e = codec.Parse(raw); e != nil {
		return Metadata{}, e
	}
	var m baseManifest
	if e = json.Unmarshal(raw, &m); e != nil {
		return Metadata{}, e
	}
	return m.Metadata, m.Metadata.Validate()
}
