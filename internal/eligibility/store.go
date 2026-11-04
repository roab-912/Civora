package eligibility

import (
	"bytes"
	"civora/internal/codec"
	"encoding/binary"
	"encoding/json"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Store retains immutable base levels and MVCC node deltas. All changes and
// the snapshot cursor commit in one bbolt transaction. It is an eligibility
// index; its root must be compared to an authenticated ledger reference.
type Store struct {
	db    *bolt.DB
	dir   string
	base  baseManifest
	files []*os.File
	hook  func(string)
}

var bucketNodes = []byte("nodes")
var bucketMeta = []byte("snapshots")
var latestKey = []byte("latest")

func u64(n uint64) []byte { var b [8]byte; binary.BigEndian.PutUint64(b[:], n); return b[:] }
func nodeKey(level int, index, height uint64) []byte {
	b := make([]byte, 17)
	b[0] = byte(level)
	binary.BigEndian.PutUint64(b[1:9], index)
	binary.BigEndian.PutUint64(b[9:], height)
	return b
}

type baseFile struct {
	Name   string `json:"name"`
	Size   string `json:"size"`
	SHA256 string `json:"sha256"`
}
type baseManifest struct {
	Metadata Metadata   `json:"metadata"`
	Files    []baseFile `json:"files"`
}

func OpenStore(dir string, initial Metadata) (*Store, error) {
	if e := initial.Validate(); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	s := &Store{dir: dir}
	raw, e := os.ReadFile(filepath.Join(dir, "base.json"))
	if e == nil {
		if _, e = codec.Parse(raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &s.base); e != nil {
			return nil, e
		}
		if e = s.base.Metadata.Validate(); e != nil {
			return nil, e
		}
		a, b := s.base.Metadata, initial
		if a.NetworkID != b.NetworkID || a.Epoch != b.Epoch || a.TreeID != b.TreeID || a.SuiteID != b.SuiteID || a.CircuitDepth != b.CircuitDepth {
			return nil, fmt.Errorf("ERR_TREE_DOMAIN")
		}
		if e = s.openBase(); e != nil {
			s.Close()
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	} else {
		if initial.Size != "0" || initial.Root != nil {
			return nil, fmt.Errorf("ERR_BASE_MISSING")
		}
		s.base = baseManifest{Metadata: initial, Files: []baseFile{}}
		if e = writeBaseManifest(dir, s.base); e != nil {
			return nil, e
		}
	}
	db, e := bolt.Open(filepath.Join(dir, "versions.db"), 0600, &bolt.Options{Timeout: 2 * time.Second})
	if e != nil {
		s.Close()
		return nil, e
	}
	s.db = db
	e = db.Update(func(tx *bolt.Tx) error {
		if _, e := tx.CreateBucketIfNotExists(bucketNodes); e != nil {
			return e
		}
		b, e := tx.CreateBucketIfNotExists(bucketMeta)
		if e != nil {
			return e
		}
		if b.Get(latestKey) == nil {
			height, _ := codec.Uint(s.base.Metadata.Height)
			if e = b.Put(u64(height), codec.Must(s.base.Metadata)); e != nil {
				return e
			}
			return b.Put(latestKey, u64(height))
		}
		return nil
	})
	if e != nil {
		s.Close()
		return nil, e
	}
	last, e := s.Latest()
	if e != nil {
		s.Close()
		return nil, e
	}
	if last.NetworkID != initial.NetworkID || last.Epoch != initial.Epoch || last.TreeID != initial.TreeID || last.SuiteID != initial.SuiteID || last.CircuitDepth != initial.CircuitDepth {
		s.Close()
		return nil, fmt.Errorf("ERR_TREE_DOMAIN")
	}
	return s, nil
}
func (s *Store) Close() error {
	var e error
	if s.db != nil {
		e = s.db.Close()
	}
	for _, f := range s.files {
		_ = f.Close()
	}
	return e
}
func (s *Store) readMeta(tx *bolt.Tx, height uint64) (Metadata, error) {
	var m Metadata
	raw := tx.Bucket(bucketMeta).Get(u64(height))
	if raw == nil {
		return m, fmt.Errorf("ERR_SNAPSHOT_NOT_FOUND")
	}
	if e := json.Unmarshal(raw, &m); e != nil {
		return m, e
	}
	if e := m.Validate(); e != nil {
		return m, e
	}
	if m.Height != strconv.FormatUint(height, 10) {
		return m, fmt.Errorf("ERR_SNAPSHOT_HEIGHT")
	}
	return m, nil
}
func (s *Store) Latest() (Metadata, error) {
	var m Metadata
	e := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMeta).Get(latestKey)
		if len(b) != 8 {
			return fmt.Errorf("ERR_SNAPSHOT_CURSOR")
		}
		var e error
		m, e = s.readMeta(tx, binary.BigEndian.Uint64(b))
		return e
	})
	return m, e
}
func (s *Store) Snapshot(height uint64) (Metadata, error) {
	var m Metadata
	e := s.db.View(func(tx *bolt.Tx) error { var e error; m, e = s.readMeta(tx, height); return e })
	return m, e
}
func (s *Store) get(tx *bolt.Tx, level int, index, height uint64) (Node, error) {
	key := nodeKey(level, index, height)
	cursor := tx.Bucket(bucketNodes).Cursor()
	k, v := cursor.Seek(key)
	if k == nil || !bytes.Equal(k, key) {
		k, v = cursor.Prev()
	}
	if len(k) == 17 && bytes.Equal(k[:9], key[:9]) {
		if len(v) != 32 {
			return Node{}, fmt.Errorf("ERR_NODE_SIZE")
		}
		var n Node
		copy(n[:], v)
		return n, nil
	}
	baseSize, _ := codec.Uint(s.base.Metadata.Size)
	baseHeight, _ := codec.Uint(s.base.Metadata.Height)
	if height < baseHeight || baseSize == 0 || level >= len(s.files) || index >= width(baseSize, level) {
		return Node{}, fmt.Errorf("ERR_NODE_NOT_FOUND")
	}
	var n Node
	_, e := s.files[level].ReadAt(n[:], int64(index*32))
	return n, e
}
func (s *Store) Path(height, index uint64) (Path, error) {
	var p Path
	e := s.db.View(func(tx *bolt.Tx) error {
		m, e := s.readMeta(tx, height)
		if e != nil {
			return e
		}
		p, e = makePath(m, index, func(level int, index uint64) (Node, error) { return s.get(tx, level, index, height) })
		return e
	})
	return p, e
}

type Change struct {
	Operation  string `json:"operation"`
	Index      string `json:"index"`
	Commitment string `json:"commitment"`
}

func (s *Store) Commit(height uint64, changes []Change) (Metadata, error) {
	return s.CommitExpected(height, changes, nil)
}

// CommitExpected prevents a derived index from publishing a root that differs
// from the already verified ledger snapshot. A mismatch aborts the transaction.
func (s *Store) CommitExpected(height uint64, changes []Change, expected *Metadata) (Metadata, error) {
	var result Metadata
	e := s.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		last := binary.BigEndian.Uint64(meta.Get(latestKey))
		if height <= last {
			return fmt.Errorf("ERR_HEIGHT")
		}
		m, e := s.readMeta(tx, last)
		if e != nil {
			return e
		}
		size, _ := codec.Uint(m.Size)
		active, _ := codec.Uint(m.Active)
		circuit, _ := codec.Uint(m.CircuitDepth)
		nodes := tx.Bucket(bucketNodes)
		for _, change := range changes {
			index, e := codec.Uint(change.Index)
			if e != nil {
				return e
			}
			n, e := ParseField(change.Commitment, change.Operation == "append")
			if e != nil {
				return e
			}
			switch change.Operation {
			case "append":
				if index != size {
					return fmt.Errorf("ERR_INDEX")
				}
				if size == uint64(1)<<circuit {
					return fmt.Errorf("ERR_CAPACITY")
				}
				size++
				active++
			case "update":
				if index >= size {
					return fmt.Errorf("ERR_INDEX")
				}
				old, e := s.get(tx, 0, index, height)
				if e != nil {
					return e
				}
				if old == (Node{}) && n != (Node{}) {
					return fmt.Errorf("ERR_RETIRED_POSITION")
				}
				if old != (Node{}) && n == (Node{}) {
					active--
				}
			default:
				return fmt.Errorf("ERR_OPERATION")
			}
			// bbolt retains value slices until commit; parent hashing reuses n.
			// Each persisted level must own its bytes throughout the transaction.
			if e = nodes.Put(nodeKey(0, index, height), append([]byte(nil), n[:]...)); e != nil {
				return e
			}
			for level := 1; level <= depth(size); level++ {
				index /= 2
				left, e := s.get(tx, level-1, index*2, height)
				if e != nil {
					return e
				}
				n = left
				if index*2+1 < width(size, level-1) {
					right, e := s.get(tx, level-1, index*2+1, height)
					if e != nil {
						return e
					}
					n = Hash(left, right)
				}
				if e = nodes.Put(nodeKey(level, index, height), append([]byte(nil), n[:]...)); e != nil {
					return e
				}
			}
		}
		m.Height = strconv.FormatUint(height, 10)
		if s.hook != nil {
			s.hook("after_nodes")
		}
		m.Size = strconv.FormatUint(size, 10)
		m.Active = strconv.FormatUint(active, 10)
		if size == 0 {
			m.Root = nil
		} else {
			root, e := s.get(tx, depth(size), 0, height)
			if e != nil {
				return e
			}
			text := root.String()
			m.Root = &text
		}
		m.SnapshotID = m.ID()
		if expected != nil && string(codec.Must(m)) != string(codec.Must(*expected)) {
			return fmt.Errorf("ERR_LEDGER_ROOT_MISMATCH")
		}
		if e = meta.Put(u64(height), codec.Must(m)); e != nil {
			return e
		}
		if s.hook != nil {
			s.hook("before_cursor")
		}
		if e = meta.Put(latestKey, u64(height)); e != nil {
			return e
		}
		result = m
		return nil
	})
	if e == nil && s.hook != nil {
		s.hook("after_commit")
	}
	return result, e
}
