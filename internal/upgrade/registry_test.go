package upgrade_test

import (
	"civora/internal/codec"
	"civora/internal/testutil"
	"civora/internal/upgrade"
	"encoding/json"
	"strings"
	"testing"
)

func TestRevokedSuiteCannotReturn(t *testing.T) {
	var f struct {
		Registry upgrade.Registry `json:"registry"`
		Plan     upgrade.Plan     `json:"plan"`
		Error    string           `json:"expected_error"`
	}
	if e := codec.Decode(testutil.Read(t, "revoked-suite"), "revoked", &f); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Registry.Next(f.Plan); e == nil || !strings.Contains(e.Error(), f.Error) {
		t.Fatal(e)
	}
	for _, historical := range []bool{false, true} {
		if e := f.Registry.Authorize("2", "account.sign", "dev.account.ed25519.v1", historical); e == nil {
			t.Fatal("revoked suite authorized")
		}
	}
}
func TestRegistryContextsAndLegacy(t *testing.T) {
	f := testutil.Load(t, "accounts")
	r := f.Genesis.Registry
	id := "dev.account.ed25519.v1"
	for _, tc := range []struct{ epoch, op, id string }{{"2", "account.sign", id}, {"1", "ballot.prove", id}, {"1", "account.sign", "unknown"}, {"1", "eligibility.prove", "dev.unimplemented.eligibility"}} {
		if e := r.Authorize(tc.epoch, tc.op, tc.id, false); e == nil {
			t.Fatal("unauthorized context")
		}
	}
	if e := r.Authorize("1", "account.sign", id, false); e != nil {
		t.Fatal(e)
	}
	r.Modules[1].Status = "legacy_verify_only"
	if e := r.Authorize("1", "account.sign", id, false); e == nil {
		t.Fatal("legacy new operation")
	}
	if e := r.Authorize("1", "account.sign", id, true); e != nil {
		t.Fatal(e)
	}
}
func TestPlanAndInstanceIdentity(t *testing.T) {
	f := testutil.Load(t, "activation")
	a := f.Genesis.Activations[0]
	if e := a.CheckFixture(f.Genesis.NetworkID, f.Genesis.ChainID); e != nil {
		t.Fatal(e)
	}
	a.Plan.PublicNotice += " changed"
	if e := a.CheckFixture(f.Genesis.NetworkID, f.Genesis.ChainID); e == nil {
		t.Fatal("tampered plan")
	}
	a = f.Genesis.Activations[0]
	raw := codec.Must(a)
	var copied upgrade.Armed
	_ = json.Unmarshal(raw, &copied)
	copied.Instance.Heights[0].Height = "4"
	if e := copied.CheckFixture(f.Genesis.NetworkID, f.Genesis.ChainID); e == nil {
		t.Fatal("tampered activation instance")
	}
}

func TestStateHashHasNoImplicitFallback(t *testing.T) {
	f := testutil.Load(t, "accounts")
	f.Genesis.Registry.Modules[0].Status = "revoked"
	if err := f.Genesis.Registry.Validate(); err == nil || !strings.Contains(err.Error(), "ERR_SUITE") {
		t.Fatal(err)
	}
	f.Genesis.Registry.Modules[0].Status = "active"
	f.Genesis.Registry.Modules[0].Parameters.Format = "unknown"
	if err := f.Genesis.Registry.Validate(); err == nil || !strings.Contains(err.Error(), "ERR_UNSUPPORTED_HASH_SUITE") {
		t.Fatal(err)
	}
}
