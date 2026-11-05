package citizen

import (
	"civora/internal/codec"
	"civora/internal/eligibility"
	"civora/internal/partition"
	"encoding/json"
	"net/http"
	"strings"
)

func (h *Host) pollHTTP(w http.ResponseWriter, r *http.Request) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if r.Method != "GET" || len(parts) != 4 || parts[0] != "v1" || parts[1] != "polls" {
		return false
	}
	kind := parts[3]
	if kind != "group" && kind != "membership-path" && kind != "checkpoints" && kind != "context" {
		return false
	}
	reader, main, e := h.main()
	if e != nil {
		failure(w, 503, "ERR_INDEX_NOT_READY", true)
		return true
	}
	id := parts[2]
	f, ok := main.Polls[id]
	if !ok {
		failure(w, 404, "ERR_POLL_UNKNOWN", false)
		return true
	}
	if kind == "checkpoints" {
		items := []any{}
		for height := uint64(1); height <= mustUint(reader.Latest().Height); height++ {
			view, e := reader.At(height)
			if e != nil {
				continue
			}
			for _, tx := range view.Transactions {
				raw, _ := codec.Blob(tx.Bytes)
				var cmd partition.Command
				if json.Unmarshal(raw, &cmd) != nil || cmd.Type != "lane.checkpoint" {
					continue
				}
				var cp partition.Checkpoint
				if codec.Decode(cmd.Value, "partition_checkpoint", &cp) != nil {
					continue
				}
				var summary partition.Summary
				if json.Unmarshal(codec.Must(cp.Proof.Path.Entry.Value), &summary) != nil || summary.PollID != id {
					continue
				}
				proof, e := reader.TransactionProof(tx.ID)
				if e != nil {
					failure(w, 503, "ERR_PROOF_PENDING", true)
					return true
				}
				items = append(items, map[string]any{"checkpoint": cp, "publication": proof})
			}
		}
		WriteJSON(w, 200, map[string]any{"height": reader.Latest().Height, "accepted": main.Checkpoints[id], "items": items})
		return true
	}
	proof, e := reader.StateProof(mustUint(f.SourceHeight), "transport_polls", id)
	if e != nil {
		failure(w, 503, "ERR_PROOF_PENDING", true)
		return true
	}
	if kind == "context" {
		WriteJSON(w, 200, map[string]any{"context": f, "proof": proof})
		return true
	}
	poll := main.Engine.Governance.Polls[id]
	depth, _ := codec.Uint(f.Context.Depth)
	tree, e := eligibility.New(uint8(depth))
	if e != nil {
		failure(w, 422, "ERR_PROOF", false)
		return true
	}
	for _, v := range poll.Commitments {
		if e = tree.Append(v); e != nil {
			failure(w, 422, "ERR_PROOF", false)
			return true
		}
	}
	meta := tree.Metadata(f.Context.NetworkID, f.Context.Epoch, "poll-"+id, "semaphore-v4.14.5-depth-"+f.Context.Depth, f.SourceHeight)
	if meta.Root == nil || *meta.Root != f.Context.Root {
		failure(w, 422, "ERR_PROOF", false)
		return true
	}
	if kind == "group" {
		WriteJSON(w, 200, map[string]any{"snapshot": meta, "proof": proof})
		return true
	}
	index, e := codec.Uint(r.URL.Query().Get("leaf_index"))
	if e != nil {
		failure(w, 400, "ERR_ENCODING", false)
		return true
	}
	path, e := tree.Path(meta, index)
	if e != nil {
		failure(w, 404, "ERR_LEAF_UNKNOWN", false)
	} else {
		WriteJSON(w, 200, path)
	}
	return true
}
