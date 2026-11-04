package semaphore

import (
	"civora/internal/codec"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	Context    Context    `json:"context"`
	Ballot     string     `json:"ballot_base64url"`
	Membership Membership `json:"membership"`
}

func read(t *testing.T, depth string) fixture {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join("..", "..", "spec", "fixtures", "v0.3", "membership-"+depth+".json"))
	if e != nil {
		t.Fatal(e)
	}
	var f fixture
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	return f
}
func TestSDKGroth16VectorsAndContextRefusals(t *testing.T) {
	v, e := New()
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range []string{"16", "26"} {
		t.Run(d, func(t *testing.T) {
			f := read(t, d)
			ballot, e := codec.Blob(f.Ballot)
			if e != nil {
				t.Fatal(e)
			}
			if e = v.Verify(f.Context, ballot, f.Membership); e != nil {
				t.Fatal(e)
			}
			for _, mutate := range []func(*fixture){func(f *fixture) { f.Context.PollID = strings.Repeat("a", 64) }, func(f *fixture) { f.Context.NetworkID = strings.Repeat("b", 64) }, func(f *fixture) { f.Context.Root = "1" }, func(f *fixture) { f.Context.SuiteID = "unknown" }, func(f *fixture) { f.Membership.Message = "1" }, func(f *fixture) { f.Membership.Scope = "1" }, func(f *fixture) { f.Membership.Nullifier = "1" }, func(f *fixture) {
				f.Membership.Points = append([]string{}, f.Membership.Points...)
				f.Membership.Points[0] = "1"
			}, func(f *fixture) {
				f.Membership.Points = append([]string{}, f.Membership.Points...)
				f.Membership.Points[0] = CoordinateModulus
			}, func(f *fixture) { f.Membership.Depth = "20" }} {
				bad := read(t, d)
				mutate(&bad)
				if e = v.Verify(bad.Context, ballot, bad.Membership); e == nil {
					t.Fatal("foreign/malformed proof accepted")
				}
			}
			if e = v.Verify(f.Context, append(ballot, 1), f.Membership); e == nil {
				t.Fatal("proof relabeled to another ballot")
			}
		})
	}
}
