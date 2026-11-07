package recovery

import (
	"civora/internal/codec"
	"encoding/json"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type State struct {
	RootID   string            `json:"root_sha3_384"`
	Counter  string            `json:"last_counter"`
	Observed string            `json:"observed_at_ms"`
	Orders   []Order           `json:"orders"`
	ReadOnly bool              `json:"read_only"`
	Counters map[string]string `json:"counters_by_recovery_epoch"`
}
type Store struct {
	db        *bolt.DB
	root      Root
	allowTest bool
	mu        sync.Mutex
}

func Open(path string, r Root, now uint64, allowTest bool) (*Store, error) {
	if err := r.Validate(now, allowTest); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, root: r, allowTest: allowTest}
	err = db.Update(func(tx *bolt.Tx) error {
		b, e := tx.CreateBucketIfNotExists([]byte("recovery"))
		if e != nil {
			return e
		}
		if raw := b.Get([]byte("state")); raw != nil {
			var v State
			if e = json.Unmarshal(raw, &v); e != nil {
				return e
			}
			if v.RootID != RootID(r) {
				return fmt.Errorf("ERR_RECOVERY_ROOT_PIN")
			}
			return nil
		}
		return b.Put([]byte("state"), codec.Must(State{RootID: RootID(r), Counter: "0", Observed: Number(now), Orders: []Order{}, ReadOnly: true, Counters: map[string]string{r.Epoch: "0"}}))
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Snapshot() (State, error) {
	var v State
	err := s.db.View(func(tx *bolt.Tx) error { return json.Unmarshal(tx.Bucket([]byte("recovery")).Get([]byte("state")), &v) })
	return v, err
}
func (s *Store) Apply(b Bundle, now uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("recovery"))
		var v State
		if err := json.Unmarshal(bucket.Get([]byte("state")), &v); err != nil {
			return err
		}
		observed, e := codec.Uint(v.Observed)
		if e != nil {
			return e
		}
		last, e := codec.Uint(v.Counter)
		if e != nil {
			return e
		}
		if now < observed {
			return fmt.Errorf("ERR_RECOVERY_CLOCK_ROLLBACK")
		}
		if err := Verify(s.root, b, now, last, s.allowTest); err != nil {
			return err
		}
		// All off-chain orders only restrict writes or publish references. A hold
		// remains visible after expiry; this verifier never resumes an interrupted poll.
		v.Counter = b.Order.Counter
		v.Counters[s.root.Epoch] = v.Counter
		v.Observed = Number(now)
		v.ReadOnly = true
		v.Orders = append(v.Orders, b.Order)
		if len(v.Orders) > 10000 {
			return fmt.Errorf("ERR_RECOVERY_CAPACITY")
		}
		return bucket.Put([]byte("state"), codec.Must(v))
	})
}

// Rotate persists both configurations' authorization atomically. The caller
// must supply a verifier of the constitutional proof; the CLI has no bypass.
func (s *Store) Rotate(rotation Rotation, now uint64, approval ApprovalVerifier) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := VerifyRotation(s.root, rotation, now, s.allowTest, approval); err != nil {
		return err
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("recovery"))
		var v State
		if err := json.Unmarshal(bucket.Get([]byte("state")), &v); err != nil {
			return err
		}
		observed, e := codec.Uint(v.Observed)
		if e != nil {
			return e
		}
		if now < observed {
			return fmt.Errorf("ERR_RECOVERY_CLOCK_ROLLBACK")
		}
		if _, ok := v.Counters[rotation.Next.Epoch]; ok {
			return fmt.Errorf("ERR_RECOVERY_REPLAY")
		}
		v.RootID = RootID(rotation.Next)
		v.Counter = "0"
		v.Observed = Number(now)
		v.Counters[rotation.Next.Epoch] = "0"
		return bucket.Put([]byte("state"), codec.Must(v))
	})
	if err == nil {
		s.root = rotation.Next
	}
	return err
}
