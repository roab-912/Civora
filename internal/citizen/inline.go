package citizen

import (
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/core"
	"civora/internal/eligibility"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func InlinePollHTTP(w http.ResponseWriter, r *http.Request, reader *Reader) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "v1" || parts[1] != "polls" || r.Method != "GET" {
		return false
	}
	kind := parts[3]
	if kind != "membership-path" && kind != "group" && kind != "tally" {
		return false
	}
	if e := reader.Refresh(); e != nil {
		WriteJSON(w, 503, map[string]any{"code": "ERR_PROOF", "message": "Index pending", "retryable": true})
		return true
	}
	if reader.Latest() == nil {
		return false
	}
	ledger, ok := application(reader.Latest().Machine).(*consensus.Ledger)
	if !ok {
		return false
	}
	m := ledger.Engine
	if m.Governance == nil {
		return false
	}
	cfg := m.Config
	poll := m.Governance.Polls[parts[2]]
	if poll == nil {
		http.NotFound(w, r)
		return true
	}
	if kind == "tally" {
		for h := m.Height; h > 0; h-- {
			v, e := reader.At(h)
			if e != nil {
				continue
			}
			for _, t := range v.Transactions {
				data, _ := codec.Blob(t.Bytes)
				var tx core.Transaction
				_ = json.Unmarshal(data, &tx)
				if tx.Body.Type != "poll.tally" {
					continue
				}
				var payload struct {
					PollID string `json:"poll_id"`
					Record string `json:"decryption_record"`
				}
				_ = json.Unmarshal(tx.Body.Payload, &payload)
				if payload.PollID == parts[2] {
					proof, e := reader.TransactionProof(t.ID)
					if e != nil {
						http.Error(w, "proof pending", 503)
						return true
					}
					WriteJSON(w, 200, map[string]any{"decryption_record": payload.Record, "publication": proof})
					return true
				}
			}
		}
		http.NotFound(w, r)
		return true
	}
	if poll.GroupRoot == nil {
		http.Error(w, "group pending", 409)
		return true
	}
	depth, _ := codec.Uint(cfg.Eligibility.CircuitDepth)
	tree, _ := eligibility.New(uint8(depth))
	for _, c := range poll.Commitments {
		if e := tree.Append(c); e != nil {
			http.Error(w, "group invalid", 422)
			return true
		}
	}
	meta := tree.Metadata(cfg.NetworkID, poll.ProtocolEpoch, "poll-"+parts[2], poll.EligibilitySuite, *poll.SnapshotHeight)
	if kind == "group" {
		WriteJSON(w, 200, map[string]any{"snapshot": meta, "items": poll.Commitments})
		return true
	}
	index, e := codec.Uint(r.URL.Query().Get("leaf_index"))
	if commitment := r.URL.Query().Get("commitment"); commitment != "" {
		index = ^uint64(0)
		for i, c := range poll.Commitments {
			if c == commitment {
				index = uint64(i)
				break
			}
		}
		e = nil
	}
	if e != nil {
		http.Error(w, "index invalid", 400)
		return true
	}
	path, e := tree.Path(meta, index)
	if e != nil {
		http.Error(w, fmt.Sprint("path unavailable"), 404)
	} else {
		WriteJSON(w, 200, path)
	}
	return true
}
