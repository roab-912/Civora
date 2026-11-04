package testutil

import (
	"civora/internal/codec"
	"civora/internal/core"
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
