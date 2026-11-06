package citizen

import (
	"bytes"
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/partition"
	"civora/internal/state"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type API struct {
	Manifest Manifest
	Readers  map[string]*Reader
	Objects  archive.Store
	Submit   func(string, []byte) error
	Extra    func(http.ResponseWriter, *http.Request) bool
	Enqueue  func(string, []byte) error
	Pending  func(string) bool
	mu       sync.Mutex
	seen     map[string]string
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	raw := codec.Must(v)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}
func failure(w http.ResponseWriter, status int, code string, retry bool) {
	WriteJSON(w, status, map[string]any{"code": code, "message": strings.ReplaceAll(strings.TrimPrefix(code, "ERR_"), "_", " "), "retryable": retry})
}
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.Extra != nil && a.Extra(w, r) {
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	if path == "v1/transactions" && r.Method == http.MethodPost {
		a.submit(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		failure(w, 405, "ERR_METHOD", false)
		return
	}
	if strings.HasPrefix(path, "v1/objects/") {
		id := strings.TrimPrefix(path, "v1/objects/")
		p, e := a.Objects.Path(id)
		if e != nil {
			failure(w, 400, "ERR_ENCODING", false)
			return
		}
		info, e := os.Stat(p)
		if e != nil {
			failure(w, 404, "ERR_OBJECT_UNAVAILABLE", true)
			return
		}
		raw, e := a.Objects.Get(archive.Ref{SHA256: id, Size: fmt.Sprint(info.Size())})
		if e != nil {
			failure(w, 503, "ERR_ARCHIVE_INTEGRITY", true)
			return
		}
		w.Header().Set("ETag", "\""+id+"\"")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, id, time.Time{}, bytes.NewReader(raw))
		return
	}
	if path == "v1/network" {
		a.network(w)
		return
	}
	if path == "v1/network/manifest" {
		WriteJSON(w, 200, a.Manifest)
		return
	}
	selected := a.Manifest.Main.ChainID
	localPath := path
	if strings.HasPrefix(path, "v1/chains/") {
		parts := strings.SplitN(strings.TrimPrefix(path, "v1/chains/"), "/", 2)
		if len(parts) != 2 {
			failure(w, 404, "ERR_NOT_FOUND", false)
			return
		}
		selected = parts[0]
		localPath = "v1/" + parts[1]
	}
	reader := a.Readers[selected]
	if reader == nil {
		failure(w, 404, "ERR_CHAIN_UNAVAILABLE", true)
		return
	}
	if e := reader.Refresh(); e != nil {
		failure(w, 503, "ERR_PROOF", true)
		return
	}
	if localPath == "v1/genesis" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(reader.Genesis.Raw)
		return
	}
	if localPath == "v1/blocks" {
		v := reader.Latest()
		height := "0"
		if v != nil {
			height = v.Height
		}
		WriteJSON(w, 200, map[string]any{"chain_id": selected, "height": height, "first_block_height": "1"})
		return
	}
	if localPath == "v1/verify" {
		latest := reader.Latest()
		if latest == nil {
			failure(w, 503, "ERR_INDEX_NOT_READY", true)
			return
		}
		v, e := chain.New(reader.Genesis)
		if e == nil {
			height, _ := codec.Uint(latest.Height)
			for h := uint64(1); h <= height; h++ {
				b, err := reader.Source.Bundle(h)
				if err != nil {
					e = err
					break
				}
				if err = v.Apply(b); err != nil {
					e = err
					break
				}
			}
		}
		if e != nil {
			failure(w, 422, "ERR_PROOF", false)
			return
		}
		WriteJSON(w, 200, map[string]any{"chain_id": selected, "from": "genesis", "height": latest.Height, "app_hash": v.Machine.Root(), "replayed": true})
		return
	}
	if strings.HasPrefix(localPath, "v1/blocks/") {
		h, e := codec.Uint(strings.TrimPrefix(localPath, "v1/blocks/"))
		if e != nil || h == 0 {
			failure(w, 400, "ERR_ENCODING", false)
			return
		}
		v, e := reader.At(h)
		if e != nil {
			failure(w, 404, "ERR_BLOCK_UNAVAILABLE", true)
			return
		}
		raw, e := reader.Objects.Get(v.Bundle)
		if e != nil {
			failure(w, 503, "ERR_ARCHIVE_INTEGRITY", true)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
		return
	}
	view := reader.Latest()
	if view == nil {
		failure(w, 503, "ERR_INDEX_NOT_READY", true)
		return
	}
	if localPath == "v1/proofs/state" {
		h, e := heightQuery(r, view.Height)
		keyRaw, e2 := codec.Blob(r.URL.Query().Get("key"))
		key, e3 := codec.Parse(keyRaw)
		table := r.URL.Query().Get("table")
		if e != nil || e2 != nil || e3 != nil || len(keyRaw) > 4096 || len(table) == 0 || len(table) > 96 {
			failure(w, 400, "ERR_ENCODING", false)
			return
		}
		proof, e := reader.StateProof(h, table, key)
		if e != nil {
			failure(w, 503, "ERR_PROOF_PENDING", true)
			return
		}
		WriteJSON(w, 200, proof)
		return
	}
	if strings.HasPrefix(localPath, "v1/proofs/tx/") {
		p, e := reader.TransactionProof(strings.TrimPrefix(localPath, "v1/proofs/tx/"))
		if e != nil {
			failure(w, 404, "ERR_TRANSACTION_UNKNOWN", true)
			return
		}
		WriteJSON(w, 200, p)
		return
	}
	if strings.HasPrefix(localPath, "v1/transactions/") {
		id := strings.TrimPrefix(localPath, "v1/transactions/")
		if t, ok := reader.FindTransaction(id); ok {
			WriteJSON(w, 200, t)
			return
		}
		status := "unknown"
		a.mu.Lock()
		if a.seen[id] != "" {
			status = "seen"
		}
		a.mu.Unlock()
		if a.Pending != nil && a.Pending(id) {
			status = "seen"
		}
		WriteJSON(w, 200, map[string]any{"tx_id": id, "status": status, "height": view.Height})
		return
	}
	if localPath == "v1/events" {
		a.events(w, r, reader, view)
		return
	}
	if localPath == "v1/network/upgrades" {
		items := []any{}
		for _, e := range view.Entries {
			if e.Table == "upgrades" {
				items = append(items, e.Value)
			}
		}
		WriteJSON(w, 200, map[string]any{"height": view.Height, "items": items})
		return
	}
	for _, resource := range []string{"proposals", "polls", "laws", "roles", "accounts"} {
		prefix := "v1/" + resource
		if localPath == prefix {
			a.list(w, r, reader, view, resource)
			return
		}
		if strings.HasPrefix(localPath, prefix+"/") {
			suffix := strings.TrimPrefix(localPath, prefix+"/")
			if !strings.Contains(suffix, "/") {
				a.detail(w, view, resource, suffix)
				return
			}
		}
	}
	failure(w, 404, "ERR_NOT_FOUND", false)
}
func heightQuery(r *http.Request, defaultHeight string) (uint64, error) {
	h := r.URL.Query().Get("height")
	if h == "" {
		h = defaultHeight
	}
	return codec.Uint(h)
}
func (a *API) network(w http.ResponseWriter) {
	chains := []any{}
	mainHeight := "0"
	epoch, lastTime := "0", "0"
	for _, c := range append([]Chain{a.Manifest.Main}, a.Manifest.Lanes...) {
		reader := a.Readers[c.ChainID]
		if reader == nil {
			continue
		}
		if e := reader.Refresh(); e != nil {
			failure(w, 503, "ERR_PROOF", true)
			return
		}
		v := reader.Latest()
		h, tm, verified := "0", "0", "0"
		if v != nil {
			h = v.Height
			tm = v.Time
			n, _ := codec.Uint(h)
			if n > 1 {
				verified = fmt.Sprint(n - 1)
			}
			if c.Lane == nil {
				mainHeight = h
				lastTime = tm
				switch m := application(v.Machine).(type) {
				case *consensus.Ledger:
					epoch = m.Engine.Config.Registry.Epoch
				case *partition.Main:
					epoch = m.Engine.Config.Registry.Epoch
				}
			}
		}
		chains = append(chains, map[string]any{"chain_id": c.ChainID, "height": h, "verified_state_height": verified, "time_ms": tm, "first_block_height": "1", "first_state_height": "1", "mode": "archive", "has_consensus_key": false})
	}
	WriteJSON(w, 200, map[string]any{"network_id": a.Manifest.NetworkID, "mode": a.Manifest.Mode, "height": mainHeight, "protocol_epoch": epoch, "last_block_time_ms": lastTime, "stale": time.Now().UnixMilli()-int64(mustUint(lastTime)) > 60000, "profile": a.Manifest.Profile, "security_status": "development_unqualified", "chains": chains, "manifest": "/v1/network/manifest"})
}
func tableFor(resource string) string {
	if resource == "laws" || resource == "roles" {
		return "objects"
	}
	return resource
}
func publicRow(resource string, e state.Entry) (map[string]any, bool) {
	value := map[string]any{}
	if json.Unmarshal(codec.Must(e.Value), &value) != nil {
		return nil, false
	}
	if resource == "laws" && value["kind"] != "law" || resource == "roles" && value["kind"] != "role" {
		return nil, false
	}
	value["id"] = e.Key
	if resource == "laws" || resource == "roles" {
		var tuple []string
		if json.Unmarshal(codec.Must(e.Key), &tuple) != nil || len(tuple) != 2 {
			return nil, false
		}
		value["id"] = tuple[1]
	}
	if resource == "polls" {
		delete(value, "commitments")
	}
	return value, true
}
func (a *API) detail(w http.ResponseWriter, v *View, resource, id string) {
	for _, e := range v.Entries {
		if e.Table == tableFor(resource) {
			row, ok := publicRow(resource, e)
			if !ok || fmt.Sprint(row["id"]) != id {
				continue
			}
			if resource == "laws" || resource == "roles" {
				versions := []any{}
				for _, revision := range v.Entries {
					if revision.Table != "revisions" {
						continue
					}
					var tuple []string
					if json.Unmarshal(codec.Must(revision.Key), &tuple) == nil && len(tuple) == 3 && tuple[1] == id {
						versions = append(versions, map[string]any{"revision": tuple[2], "value": revision.Value})
						if tuple[2] == fmt.Sprint(row["revision"]) {
							row["current"] = revision.Value
						}
					}
				}
				row["versions"] = versions
				if resource == "roles" {
					grants := []any{}
					for _, entry := range v.Entries {
						if entry.Table != "grants" {
							continue
						}
						var tuple []string
						if json.Unmarshal(codec.Must(entry.Key), &tuple) != nil || len(tuple) != 2 || tuple[0] != id {
							continue
						}
						value := map[string]any{}
						_ = json.Unmarshal(codec.Must(entry.Value), &value)
						value["id"] = tuple[1]
						value["proof"] = map[string]any{"table": "grants", "key": base64.RawURLEncoding.EncodeToString(codec.Must(entry.Key)), "height": v.Height}
						grants = append(grants, value)
					}
					row["grants"] = grants
				}
			}
			row["height"] = v.Height
			row["proof"] = map[string]any{"table": e.Table, "key": base64.RawURLEncoding.EncodeToString(codec.Must(e.Key)), "height": v.Height}
			WriteJSON(w, 200, row)
			return
		}
	}
	failure(w, 404, "ERR_NOT_FOUND", false)
}

type cursor struct {
	Height   string `json:"height"`
	Last     string `json:"last"`
	Resource string `json:"resource"`
	Filter   string `json:"filter"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request, reader *Reader, latest *View, resource string) {
	limit := 50
	if s := r.URL.Query().Get("limit"); s != "" {
		n, e := strconv.Atoi(s)
		if e != nil || n < 1 || n > 100 {
			failure(w, 400, "ERR_ENCODING", false)
			return
		}
		limit = n
	}
	filter := r.URL.Query().Get("status")
	kind := r.URL.Query().Get("type")
	if kind != "" && (resource != "proposals" || (kind != "law" && kind != "role")) {
		failure(w, 400, "ERR_ENCODING", false)
		return
	}
	cursorFilter := filter
	if kind != "" {
		cursorFilter += ";type=" + kind
	}
	height, _ := codec.Uint(latest.Height)
	last := ""
	if s := r.URL.Query().Get("cursor"); s != "" {
		raw, e := codec.Blob(s)
		var c cursor
		if e != nil || codec.Decode(raw, "citizen_cursor", &c) != nil || c.Resource != resource || c.Filter != cursorFilter {
			failure(w, 400, "ERR_ENCODING", false)
			return
		}
		h, _ := codec.Uint(c.Height)
		if h > height || height-h > 512 {
			failure(w, 410, "ERR_CURSOR_EXPIRED", true)
			return
		}
		height = h
		last = c.Last
	}
	view, e := reader.At(height)
	if e != nil {
		failure(w, 410, "ERR_CURSOR_EXPIRED", true)
		return
	}
	items := []map[string]any{}
	for _, entry := range view.Entries {
		if entry.Table != tableFor(resource) {
			continue
		}
		row, ok := publicRow(resource, entry)
		if !ok {
			continue
		}
		status, _ := row["state"].(string)
		if status == "" {
			status, _ = row["status"].(string)
		}
		if filter != "" && filter != status {
			continue
		}
		if kind != "" && !strings.HasPrefix(fmt.Sprint(row["action"]), kind+".") {
			continue
		}
		if resource == "laws" || resource == "roles" {
			for _, revision := range view.Entries {
				if revision.Table != "revisions" {
					continue
				}
				var tuple []string
				if json.Unmarshal(codec.Must(revision.Key), &tuple) == nil && len(tuple) == 3 && tuple[1] == row["id"] && tuple[2] == row["revision"] {
					var rev struct {
						Content map[string]any `json:"content"`
					}
					if json.Unmarshal(codec.Must(revision.Value), &rev) == nil {
						if resource == "laws" {
							row["title"] = rev.Content["title"]
						} else {
							row["title"] = rev.Content["label"]
						}
					}
					break
				}
			}
		}
		items = append(items, row)
		row["proof"] = map[string]any{"table": entry.Table, "key": base64.RawURLEncoding.EncodeToString(codec.Must(entry.Key)), "height": view.Height}
	}
	sort.Slice(items, func(i, j int) bool {
		if resource == "proposals" {
			a, _ := items[i]["submitted_height"].(string)
			b, _ := items[j]["submitted_height"].(string)
			an, _ := codec.Uint(a)
			bn, _ := codec.Uint(b)
			if an != bn {
				return an > bn
			}
		}
		return fmt.Sprint(items[i]["id"]) < fmt.Sprint(items[j]["id"])
	})
	after := last == ""
	page := []any{}
	next := ""
	for i, row := range items {
		id := fmt.Sprint(row["id"])
		if !after {
			if id == last {
				after = true
			}
			continue
		}
		page = append(page, row)
		if len(page) == limit {
			if i+1 < len(items) {
				next = base64.RawURLEncoding.EncodeToString(codec.Must(cursor{view.Height, id, resource, cursorFilter}))
			}
			break
		}
	}
	WriteJSON(w, 200, map[string]any{"height": view.Height, "items": page, "next_cursor": next})
}
func (a *API) events(w http.ResponseWriter, r *http.Request, reader *Reader, latest *View) {
	after := r.URL.Query().Get("after")
	from, pos := uint64(1), uint64(0)
	if after != "" {
		parts := strings.Split(after, ":")
		if len(parts) != 2 {
			failure(w, 400, "ERR_ENCODING", false)
			return
		}
		h, e := codec.Uint(parts[0])
		p, e2 := codec.Uint(parts[1])
		if e != nil || e2 != nil {
			failure(w, 400, "ERR_ENCODING", false)
			return
		}
		from = h
		pos = p + 1
	}
	limit := 100
	if s := r.URL.Query().Get("limit"); s != "" {
		n, e := strconv.Atoi(s)
		if e != nil || n < 1 || n > 500 {
			failure(w, 400, "ERR_ENCODING", false)
			return
		}
		limit = n
	}
	max, _ := codec.Uint(latest.Height)
	out := []Event{}
	next := after
	for h := from; h <= max; h++ {
		v, e := reader.At(h)
		if e != nil {
			continue
		}
		for _, event := range v.Events {
			p, _ := codec.Uint(event.Position)
			if h == from && p < pos {
				continue
			}
			out = append(out, event)
			next = event.Height + ":" + event.Position
			if len(out) == limit {
				WriteJSON(w, 200, map[string]any{"height": latest.Height, "items": out, "next_cursor": next})
				return
			}
		}
	}
	WriteJSON(w, 200, map[string]any{"height": latest.Height, "items": out, "next_cursor": next})
}
func (a *API) submit(w http.ResponseWriter, r *http.Request) {
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024))
	if e != nil {
		failure(w, 413, "ERR_CAPACITY", false)
		return
	}
	if _, e = codec.Parse(raw); e != nil {
		failure(w, 400, "ERR_ENCODING", false)
		return
	}
	reader := a.Readers[a.Manifest.Main.ChainID]
	if reader == nil || reader.Refresh() != nil || reader.Latest() == nil || a.Submit == nil {
		failure(w, 503, "ERR_UNAVAILABLE", true)
		return
	}
	id := TransactionID(raw)
	if _, ok := reader.FindTransaction(id); ok {
		WriteJSON(w, 202, map[string]any{"tx_id": id, "status": "seen"})
		return
	}
	a.mu.Lock()
	if a.seen == nil {
		a.seen = map[string]string{}
	}
	known := a.seen[id] != ""
	a.mu.Unlock()
	if !known {
		if e = reader.Latest().Machine.Check(raw); e != nil {
			failure(w, 422, "ERR_PROOF", false)
			return
		}
		if a.Enqueue != nil {
			if e = a.Enqueue(id, raw); e != nil {
				failure(w, 503, "ERR_CAPACITY", true)
				return
			}
		}
		if e = a.Submit(a.Manifest.Main.ChainID, raw); e != nil {
			if a.Enqueue == nil {
				failure(w, 503, "ERR_UNAVAILABLE", true)
				return
			}
		}
		a.mu.Lock()
		a.seen[id] = archive.Digest(raw)
		a.mu.Unlock()
	}
	WriteJSON(w, 202, map[string]any{"tx_id": id, "status": "seen"})
}

var _ chain.Provider = (*Native)(nil)
