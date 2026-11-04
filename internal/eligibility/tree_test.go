package eligibility

import (
	"bytes"
	"civora/internal/codec"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type vector struct {
	NetworkID string `json:"network_id"`
	Steps     []struct {
		Changes  []Change `json:"changes"`
		Snapshot Metadata `json:"snapshot"`
		Paths    []Path   `json:"paths"`
	} `json:"steps"`
}

func loadVector(t *testing.T) vector {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join("..", "..", "spec", "fixtures", "v0.3", "lean-imt.json"))
	if e != nil {
		t.Fatal(e)
	}
	var v vector
	if e = json.Unmarshal(raw, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func applyChanges(t *testing.T, tree *Tree, changes []Change) {
	t.Helper()
	for _, c := range changes {
		index, e := codec.Uint(c.Index)
		if e != nil {
			t.Fatal(e)
		}
		if c.Operation == "append" {
			if index != tree.Size {
				t.Fatal("index")
			}
			e = tree.Append(c.Commitment)
		} else {
			e = tree.Update(index, c.Commitment)
		}
		if e != nil {
			t.Fatal(e)
		}
	}
}
func TestSDKLeanIMTVectorsAndHistoricalSnapshots(t *testing.T) {
	v := loadVector(t)
	tree, _ := New(16)
	initial := tree.Metadata(v.NetworkID, "1", "sdk-cross-vector", "semaphore-v4.14.5-depth-16", "0")
	dir := t.TempDir()
	store, e := OpenStore(dir, initial)
	if e != nil {
		t.Fatal(e)
	}
	for i, step := range v.Steps {
		old := tree.Clone()
		oldRoot := codec.Must(old.Root())
		applyChanges(t, tree, step.Changes)
		m := tree.Metadata(v.NetworkID, "1", "sdk-cross-vector", "semaphore-v4.14.5-depth-16", fmt.Sprint(i+1))
		if !bytes.Equal(codec.Must(m), codec.Must(step.Snapshot)) {
			t.Fatalf("root/size metadata mismatch at step %d", i)
		}
		durable, e := store.Commit(uint64(i+1), step.Changes)
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(codec.Must(durable), codec.Must(m)) {
			t.Fatal("durable metadata")
		}
		for _, p := range step.Paths {
			idx, _ := codec.Uint(p.LeafIndex)
			got, e := tree.Path(m, idx)
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(codec.Must(got), codec.Must(p)) {
				t.Fatal("SDK compressed index/path mismatch")
			}
			got, e = store.Path(uint64(i+1), idx)
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(codec.Must(got), codec.Must(p)) {
				t.Fatal("persisted path mismatch")
			}
		}
		if !bytes.Equal(oldRoot, codec.Must(old.Root())) {
			t.Fatal("old tree changed")
		}
	}
	if e = store.Close(); e != nil {
		t.Fatal(e)
	}
	meta, e := BaseMetadata(dir)
	if e != nil {
		t.Fatal(e)
	}
	store, e = OpenStore(dir, meta)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	for i, step := range v.Steps {
		for _, p := range step.Paths {
			idx, _ := codec.Uint(p.LeafIndex)
			got, e := store.Path(uint64(i+1), idx)
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(codec.Must(got), codec.Must(p)) {
				t.Fatal("historical snapshot was overwritten")
			}
		}
	}
	if _, e = store.Path(uint64(len(v.Steps)), 1); e == nil {
		t.Fatal("retired leaf served as active")
	}
	before, e := store.Latest()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.Commit(uint64(len(v.Steps)+1), []Change{{"update", "1", "123"}}); e == nil {
		t.Fatal("retired position reused")
	}
	after, _ := store.Latest()
	if before.SnapshotID != after.SnapshotID {
		t.Fatal("failed transaction moved cursor")
	}
}
func TestForgedPathAndFieldBounds(t *testing.T) {
	v := loadVector(t)
	p := v.Steps[4].Paths[4]
	if p.Index == p.LeafIndex {
		t.Fatal("fixture must exercise compressed index")
	}
	if e := p.Verify(); e != nil {
		t.Fatal(e)
	}
	p.Siblings = append([]string{}, p.Siblings...)
	p.Siblings[0] = "1"
	if e := p.Verify(); e == nil {
		t.Fatal("false path accepted")
	}
	for _, s := range []string{"00", "-1", ScalarModulus, "1.0", ""} {
		if _, e := ParseField(s, false); e == nil {
			t.Fatal("field accepted", s)
		}
	}
	tree, _ := New(16)
	tree.Size = 1 << 16
	if e := tree.Append("1"); e == nil {
		t.Fatal("capacity overflow")
	}
}
func TestBulkBaseDeltasAndAPI(t *testing.T) {
	v := loadVector(t)
	empty, _ := New(26)
	m := empty.Metadata(v.NetworkID, "1", "bulk-test", "semaphore-v4.14.5-depth-26", "1")
	dir := t.TempDir()
	tree, _ := New(26)
	values := []string{}
	for i := 1; i <= 37; i++ {
		values = append(values, fmt.Sprint(i))
	}
	if e := tree.Append(values...); e != nil {
		t.Fatal(e)
	}
	s, e := BuildStore(dir, m, 37, func(i uint64) (Node, error) { return ParseField(fmt.Sprint(i+1), true) }, nil)
	if e != nil {
		t.Fatal(e)
	}
	latest, e := s.Latest()
	if e != nil || *latest.Root != *tree.Root() {
		t.Fatal(e)
	}
	if _, e = s.Commit(2, []Change{{"update", "0", "0"}, {"append", "37", "99"}}); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	meta, e := BaseMetadata(dir)
	if e != nil {
		t.Fatal(e)
	}
	s, e = OpenStore(dir, meta)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.Path(1, 0); e != nil {
		t.Fatal("old root lost", e)
	}
	if _, e = s.Path(2, 0); e == nil {
		t.Fatal("retired leaf returned")
	}
	p, e := s.Path(2, 37)
	if e != nil {
		t.Fatal(e)
	}
	handler := Handler(s)
	for _, tc := range []struct {
		method, url string
		status      int
	}{{"GET", "/v1/eligibility/snapshots/latest", 200}, {"GET", "/v1/eligibility/snapshots/2", 200}, {"GET", "/v1/eligibility/snapshots/2/paths/37", 200}, {"GET", "/v1/eligibility/snapshots/2/paths/0", 404}, {"GET", "/v1/eligibility/snapshots/999", 404}, {"GET", "/v1/eligibility/snapshots/02", 400}, {"POST", "/v1/eligibility/snapshots/2", 405}} {
		r := httptest.NewRequest(tc.method, tc.url, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.url, w.Code)
		}
		if tc.url == "/v1/eligibility/snapshots/2/paths/37" && !bytes.Equal(w.Body.Bytes(), codec.Must(p)) {
			t.Fatal("API path differs")
		}
	}
}
func TestBulkCorruptionRefused(t *testing.T) {
	v := loadVector(t)
	tree, _ := New(16)
	m := tree.Metadata(v.NetworkID, "1", "corruption", "semaphore-v4.14.5-depth-16", "1")
	dir := t.TempDir()
	s, e := BuildStore(dir, m, 3, func(i uint64) (Node, error) { return ParseField(fmt.Sprint(i+1), true) }, nil)
	if e != nil {
		t.Fatal(e)
	}
	_ = s.Close()
	raw, e := os.ReadFile(filepath.Join(dir, "base", "level-00.bin"))
	if e != nil {
		t.Fatal(e)
	}
	raw[0] ^= 1
	if e = os.WriteFile(filepath.Join(dir, "base", "level-00.bin"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	meta, e := BaseMetadata(dir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = OpenStore(dir, meta); e == nil || !strings.Contains(e.Error(), "ERR_BASE_HASH") {
		t.Fatal("corrupt base accepted", e)
	}
}
