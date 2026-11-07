package distribution

import (
	"archive/zip"
	"crypto"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

// These seeds are PUBLIC TEST MATERIAL, never an operator's signing keys.
func testKey(role string, seat int) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte(fmt.Sprintf("CIVORA/DISTRIBUTION/PUBLIC_TEST_ONLY/1/%s/%d", role, seat)))
	return ed25519.NewKeyFromSeed(seed[:])
}

func isTestPublicKey(pub string) bool {
	for _, role := range []string{"root", "targets", "snapshot", "timestamp"} {
		for seat := 0; seat < 5; seat++ {
			if hex.EncodeToString(testKey(role, seat).Public().(ed25519.PublicKey)) == pub {
				return true
			}
		}
	}
	return false
}

func testRoot(epoch time.Time, version int64, offset int) (*metadata.Metadata[metadata.RootType], error) {
	r := metadata.Root(epoch.AddDate(1, 0, 0))
	r.Signed.ConsistentSnapshot = true
	r.Signed.Version = version
	r.Signed.UnrecognizedFields = map[string]any{modeField: TestMode}
	for _, role := range []string{"root", "targets", "snapshot", "timestamp"} {
		count, threshold := 1, 1
		if role == "root" || role == "targets" {
			count, threshold = 5, 3
		}
		for i := 0; i < count; i++ {
			key, err := metadata.KeyFromPublicKey(testKey(role, i+offset).Public())
			if err != nil {
				return nil, err
			}
			if err = r.Signed.AddKey(key, role); err != nil {
				return nil, err
			}
		}
		r.Signed.Roles[role].Threshold = threshold
	}
	return r, nil
}

func sign[T metadata.Roles](m *metadata.Metadata[T], role string, offset, count int) error {
	for i := 0; i < count; i++ {
		s, err := signature.LoadSigner(testKey(role, i+offset), crypto.Hash(0))
		if err != nil {
			return err
		}
		if _, err = m.Sign(s); err != nil {
			return err
		}
	}
	return nil
}

// SealTest creates an explicitly unsafe local mirror for repeatable distribution exercises.
// It is intentionally incapable of signing a public release.
func SealTest(payload, repository string, epoch time.Time, version int64) error {
	if epoch.IsZero() || version < 1 {
		return errors.New("explicit epoch and positive metadata version required")
	}
	if _, err := os.Stat(repository); !errors.Is(err, os.ErrNotExist) {
		return errors.New("repository destination must not exist")
	}
	if err := os.MkdirAll(filepath.Join(repository, "metadata"), 0700); err != nil {
		return err
	}
	r, err := testRoot(epoch, 1, 0)
	if err != nil {
		return err
	}
	if err = sign(r, "root", 0, 3); err != nil {
		return err
	}
	raw, err := r.ToBytes(false)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(repository, "metadata", "1.root.json"), raw, 0644); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(repository, "root.TEST_ONLY.json"), raw, 0644); err != nil {
		return err
	}
	targets := metadata.Targets(epoch.AddDate(0, 0, 7))
	targets.Signed.Version = version
	err = filepath.WalkDir(payload, func(file string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("payload contains a link or special file")
		}
		name, e := filepath.Rel(payload, file)
		if e != nil {
			return e
		}
		name = filepath.ToSlash(name)
		if !safeName(name) {
			return errors.New("unsafe payload name")
		}
		target, e := metadata.TargetFile().FromFile(file, "sha256")
		if e != nil {
			return e
		}
		targets.Signed.Targets[name] = target
		dst := filepath.Join(repository, "targets", filepath.Dir(name), hex.EncodeToString(target.Hashes["sha256"])+"."+filepath.Base(name))
		if e = os.MkdirAll(filepath.Dir(dst), 0755); e != nil {
			return e
		}
		in, e := os.Open(file)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.Create(dst)
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		closeErr := out.Close()
		return errors.Join(e, closeErr)
	})
	if err != nil {
		return err
	}
	if len(targets.Signed.Targets) == 0 {
		return errors.New("empty payload")
	}
	if err = sign(targets, "targets", 0, 3); err != nil {
		return err
	}
	raw, err = targets.ToBytes(false)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(repository, "metadata", fmt.Sprintf("%d.targets.json", version)), raw, 0644); err != nil {
		return err
	}
	snapshot := metadata.Snapshot(epoch.AddDate(0, 0, 7))
	snapshot.Signed.Version = version
	snapshot.Signed.Meta["targets.json"] = metaInfo(version, raw)
	if err = sign(snapshot, "snapshot", 0, 1); err != nil {
		return err
	}
	raw, err = snapshot.ToBytes(false)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(repository, "metadata", fmt.Sprintf("%d.snapshot.json", version)), raw, 0644); err != nil {
		return err
	}
	timestamp := metadata.Timestamp(epoch.Add(24 * time.Hour))
	timestamp.Signed.Version = version
	timestamp.Signed.Meta["snapshot.json"] = metaInfo(version, raw)
	if err = sign(timestamp, "timestamp", 0, 1); err != nil {
		return err
	}
	raw, err = timestamp.ToBytes(false)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(repository, "metadata", "timestamp.json"), raw, 0644)
}

func metaInfo(version int64, raw []byte) *metadata.MetaFiles {
	h := sha256.Sum256(raw)
	return &metadata.MetaFiles{Version: version, Length: int64(len(raw)), Hashes: metadata.Hashes{"sha256": h[:]}}
}

// Archive uses a fixed timestamp, sorted paths and stable modes. It never extracts files.
func Archive(source, destination string) (err error) {
	base, e := filepath.Abs(source)
	if e != nil {
		return e
	}
	dest, e := filepath.Abs(destination)
	if e != nil {
		return e
	}
	rel, e := filepath.Rel(base, dest)
	if e != nil {
		return e
	}
	if rel == "." || filepath.IsLocal(rel) {
		return errors.New("archive output must be outside its source directory")
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	z := zip.NewWriter(f)
	err = filepath.WalkDir(source, func(file string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("archives contain only regular files")
		}
		name, e := filepath.Rel(source, file)
		if e != nil {
			return e
		}
		name = filepath.ToSlash(name)
		if !safeName(name) {
			return errors.New("unsafe archive name")
		}
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)}
		h.SetMode(0644)
		if strings.HasPrefix(name, "bin/") && !strings.HasSuffix(name, ".exe") {
			h.SetMode(0755)
		}
		out, e := z.CreateHeader(h)
		if e != nil {
			return e
		}
		in, e := os.Open(file)
		if e != nil {
			return e
		}
		defer in.Close()
		_, e = io.Copy(out, in)
		return e
	})
	if err != nil {
		z.Close()
		return err
	}
	if err = z.Close(); err != nil {
		return err
	}
	return f.Sync()
}
