package distribution

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theupdateframework/go-tuf/v2/metadata"
)

var epoch = time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T, version int64) Options {
	t.Helper()
	dir := t.TempDir()
	payload := filepath.Join(dir, "payload")
	must(t, os.Mkdir(payload, 0700))
	must(t, os.WriteFile(filepath.Join(payload, "release.json"), []byte(`{"version":"1.0.0-rc.1","mode":"experimental"}`), 0644))
	repo := filepath.Join(dir, "mirror")
	must(t, SealTest(payload, repo, epoch, version))
	root := filepath.Join(repo, "root.TEST_ONLY.json")
	b, e := os.ReadFile(root)
	must(t, e)
	h := sha256.Sum256(b)
	return Options{Repository: repo, RootFile: root, RootSHA256: hex.EncodeToString(h[:]), StateFile: filepath.Join(dir, "state.db"), AllowTestKeys: true}
}
func accepts(t *testing.T, o Options) *Report {
	t.Helper()
	r, e := verify(o, epoch.Add(time.Hour), nil)
	must(t, e)
	return r
}
func rejects(t *testing.T, o Options, fragment string) {
	t.Helper()
	_, e := verify(o, epoch.Add(time.Hour), nil)
	if e == nil || fragment != "" && !strings.Contains(e.Error(), fragment) {
		t.Fatalf("expected refusal %q, got %v", fragment, e)
	}
}
func TestVerifyAndDefaults(t *testing.T) {
	o := fixture(t, 1)
	r := accepts(t, o)
	if r.Mode != TestMode || r.RootVersion != 1 || r.TargetsVersion != 1 || len(r.Targets) != 1 || r.ProtocolActivation {
		t.Fatal(r)
	}
	accepts(t, o)
	o.AllowTestKeys = false
	rejects(t, o, "TEST KEYS")
	o.AllowTestKeys = true
	o.RootSHA256 = strings.Repeat("0", 64)
	rejects(t, o, "pin mismatch")
}
func TestTargetsCorruptionAndRollback(t *testing.T) {
	o := fixture(t, 2)
	accepts(t, o)
	newer := fixture(t, 3)
	newer.StateFile = o.StateFile
	accepts(t, newer)
	rejects(t, o, "")
	must(t, filepath.WalkDir(filepath.Join(newer.Repository, "targets"), func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			return os.WriteFile(p, []byte("altered"), 0644)
		}
		return nil
	}))
	rejects(t, newer, "integrity")
	_, e := verify(newer, epoch.Add(30*time.Minute), nil)
	if e == nil || !strings.Contains(e.Error(), "clock rollback") {
		t.Fatal(e)
	}
}
func TestExpiration(t *testing.T) {
	o := fixture(t, 1)
	_, e := verify(o, epoch.Add(25*time.Hour), nil)
	if e == nil {
		t.Fatal("expired timestamp accepted")
	}
}

func TestExpiredAuthenticatedTimestampStillPreventsRollback(t *testing.T) {
	old := fixture(t, 1)
	accepts(t, old)
	newer := fixture(t, 2)
	newer.StateFile = old.StateFile
	file := filepath.Join(newer.Repository, "metadata", "timestamp.json")
	raw, e := os.ReadFile(file)
	must(t, e)
	timestamp, e := metadata.Timestamp().FromBytes(raw)
	must(t, e)
	timestamp.Signed.Expires = epoch.Add(30 * time.Minute)
	timestamp.ClearSignatures()
	must(t, sign(timestamp, "timestamp", 0, 1))
	raw, e = timestamp.ToBytes(false)
	must(t, e)
	must(t, os.WriteFile(file, raw, 0644))
	rejects(t, newer, "expired")
	rejects(t, old, "version")
	latest := fixture(t, 3)
	latest.StateFile = old.StateFile
	accepts(t, latest)
}

func TestTargetsThresholdEvenWithValidSnapshotAndTimestamp(t *testing.T) {
	o := fixture(t, 1)
	file := filepath.Join(o.Repository, "metadata", "1.targets.json")
	raw, e := os.ReadFile(file)
	must(t, e)
	targets, e := metadata.Targets().FromBytes(raw)
	must(t, e)
	targets.Signatures = targets.Signatures[:2]
	raw, e = targets.ToBytes(false)
	must(t, e)
	must(t, os.WriteFile(file, raw, 0644))
	snapshot := metadata.Snapshot(epoch.AddDate(0, 0, 7))
	snapshot.Signed.Meta["targets.json"] = metaInfo(1, raw)
	must(t, sign(snapshot, "snapshot", 0, 1))
	raw, e = snapshot.ToBytes(false)
	must(t, e)
	must(t, os.WriteFile(filepath.Join(o.Repository, "metadata", "1.snapshot.json"), raw, 0644))
	timestamp := metadata.Timestamp(epoch.Add(24 * time.Hour))
	timestamp.Signed.Meta["snapshot.json"] = metaInfo(1, raw)
	must(t, sign(timestamp, "timestamp", 0, 1))
	raw, e = timestamp.ToBytes(false)
	must(t, e)
	must(t, os.WriteFile(filepath.Join(o.Repository, "metadata", "timestamp.json"), raw, 0644))
	rejects(t, o, "")
}
func TestInsufficientAndDuplicateSignatures(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "two signers", true: "duplicate signer"}[duplicate], func(t *testing.T) {
			o := fixture(t, 1)
			f := filepath.Join(o.Repository, "metadata", "1.root.json")
			raw, e := os.ReadFile(f)
			must(t, e)
			r, e := metadata.Root().FromBytes(raw)
			must(t, e)
			if duplicate {
				r.Signatures = []metadata.Signature{r.Signatures[0], r.Signatures[0], r.Signatures[1]}
			} else {
				r.Signatures = r.Signatures[:2]
			}
			raw, e = r.ToBytes(false)
			must(t, e)
			must(t, os.WriteFile(o.RootFile, raw, 0644))
			h := sha256.Sum256(raw)
			o.RootSHA256 = hex.EncodeToString(h[:])
			rejects(t, o, "")
		})
	}
}
func TestRootRotationRequiresBothQuorums(t *testing.T) {
	for _, sigs := range []string{"old", "new", "both"} {
		t.Run(sigs, func(t *testing.T) {
			o := fixture(t, 1)
			accepts(t, o)
			r, e := testRoot(epoch, 2, 0)
			must(t, e)
			r.Signed.Roles["root"].KeyIDs = nil
			for i := 10; i < 15; i++ {
				key, e := metadata.KeyFromPublicKey(testKey("root", i).Public())
				must(t, e)
				must(t, r.Signed.AddKey(key, "root"))
			}
			if sigs != "new" {
				must(t, sign(r, "root", 0, 3))
			}
			if sigs != "old" {
				must(t, sign(r, "root", 10, 3))
			}
			raw, e := r.ToBytes(false)
			must(t, e)
			must(t, os.WriteFile(filepath.Join(o.Repository, "metadata", "2.root.json"), raw, 0644))
			if sigs == "both" {
				if accepts(t, o).RootVersion != 2 {
					t.Fatal("rotation not retained")
				}
				accepts(t, o)
			} else {
				rejects(t, o, "")
			}
		})
	}
}
func TestKnownTestKeysCannotBeRelabelled(t *testing.T) {
	o := fixture(t, 1)
	raw, e := os.ReadFile(o.RootFile)
	must(t, e)
	r, e := metadata.Root().FromBytes(raw)
	must(t, e)
	r.Signed.UnrecognizedFields[modeField] = "public"
	r.ClearSignatures()
	must(t, sign(r, "root", 0, 3))
	raw, e = r.ToBytes(false)
	must(t, e)
	must(t, os.WriteFile(o.RootFile, raw, 0644))
	h := sha256.Sum256(raw)
	o.RootSHA256 = hex.EncodeToString(h[:])
	o.AllowTestKeys = false
	rejects(t, o, "PUBLIC TEST KEY")
}
func TestRoleKeyReuseRefused(t *testing.T) {
	o := fixture(t, 1)
	raw, e := os.ReadFile(o.RootFile)
	must(t, e)
	r, e := metadata.Root().FromBytes(raw)
	must(t, e)
	r.Signed.Roles["timestamp"].KeyIDs = []string{r.Signed.Roles["snapshot"].KeyIDs[0]}
	r.ClearSignatures()
	must(t, sign(r, "root", 0, 3))
	raw, e = r.ToBytes(false)
	must(t, e)
	must(t, os.WriteFile(o.RootFile, raw, 0644))
	h := sha256.Sum256(raw)
	o.RootSHA256 = hex.EncodeToString(h[:])
	rejects(t, o, "distinct")
}
func TestMirrorConfinementAndNames(t *testing.T) {
	for _, p := range []string{"../root", "x/../../root", "C:/keys", "x\\keys", "/abs", "x//y", "x/.", "a%2fb", "a:", "a."} {
		if safeName(p) {
			t.Fatalf("unsafe path %s", p)
		}
	}
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	must(t, os.WriteFile(outside, []byte("secret"), 0600))
	if e := os.Symlink(outside, filepath.Join(base, "link")); e != nil {
		t.Skip("symlink unavailable on this Windows account")
	}
	if f, e := openMirror(base, "link"); e == nil {
		f.Close()
		t.Fatal("mirror escaped its root")
	}
}

func TestSignedLengthsCannotRemoveLocalResourceLimits(t *testing.T) {
	f := mirrorFetcher{repository: t.TempDir(), persist: func() error { return nil }}
	for _, u := range []string{"https://mirror.invalid/metadata/2.snapshot.json", "https://mirror.invalid/metadata/1.targets.json", "https://mirror.invalid/metadata/timestamp.json"} {
		if _, e := f.DownloadFile(u, 1<<60, 0); e == nil || !strings.Contains(e.Error(), "local profile limit") {
			t.Fatal(e)
		}
	}
}
func TestArchiveReproducible(t *testing.T) {
	source := t.TempDir()
	must(t, os.Mkdir(filepath.Join(source, "bin"), 0755))
	must(t, os.WriteFile(filepath.Join(source, "bin", "civdist"), []byte("binary"), 0755))
	a := filepath.Join(t.TempDir(), "a.zip")
	b := filepath.Join(t.TempDir(), "b.zip")
	must(t, Archive(source, a))
	must(t, os.Chtimes(filepath.Join(source, "bin", "civdist"), time.Now(), time.Now()))
	must(t, Archive(source, b))
	x, e := os.ReadFile(a)
	must(t, e)
	y, e := os.ReadFile(b)
	must(t, e)
	if !bytes.Equal(x, y) {
		t.Fatal("archive depends on filesystem timestamps")
	}
	z, e := zip.OpenReader(a)
	must(t, e)
	defer z.Close()
	if len(z.File) != 1 || z.File[0].Name != "bin/civdist" || z.File[0].Mode().Perm() != 0755 {
		t.Fatal("unexpected archive layout")
	}
	if Archive(source, a) == nil {
		t.Fatal("existing output overwritten")
	}
	if Archive(source, filepath.Join(source, "recursive.zip")) == nil {
		t.Fatal("archive included its own output")
	}
}
func TestCrashHelper(t *testing.T) {
	data := os.Getenv("CIVORA_DISTRIBUTION_CRASH_TEST")
	if data == "" {
		return
	}
	var o Options
	must(t, json.Unmarshal([]byte(data), &o))
	calls := 0
	_, e := verify(o, epoch.Add(time.Hour), func() {
		calls++
		if calls == 3 {
			os.Exit(77)
		}
	})
	t.Fatalf("expected abrupt exit after durable timestamp, got %v", e)
}
func TestAbruptExitPreservesHighWaterAndResumes(t *testing.T) {
	old := fixture(t, 1)
	accepts(t, old)
	newer := fixture(t, 2)
	newer.StateFile = old.StateFile
	data, e := json.Marshal(newer)
	must(t, e)
	child := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
	child.Env = append(os.Environ(), "CIVORA_DISTRIBUTION_CRASH_TEST="+string(data))
	output, e := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(e, &exit) || exit.ExitCode() != 77 {
		t.Fatalf("child did not crash at checkpoint: %v %s", e, output)
	}
	rejects(t, old, "")
	if accepts(t, newer).TargetsVersion != 2 {
		t.Fatal("interrupted refresh failed to resume")
	}
}

func TestCommittedDistributionFixture(t *testing.T) {
	payload := t.TempDir()
	must(t, os.WriteFile(filepath.Join(payload, "sample.TEST_ONLY.json"), []byte(`{"purpose":"PUBLIC_TEST_ONLY distribution exercise"}`), 0644))
	repo := filepath.Join(t.TempDir(), "mirror")
	must(t, SealTest(payload, repo, epoch, 1))
	committed := filepath.FromSlash("../../../../spec/fixtures/v1.0/distribution")
	must(t, filepath.WalkDir(repo, func(file string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(repo, file)
		if e != nil {
			return e
		}
		actual, e := os.ReadFile(file)
		if e != nil {
			return e
		}
		expected, e := os.ReadFile(filepath.Join(committed, rel))
		if e != nil {
			return e
		}
		if !bytes.Equal(actual, expected) {
			t.Errorf("committed fixture differs: %s", rel)
		}
		return nil
	}))
}
