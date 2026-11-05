package citizen

import (
	"civora/internal/archive"
	"civora/internal/codec"
	"civora/internal/electionaudit"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type RecordIndex struct {
	Format    string        `json:"format"`
	NetworkID string        `json:"network_id"`
	PollID    string        `json:"poll_id"`
	Dossier   string        `json:"dossier_id"`
	Record    archive.Ref   `json:"record"`
	Objects   []archive.Ref `json:"objects"`
}

func (h *Host) Extra(w http.ResponseWriter, r *http.Request) bool {
	if h.servicesHTTP(w, r) || h.pollHTTP(w, r) {
		return true
	}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) == 4 && parts[0] == "v1" && parts[1] == "polls" && parts[3] == "record" && r.Method == "GET" {
		id := parts[2]
		record, e := h.Election(id)
		if e != nil {
			failure(w, 503, "ERR_RECORD_INCOMPLETE", true)
			return true
		}
		// Export observed evidence even if replay exposes a validator error.
		// Only the independent auditor may issue an attestation.
		var result struct {
			Dossier string `json:"dossier_id"`
		}
		_ = json.Unmarshal(codec.Must(record.Result.Path.Entry.Value), &result)
		ref, e := h.API.Objects.Put(codec.Must(record))
		if e != nil {
			failure(w, 503, "ERR_ARCHIVE_INTEGRITY", true)
			return true
		}
		WriteJSON(w, 200, RecordIndex{"civic-record-index-1", h.Manifest.NetworkID, id, result.Dossier, ref, RecordReferences(record)})
		return true
	}
	return false
}
func DownloadRecord(m Manifest, pollID string, store archive.Store) (electionaudit.Record, error) {
	var last error
	for _, o := range m.Main.Origins {
		raw, e := GetBounded(Client(), strings.TrimRight(o.API, "/")+"/v1/polls/"+pollID+"/record", 1024*1024)
		if e != nil {
			last = e
			continue
		}
		var index RecordIndex
		if e = codec.Decode(raw, "citizen_record_index", &index); e != nil || index.NetworkID != m.NetworkID || index.PollID != pollID {
			last = fmt.Errorf("foreign record index")
			continue
		}
		if e = FetchObject(m.Main, store, index.Record); e != nil {
			last = e
			continue
		}
		raw, e = store.Get(index.Record)
		if e != nil {
			last = e
			continue
		}
		var record electionaudit.Record
		if e = codec.Decode(raw, "citizen_election_record", &record); e != nil || record.Genesis.SHA256 != m.Main.Genesis.SHA256 {
			last = fmt.Errorf("untrusted record genesis")
			continue
		}
		valid := true
		for _, ref := range RecordReferences(record) {
			if e = FetchObject(m.Main, store, ref); e != nil {
				last = e
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		report, e := electionaudit.Verify(record, store, m.Main.Genesis.SHA256)
		if e != nil || report.Dossier != index.Dossier {
			last = fmt.Errorf("record replay failed: %v", e)
			continue
		}
		return record, nil
	}
	return electionaudit.Record{}, last
}
