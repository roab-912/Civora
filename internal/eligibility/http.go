package eligibility

import (
	"civora/internal/codec"
	"net/http"
)

// Handler serves public, read-only paths. HTTP is discovery/transport only:
// clients must obtain the expected root from an authenticated ledger context.
func Handler(s *Store) http.Handler {
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, status int, value any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(status)
		_, _ = w.Write(codec.Must(value))
	}
	bad := func(w http.ResponseWriter) { reply(w, 400, map[string]any{"error": "ERR_REQUEST"}) }
	mux.HandleFunc("GET /v1/eligibility/snapshots/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			bad(w)
			return
		}
		m, e := s.Latest()
		if e != nil {
			reply(w, 500, map[string]any{"error": "ERR_STORAGE"})
			return
		}
		reply(w, 200, m)
	})
	mux.HandleFunc("GET /v1/eligibility/snapshots/{height}", func(w http.ResponseWriter, r *http.Request) {
		h, e := codec.Uint(r.PathValue("height"))
		if e != nil || r.URL.RawQuery != "" {
			bad(w)
			return
		}
		m, e := s.Snapshot(h)
		if e != nil {
			reply(w, 404, map[string]any{"error": "ERR_SNAPSHOT_NOT_FOUND"})
			return
		}
		reply(w, 200, m)
	})
	mux.HandleFunc("GET /v1/eligibility/snapshots/{height}/paths/{index}", func(w http.ResponseWriter, r *http.Request) {
		h, e := codec.Uint(r.PathValue("height"))
		if e != nil {
			bad(w)
			return
		}
		index, e := codec.Uint(r.PathValue("index"))
		if e != nil || r.URL.RawQuery != "" {
			bad(w)
			return
		}
		p, e := s.Path(h, index)
		if e != nil {
			reply(w, 404, map[string]any{"error": "ERR_PATH_NOT_FOUND"})
			return
		}
		reply(w, 200, p)
	})
	return mux
}
