package recovery

import (
	"bufio"
	"bytes"
	"civora/internal/codec"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
)

const fixtureNow uint64 = 1793970000000

func fixtureRoot(label string) (Root, map[string]*mldsa65.PrivateKey, map[string]string) {
	r := Root{"1", strings.Repeat("a1", 32), "1", "development_TEST_ONLY", Number(fixtureNow - 1000), Number(fixtureNow + 7*24*3600000), []Member{}, []Member{}}
	keys := map[string]*mldsa65.PrivateKey{}
	seeds := map[string]string{}
	for _, group := range []string{"technical", "civic", "mirror"} {
		n := 7
		if group == "mirror" {
			n = 5
		}
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("%s/%s/%d", label, group, i)
			seed := sha256.Sum256([]byte("CIVORA PUBLIC_TEST_ONLY ML-DSA-65 v0.9 " + id))
			pk, sk := mldsa65.NewKeyFromSeed(&seed)
			keys[id] = sk
			seeds[id] = hex.EncodeToString(seed[:])
			m := Member{id, group, base64.RawURLEncoding.EncodeToString(pk.Bytes())}
			if group == "mirror" {
				m.College = ""
				r.Mirrors = append(r.Mirrors, m)
			} else {
				r.Members = append(r.Members, m)
			}
		}
	}
	return r, keys, seeds
}
func signature(sk *mldsa65.PrivateKey, d []byte) string {
	sig := make([]byte, mldsa65.SignatureSize)
	if e := mldsa65.SignTo(sk, d, nil, false, sig); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(sig)
}
func signQuorum(r Root, keys map[string]*mldsa65.PrivateKey, d []byte) []Signature {
	sigs := []Signature{}
	counts := map[string]int{}
	for _, m := range r.Members {
		if counts[m.College] < 5 {
			sigs = append(sigs, Signature{m.ID, signature(keys[m.ID], d)})
			counts[m.College]++
		}
	}
	return sigs
}
func fixtureBundle(r Root, keys map[string]*mldsa65.PrivateKey) Bundle {
	o := Order{"1", r.Network, r.Epoch, "1", "security.hold", "module", "dev.ballot.vulnerable", strings.Repeat("b2", 48), "last authenticated checkpoint / TEST_ONLY", "PUBLIC_TEST_ONLY incident exercise", Number(fixtureNow), Number(fixtureNow + 3600000)}
	b := Bundle{o, signQuorum(r, keys, OrderDigest(o)), []Mirror{}}
	d := hex.EncodeToString(OrderDigest(o))
	for _, m := range r.Mirrors[:3] {
		s := Statement{r.Network, r.Epoch, o.Counter, RootID(r), d, Number(fixtureNow), Number(fixtureNow + FreshnessMS)}
		b.Mirrors = append(b.Mirrors, Mirror{m.ID, s, signature(keys[m.ID], Digest("CIVIC/RECOVERY/MIRROR/1\n", s))})
	}
	return b
}
func TestRecoveryFixtures(t *testing.T) {
	r, keys, seeds := fixtureRoot("root1")
	b := fixtureBundle(r, keys)
	fixtures := map[string]any{"root.TEST_ONLY.json": r, "hold.TEST_ONLY.json": b, "keys.TEST_ONLY.json": map[string]any{"warning": "PUBLIC_TEST_ONLY deterministic seeds. Never install this root for real security.", "seeds_hex": seeds}}
	for name, value := range fixtures {
		path := filepath.Join("..", "..", "spec", "fixtures", "v0.9", name)
		raw := codec.Must(value)
		if os.Getenv("CIVORA_WRITE_RECOVERY_FIXTURES") == "1" {
			if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
		}
		got, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(got, raw) {
			t.Fatalf("fixture differs %s: %v", name, e)
		}
	}
	if err := Verify(r, b, fixtureNow, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRoot(codec.Must(r), RootID(r), fixtureNow, false); err == nil {
		t.Fatal("test root accepted without opt in")
	}
}
func TestRecoveryRefusals(t *testing.T) {
	r, k, _ := fixtureRoot("root1")
	base := fixtureBundle(r, k)
	tests := map[string]func(*Root, *Bundle){
		"missing civic quorum": func(_ *Root, b *Bundle) { b.Signatures = b.Signatures[:9] },
		"duplicate signer":     func(_ *Root, b *Bundle) { b.Signatures[9] = b.Signatures[0] },
		"forged order":         func(_ *Root, b *Bundle) { b.Order.ScopeID = "other" },
		"foreign network":      func(_ *Root, b *Bundle) { b.Order.Network = strings.Repeat("c3", 32) },
		"wrong epoch":          func(_ *Root, b *Bundle) { b.Order.Epoch = "2" },
		"expired":              func(_ *Root, b *Bundle) { b.Order.Expires = Number(fixtureNow) },
		"future":               func(_ *Root, b *Bundle) { b.Order.Starts = Number(fixtureNow + 1) },
		"hold over 72h":        func(_ *Root, b *Bundle) { b.Order.Expires = Number(fixtureNow + MaxHoldMS + 1) },
		"reused member key":    func(r *Root, _ *Bundle) { r.Members[7].PublicKey = r.Members[0].PublicKey },
		"missing mirrors":      func(_ *Root, b *Bundle) { b.Mirrors = b.Mirrors[:2] },
		"duplicate mirror":     func(_ *Root, b *Bundle) { b.Mirrors[2] = b.Mirrors[0] },
		"stale mirror":         func(_ *Root, b *Bundle) { b.Mirrors[0].Statement.Observed = "1" },
		"wrong root pin":       func(r *Root, _ *Bundle) { r.Mirrors[0].PublicKey = r.Mirrors[1].PublicKey },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			var root Root
			var b Bundle
			json.Unmarshal(codec.Must(r), &root)
			json.Unmarshal(codec.Must(base), &b)
			mutate(&root, &b)
			if e := Verify(root, b, fixtureNow, 0, true); e == nil {
				t.Fatal("unsafe bundle accepted")
			}
		})
	}
	if e := Verify(r, base, fixtureNow, 1, true); e == nil {
		t.Fatal("replay accepted")
	}
}
func TestDurableReplayAndClock(t *testing.T) {
	r, k, _ := fixtureRoot("root1")
	b := fixtureBundle(r, k)
	p := filepath.Join(t.TempDir(), "recovery.db")
	s, e := Open(p, r, fixtureNow, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Apply(b, fixtureNow); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(p, r, fixtureNow, true)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Apply(b, fixtureNow); e == nil {
		t.Fatal("replay after restart")
	}
	if e = s.Apply(b, fixtureNow-1); e == nil || !strings.Contains(e.Error(), "CLOCK_ROLLBACK") {
		t.Fatal(e)
	}
	v, e := s.Snapshot()
	if e != nil || v.Counter != "1" || !v.ReadOnly || len(v.Orders) != 1 {
		t.Fatal(v, e)
	}
	// Expiry never resumes the interrupted poll or removes the incident record.
	if v.Orders[0].Expires != b.Order.Expires {
		t.Fatal("hold lost")
	}
}

type fixtureApproval struct{}

func (fixtureApproval) VerifyRecoveryRootApproval(_, _, _, approval string) error {
	if approval != strings.Repeat("d4", 48) {
		return fmt.Errorf("TEST_ONLY approval mismatch")
	}
	return nil
}
func TestRootRotationRequiresBothConfigurationsAndConstitution(t *testing.T) {
	old, ok, _ := fixtureRoot("old")
	next, nk, _ := fixtureRoot("new")
	next.Epoch = "2"
	rot := Rotation{Next: next, Approval: strings.Repeat("d4", 48)}
	body := struct {
		Old      string `json:"old_root_sha3_384"`
		Next     Root   `json:"next_root"`
		Approval string `json:"approval_sha3_384"`
	}{RootID(old), next, rot.Approval}
	d := Digest("CIVIC/RECOVERY/ROOT/1\n", body)
	rot.Old = signQuorum(old, ok, d)
	rot.New = signQuorum(next, nk, d)
	if e := VerifyRotation(old, rot, fixtureNow, true, nil); e == nil {
		t.Fatal("missing constitutional proof")
	}
	if e := VerifyRotation(old, rot, fixtureNow, true, fixtureApproval{}); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "rotation.db")
	store, e := Open(path, old, fixtureNow, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.Apply(fixtureBundle(old, ok), fixtureNow); e != nil {
		t.Fatal(e)
	}
	if e = store.Rotate(rot, fixtureNow, fixtureApproval{}); e != nil {
		t.Fatal(e)
	}
	store.Close()
	if unsafe, e := Open(path, old, fixtureNow, true); e == nil {
		unsafe.Close()
		t.Fatal("old root accepted after durable rotation")
	}
	store, e = Open(path, next, fixtureNow, true)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	if e = store.Apply(fixtureBundle(next, nk), fixtureNow); e != nil {
		t.Fatal(e)
	}
	state, e := store.Snapshot()
	if e != nil || state.Counters["1"] != "1" || state.Counters["2"] != "1" {
		t.Fatal(state, e)
	}
	rot.New = rot.New[:9]
	if e := VerifyRotation(old, rot, fixtureNow, true, fixtureApproval{}); e == nil {
		t.Fatal("missing new-root quorum")
	}
}
func TestAbruptRecoveryCounter(t *testing.T) {
	if p := os.Getenv("CIVORA_RECOVERY_CRASH_DB"); p != "" {
		r, k, _ := fixtureRoot("root1")
		s, e := Open(p, r, fixtureNow, true)
		if e != nil {
			os.Exit(2)
		}
		if e = s.Apply(fixtureBundle(r, k), fixtureNow); e != nil {
			os.Exit(3)
		}
		fmt.Fprintln(os.Stdout, "COMMITTED")
		for {
			time.Sleep(time.Hour)
		}
	}
	p := filepath.Join(t.TempDir(), "crash.db")
	cmd := exec.Command(os.Args[0], "-test.run=^TestAbruptRecoveryCounter$")
	cmd.Env = append(os.Environ(), "CIVORA_RECOVERY_CRASH_DB="+p)
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer cmd.Process.Kill()
	line, e := bufio.NewReader(out).ReadString('\n')
	if e != nil || line != "COMMITTED\n" {
		t.Fatal(line, e)
	}
	cmd.Process.Kill()
	cmd.Wait()
	r, k, _ := fixtureRoot("root1")
	s, e := Open(p, r, fixtureNow, true)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Apply(fixtureBundle(r, k), fixtureNow); e == nil {
		t.Fatal("counter lost after SIGKILL")
	}
}
func FuzzRecoveryCanonical(f *testing.F) {
	r, k, _ := fixtureRoot("root1")
	f.Add(codec.Must(fixtureBundle(r, k)))
	onChain := fixtureBundle(r, k)
	onChain.Mirrors = []Mirror{}
	f.Add(codec.Must(onChain))
	f.Add([]byte(`{"order":null}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 256*1024 {
			return
		}
		var b Bundle
		if codec.Decode(raw, "recovery_bundle", &b) == nil {
			_ = Verify(r, b, fixtureNow, 0, true)
			_ = VerifyOnChain(r, b, fixtureNow, 0, true)
		}
	})
}
