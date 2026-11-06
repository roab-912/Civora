package citizen

import (
	"civora/internal/codec"
	"civora/internal/state"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPaginationKeepsSnapshotAndExpiresOldCursor(t *testing.T) {
	entries := []state.Entry{}
	for i, c := range []string{"a", "b", "c"} {
		entries = append(entries, state.Entry{Table: "proposals", Key: strings.Repeat(c, 64), Value: map[string]any{"state": "collecting", "submitted_height": []string{"10", "9", "8"}[i]}})
	}
	snapshot := &View{Height: "600", Entries: entries}
	newest := &View{Height: "601", Entries: entries[:1]}
	reader := &Reader{views: map[uint64]*View{600: snapshot, 601: newest}}
	api := API{}
	res := httptest.NewRecorder()
	api.list(res, httptest.NewRequest("GET", "/v1/proposals?status=collecting&limit=1", nil), reader, snapshot, "proposals")
	var page struct {
		Height string           `json:"height"`
		Next   string           `json:"next_cursor"`
		Items  []map[string]any `json:"items"`
	}
	if e := json.Unmarshal(res.Body.Bytes(), &page); e != nil || page.Next == "" {
		t.Fatal(res.Body.String(), e)
	}
	res = httptest.NewRecorder()
	api.list(res, httptest.NewRequest("GET", "/v1/proposals?status=collecting&limit=1&cursor="+page.Next, nil), reader, newest, "proposals")
	if e := json.Unmarshal(res.Body.Bytes(), &page); e != nil || page.Height != "600" || page.Items[0]["id"] != strings.Repeat("b", 64) {
		t.Fatal("page drifted", res.Body.String(), e)
	}
	old := base64.RawURLEncoding.EncodeToString(codec.Must(cursor{"1", "", "proposals", "collecting"}))
	res = httptest.NewRecorder()
	api.list(res, httptest.NewRequest("GET", "/v1/proposals?status=collecting&cursor="+old, nil), reader, newest, "proposals")
	if res.Code != 410 {
		t.Fatal("expired cursor", res.Code)
	}
	res = httptest.NewRecorder()
	api.list(res, httptest.NewRequest("GET", "/v1/proposals?status=other&cursor="+old, nil), reader, newest, "proposals")
	if res.Code != 400 {
		t.Fatal("cross-filter cursor", res.Code)
	}
}

func TestProposalTypeFilterIsBoundToCursor(t *testing.T) {
	entries := []state.Entry{}
	for i, kind := range []string{"law.create", "role.create", "law.repeal"} {
		entries = append(entries, state.Entry{Table: "proposals", Key: strings.Repeat([]string{"a", "b", "c"}[i], 64), Value: map[string]any{"state": "open", "submitted_height": "1", "action": kind}})
	}
	v := &View{Height: "2", Entries: entries}
	r := &Reader{views: map[uint64]*View{2: v}}
	api := API{}
	res := httptest.NewRecorder()
	api.list(res, httptest.NewRequest("GET", "/v1/proposals?type=law&status=open&limit=1", nil), r, v, "proposals")
	var page struct {
		Items []map[string]any `json:"items"`
		Next  string           `json:"next_cursor"`
	}
	if json.Unmarshal(res.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Items[0]["action"] != "law.create" || page.Next == "" {
		t.Fatal(res.Body.String())
	}
	for _, kind := range []string{"role", ""} {
		res = httptest.NewRecorder()
		api.list(res, httptest.NewRequest("GET", "/v1/proposals?status=open&type="+kind+"&cursor="+page.Next, nil), r, v, "proposals")
		if res.Code != 400 {
			t.Fatal("cross-type cursor accepted", res.Body.String())
		}
	}
	res = httptest.NewRecorder()
	api.list(res, httptest.NewRequest("GET", "/v1/proposals?status=open&type=law&cursor="+page.Next, nil), r, v, "proposals")
	if json.Unmarshal(res.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Items[0]["action"] != "law.repeal" {
		t.Fatal(res.Body.String())
	}
}
