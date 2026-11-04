package testutil

import (
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/eligibility"
	"civora/internal/upgrade"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type Fixture struct {
	Name           string         `json:"name"`
	Genesis        core.Config    `json:"genesis"`
	Blocks         []core.Block   `json:"blocks"`
	ExpectedRoots  []string       `json:"expected_roots"`
	ExpectedEvents [][]core.Event `json:"expected_events"`
}

func WithEligibility(f Fixture, depth string) Fixture {
	id := "semaphore-v4.14.5-depth-" + depth
	m := upgrade.Module{ID: id, Status: "active", Operations: []string{"eligibility.prove"}, ImplementationSHA256: codec.Hash("test/eligibility-module", id), Artifacts: []string{}, Dependencies: []string{}, RetirementPolicy: "never_reactivate_revoked"}
	m.Parameters.Format = id
	f.Genesis.Registry.Modules = append(f.Genesis.Registry.Modules, m)
	f.Genesis.Eligibility = &eligibility.Config{CircuitDepth: depth, SuiteID: id}
	return f
}

func Root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}
func Read(t testing.TB, name string) []byte {
	t.Helper()
	b, e := os.ReadFile(filepath.Join(Root(), "spec", "fixtures", name+".json"))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func Load(t testing.TB, name string) Fixture {
	t.Helper()
	var f Fixture
	if e := codec.Decode(Read(t, name), "fixture", &f); e != nil {
		t.Fatal(e)
	}
	return f
}
