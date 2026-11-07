// Package recoverytest contains PUBLIC_TEST_ONLY deterministic signing helpers.
// Production nodes never import this package or its public private seeds.
package recoverytest

import (
	"civora/internal/codec"
	"civora/internal/recovery"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func Config(t testing.TB, network, profile string) *recovery.Config {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "spec", "fixtures", "v0.9", "root.TEST_ONLY.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r recovery.Root
	if err = json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	r.Network = network
	r.NotBefore = "0"
	r.Expires = "18446744073709551615"
	return &recovery.Config{Root: r, Pin: recovery.RootID(r), Profile: profile}
}
func Hold(t testing.TB, c *recovery.Config, kind, id, counter string, start, end uint64) recovery.Bundle {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "spec", "fixtures", "v0.9", "keys.TEST_ONLY.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Seeds map[string]string `json:"seeds_hex"`
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	o := recovery.Order{V: "1", Network: c.Root.Network, Epoch: c.Root.Epoch, Counter: counter, Type: "security.hold", ScopeKind: kind, ScopeID: id, Reference: recovery.HashBytes([]byte("PUBLIC_TEST_ONLY evidence")), Point: "PUBLIC_TEST_ONLY last trusted checkpoint", Reason: "PUBLIC_TEST_ONLY local incident exercise", Starts: recovery.Number(start), Expires: recovery.Number(end)}
	b := recovery.Bundle{Order: o, Signatures: []recovery.Signature{}, Mirrors: []recovery.Mirror{}}
	counts := map[string]int{}
	for _, m := range c.Root.Members {
		if counts[m.College] >= 5 {
			continue
		}
		seedRaw, err := hex.DecodeString(f.Seeds[m.ID])
		if err != nil || len(seedRaw) != 32 {
			t.Fatal("TEST_ONLY seed", err)
		}
		var seed [32]byte
		copy(seed[:], seedRaw)
		_, sk := mldsa65.NewKeyFromSeed(&seed)
		sig := make([]byte, mldsa65.SignatureSize)
		if err = mldsa65.SignTo(sk, recovery.OrderDigest(o), nil, false, sig); err != nil {
			t.Fatal(err)
		}
		b.Signatures = append(b.Signatures, recovery.Signature{MemberID: m.ID, Signature: base64.RawURLEncoding.EncodeToString(sig)})
		counts[m.College]++
	}
	return b
}
func Command(b recovery.Bundle) []byte {
	return codec.Must(map[string]any{"type": "security.hold", "value": b})
}
