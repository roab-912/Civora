// Package distribution verifies a local, untrusted mirror. It never installs or runs targets.
package distribution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/config"
	"github.com/theupdateframework/go-tuf/v2/metadata/updater"
	bolt "go.etcd.io/bbolt"
)

const TestMode = "development_TEST_ONLY"
const modeField = "civora_distribution"

var filesBucket = []byte("metadata")
var stateBucket = []byte("state")

type Options struct {
	Repository, RootFile, RootSHA256, StateFile string
	AllowTestKeys                               bool
}

type VerifiedTarget struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Report struct {
	Mode               string           `json:"mode"`
	RootVersion        int64            `json:"root_version"`
	TargetsVersion     int64            `json:"targets_version"`
	Targets            []VerifiedTarget `json:"targets"`
	ProtocolActivation bool             `json:"protocol_activation"`
}

// Verify uses the installed clock. Tests inject a clock through the unexported implementation.
func Verify(o Options) (*Report, error) { return verify(o, time.Now().UTC(), nil) }

func verify(o Options, now time.Time, checkpoint func()) (*Report, error) {
	root, err := os.ReadFile(o.RootFile)
	if err != nil {
		return nil, err
	}
	pin, err := hex.DecodeString(o.RootSHA256)
	if err != nil || len(pin) != sha256.Size {
		return nil, errors.New("an independently obtained SHA-256 root pin is required")
	}
	h := sha256.Sum256(root)
	if !strings.EqualFold(hex.EncodeToString(h[:]), o.RootSHA256) {
		return nil, errors.New("installed root pin mismatch")
	}
	if err = checkRoot(root, o.AllowTestKeys); err != nil {
		return nil, err
	}
	if o.StateFile == "" {
		return nil, errors.New("persistent verifier state is required")
	}
	if err = os.MkdirAll(filepath.Dir(o.StateFile), 0700); err != nil {
		return nil, err
	}
	db, err := bolt.Open(o.StateFile, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	defer db.Close()
	cache, err := os.MkdirTemp(filepath.Dir(o.StateFile), ".civdist-cache-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(cache) // Only this freshly created, private scratch directory.
	err = db.Update(func(tx *bolt.Tx) error {
		s, e := tx.CreateBucketIfNotExists(stateBucket)
		if e != nil {
			return e
		}
		if p := s.Get([]byte("pin")); p != nil && string(p) != strings.ToLower(o.RootSHA256) {
			return errors.New("state belongs to a different trust anchor")
		}
		if prior := s.Get([]byte("clock")); prior != nil {
			t, e := time.Parse(time.RFC3339Nano, string(prior))
			if e != nil {
				return e
			}
			if now.Before(t) {
				return errors.New("clock rollback refused")
			}
		}
		if e = s.Put([]byte("pin"), []byte(strings.ToLower(o.RootSHA256))); e != nil {
			return e
		}
		if e = s.Put([]byte("clock"), []byte(now.Format(time.RFC3339Nano))); e != nil {
			return e
		}
		b, e := tx.CreateBucketIfNotExists(filesBucket)
		if e != nil {
			return e
		}
		return b.ForEach(func(k, v []byte) error {
			if !safeName(string(k)) || strings.Contains(string(k), "/") {
				return errors.New("invalid cached name")
			}
			return os.WriteFile(filepath.Join(cache, string(k)), v, 0600)
		})
	})
	if err != nil {
		return nil, err
	}
	if cached, e := os.ReadFile(filepath.Join(cache, "root.json")); e == nil {
		root = cached
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	if err = checkRoot(root, o.AllowTestKeys); err != nil {
		return nil, err
	}
	var u *updater.Updater
	persist := func() error {
		// go-tuf retains authenticated intermediate timestamp/snapshot versions even
		// when final freshness checks fail. Preserve these rollback bounds as well.
		if u != nil {
			t := u.GetTrustedMetadataSet()
			accepted := map[string]interface{ ToBytes(bool) ([]byte, error) }{"root": t.Root}
			if t.Timestamp != nil {
				accepted["timestamp"] = t.Timestamp
			}
			if t.Snapshot != nil {
				accepted["snapshot"] = t.Snapshot
			}
			if t.Targets[metadata.TARGETS] != nil {
				accepted["targets"] = t.Targets[metadata.TARGETS]
			}
			for name, m := range accepted {
				raw, e := m.ToBytes(false)
				if e != nil {
					return e
				}
				var next, prior struct {
					Signed struct {
						Version int64 `json:"version"`
					} `json:"signed"`
				}
				if e = json.Unmarshal(raw, &next); e != nil {
					return e
				}
				file := filepath.Join(cache, name+".json")
				old, e := os.ReadFile(file)
				if e != nil && !errors.Is(e, os.ErrNotExist) {
					return e
				}
				if e == nil {
					if e = json.Unmarshal(old, &prior); e != nil {
						return e
					}
				}
				if next.Signed.Version > prior.Signed.Version {
					if e = os.WriteFile(file, raw, 0600); e != nil {
						return e
					}
				}
			}
		}
		entries, e := os.ReadDir(cache)
		if e != nil {
			return e
		}
		return db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(filesBucket)
			for _, f := range entries {
				// The upstream updater writes temporary files before atomic rename.
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
					continue
				}
				data, e := os.ReadFile(filepath.Join(cache, f.Name()))
				if e != nil {
					return e
				}
				if !json.Valid(data) {
					return errors.New("corrupt metadata cache")
				}
				if e = b.Put([]byte(f.Name()), data); e != nil {
					return e
				}
			}
			return nil
		})
	}
	cfg, err := config.New("https://mirror.invalid/metadata/", root)
	if err != nil {
		return nil, err
	}
	cfg.LocalMetadataDir = cache
	cfg.LocalTargetsDir = filepath.Join(cache, "targets")
	cfg.Fetcher = &mirrorFetcher{repository: o.Repository, persist: persist, allowTest: o.AllowTestKeys, checkpoint: checkpoint}
	u, err = updater.New(cfg)
	if err != nil {
		return nil, err
	}
	// Fixed once per refresh, as required by the TUF workflow; never configurable in the CLI.
	u.UnsafeSetRefTime(now)
	refreshErr := u.Refresh()
	if err = persist(); err != nil {
		return nil, err
	}
	if refreshErr != nil {
		return nil, refreshErr
	}
	trusted := u.GetTrustedMetadataSet()
	if trusted.Targets[metadata.TARGETS].Signed.Delegations != nil {
		return nil, errors.New("delegations are outside this release profile")
	}
	report := &Report{Mode: rootMode(trusted.Root), RootVersion: trusted.Root.Signed.Version, TargetsVersion: trusted.Targets[metadata.TARGETS].Signed.Version, Targets: []VerifiedTarget{}}
	for name, target := range u.GetTopLevelTargets() {
		if !safeName(name) || len(target.Hashes) != 1 || len(target.Hashes["sha256"]) != 32 || target.Length < 0 || target.Length > 8<<30 {
			return nil, errors.New("unsupported target path, hash or size")
		}
		// Consistent snapshots protect metadata; payload names are explicitly selected by their signed SHA-256.
		payload := path.Join("targets", path.Dir(name), hex.EncodeToString(target.Hashes["sha256"])+"."+path.Base(name))
		f, e := openMirror(o.Repository, payload)
		if e != nil {
			return nil, e
		}
		hasher := sha256.New()
		n, e := io.Copy(hasher, io.LimitReader(f, target.Length+1))
		f.Close()
		if e != nil {
			return nil, e
		}
		if n != target.Length || !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), hex.EncodeToString(target.Hashes["sha256"])) {
			return nil, fmt.Errorf("target integrity failed: %s", name)
		}
		report.Targets = append(report.Targets, VerifiedTarget{Path: name, SHA256: hex.EncodeToString(target.Hashes["sha256"]), Size: n})
	}
	if len(report.Targets) == 0 {
		return nil, errors.New("empty distribution")
	}
	sort.Slice(report.Targets, func(i, j int) bool { return report.Targets[i].Path < report.Targets[j].Path })
	return report, nil
}

func rootMode(r *metadata.Metadata[metadata.RootType]) string {
	s, _ := r.Signed.UnrecognizedFields[modeField].(string)
	return s
}

func checkRoot(data []byte, allowTest bool) error {
	r, err := metadata.Root().FromBytes(data)
	if err != nil {
		return err
	}
	mode := rootMode(r)
	if mode != "public" && mode != TestMode {
		return errors.New("unknown distribution mode")
	}
	if mode == TestMode && !allowTest {
		return errors.New("PUBLIC TEST KEYS refused: explicitly use --allow-test-keys for local exercises")
	}
	if !r.Signed.ConsistentSnapshot || len(r.Signed.Roles) != 4 {
		return errors.New("unsupported root profile")
	}
	seen := map[string]bool{}
	for _, name := range []string{"root", "targets", "snapshot", "timestamp"} {
		count, threshold := 1, 1
		if name == "root" || name == "targets" {
			count, threshold = 5, 3
		}
		role := r.Signed.Roles[name]
		if role == nil || role.Threshold != threshold || len(role.KeyIDs) != count {
			return fmt.Errorf("%s must have threshold %d/%d", name, threshold, count)
		}
		for _, id := range role.KeyIDs {
			key := r.Signed.Keys[id]
			if key == nil || key.Type != "ed25519" || key.Scheme != "ed25519" {
				return errors.New("roles require distinct Ed25519 public keys")
			}
			public, decodeErr := hex.DecodeString(key.Value.PublicKey)
			if decodeErr != nil || len(public) != 32 || seen[hex.EncodeToString(public)] {
				return errors.New("roles require distinct Ed25519 public keys")
			}
			if !allowTest && isTestPublicKey(hex.EncodeToString(public)) {
				return errors.New("known PUBLIC TEST KEY refused")
			}
			seen[hex.EncodeToString(public)] = true
		}
	}
	return nil
}

type mirrorFetcher struct {
	repository string
	persist    func() error
	allowTest  bool
	checkpoint func()
}

func (f *mirrorFetcher) DownloadFile(raw string, max int64, _ time.Duration) ([]byte, error) {
	// The previous validated metadata is committed before the next download. An abrupt
	// process exit cannot discard an acknowledged version or leave half a DB transaction.
	if err := f.persist(); err != nil {
		return nil, err
	}
	if f.checkpoint != nil {
		f.checkpoint()
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host != "mirror.invalid" || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/metadata/") {
		return nil, errors.New("invalid mirror URL")
	}
	name := strings.TrimPrefix(u.Path, "/")
	// A compromised online timestamp key must not remove the local resource limits
	// by declaring an enormous signed snapshot length.
	limit := int64(5_000_000)
	if strings.HasSuffix(name, ".root.json") {
		limit = 512_000
	}
	if strings.HasSuffix(name, ".snapshot.json") {
		limit = 2_000_000
	}
	if name == "metadata/timestamp.json" {
		limit = 16_384
	}
	if max <= 0 || max > limit {
		return nil, errors.New("metadata length exceeds local profile limit")
	}
	file, err := openMirror(f.repository, name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, &metadata.ErrDownloadHTTP{StatusCode: 404, URL: raw}
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errors.New("metadata size limit exceeded")
	}
	if strings.HasSuffix(name, ".root.json") {
		if err = checkRoot(data, f.allowTest); err != nil {
			return nil, err
		}
	}
	return data, nil
}

func safeName(name string) bool {
	if name == "" || path.Clean(name) != name || strings.HasPrefix(name, "/") {
		return false
	}
	for _, p := range strings.Split(name, "/") {
		if p == ".." || p == "." || strings.HasSuffix(p, ".") {
			return false
		}
		for _, c := range p {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
				return false
			}
		}
	}
	return true
}

func openMirror(base, name string) (*os.File, error) {
	if !safeName(name) {
		return nil, errors.New("unsafe mirror path")
	}
	// os.OpenRoot confines resolution even if the mirror is concurrently replaced.
	r, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.Open(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("mirror entries must be regular files")
	}
	return f, nil
}
