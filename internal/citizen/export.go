package citizen

import (
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/codec"
	"civora/internal/electionaudit"
	"civora/internal/partition"
	"civora/internal/replica"
	"encoding/base64"
	"encoding/json"
	"fmt"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"os"
	"path/filepath"
)

func (h *Host) Election(pollID string) (electionaudit.Record, error) {
	if e := h.Refresh(); e != nil {
		return electionaudit.Record{}, e
	}
	main := h.Readers[h.Manifest.Main.ChainID]
	if main == nil || main.Latest() == nil {
		return electionaudit.Record{}, fmt.Errorf("main chain unavailable")
	}
	latest := main.Latest()
	machine, ok := application(latest.Machine).(*partition.Main)
	if !ok {
		return electionaudit.Record{}, fmt.Errorf("partition coordinator required")
	}
	f, ok := machine.Polls[pollID]
	if !ok {
		return electionaudit.Record{}, fmt.Errorf("poll unknown")
	}
	result, ok := machine.Results[pollID]
	if !ok {
		return electionaudit.Record{}, fmt.Errorf("global result not yet available")
	}
	resultHeight, _ := codec.Uint(result.PublishedHeight)
	if resultHeight == 0 {
		for n := uint64(1); n <= machine.Height(); n++ {
			v, _ := main.At(n)
			for _, e := range v.Entries {
				if e.Table == "transport_results" && string(codec.Must(e.Key)) == string(codec.Must(pollID)) {
					resultHeight = n
					break
				}
			}
			if resultHeight > 0 {
				break
			}
		}
	}
	sourceHeight, _ := codec.Uint(f.SourceHeight)
	opening, e := main.Membership(sourceHeight, "transport_polls", pollID)
	if e != nil {
		return electionaudit.Record{}, e
	}
	resultProof, e := main.StateProof(resultHeight, "transport_results", pollID)
	if e != nil {
		return electionaudit.Record{}, e
	}
	genRef, e := h.API.Objects.Put(main.Genesis.Raw)
	if e != nil {
		return electionaudit.Record{}, e
	}
	record := electionaudit.Record{Format: "civic-election-1", Genesis: genRef, Frozen: f, Opening: opening, Result: resultProof, Lanes: []electionaudit.Lane{}}
	if machine.Height() < 2 {
		return record, fmt.Errorf("status header pending")
	}
	record.Status, e = main.StateProof(machine.Height()-1, "transport_results", pollID)
	if e != nil {
		return record, e
	}
	for index, descriptor := range f.Lanes {
		reader := h.Readers[descriptor.ChainID]
		if reader == nil || reader.Latest() == nil {
			return electionaudit.Record{}, fmt.Errorf("lane %d archive missing", index)
		}
		m, ok := application(reader.Latest().Machine).(*partition.Machine)
		if !ok {
			return electionaudit.Record{}, fmt.Errorf("wrong lane application")
		}
		poll := m.Polls[pollID]
		if poll == nil || !m.Summary(pollID).ProcessingSealed {
			return electionaudit.Record{}, fmt.Errorf("lane not sealed")
		}
		lane := electionaudit.Lane{LaneID: fmt.Sprint(index), Checkpoints: []partition.Checkpoint{}, Batches: []electionaudit.Batch{}}
		lane.Accepted, e = main.StateProof(resultHeight, "transport_checkpoints", []any{pollID, lane.LaneID})
		if e != nil {
			return electionaudit.Record{}, e
		}
		for n := sourceHeight; n <= resultHeight; n++ {
			view, e := main.At(n)
			if e != nil {
				return electionaudit.Record{}, e
			}
			for _, t := range view.Transactions {
				raw, _ := codec.Blob(t.Bytes)
				var c partition.Command
				if json.Unmarshal(raw, &c) != nil || c.Type != "lane.checkpoint" {
					continue
				}
				var cp partition.Checkpoint
				if json.Unmarshal(c.Value, &cp) != nil {
					continue
				}
				var s partition.Summary
				if json.Unmarshal(codec.Must(cp.Proof.Path.Entry.Value), &s) != nil || s.PollID != pollID || s.Lane != lane.LaneID {
					continue
				}
				lane.Checkpoints = append(lane.Checkpoints, cp)
			}
		}
		for _, stored := range poll.Batches {
			start, _ := codec.Uint(stored.Reference.Start)
			count, _ := codec.Uint(stored.Reference.Count)
			if start+count > uint64(len(poll.Deposits)) || count == 0 {
				return electionaudit.Record{}, fmt.Errorf("archive batch bounds")
			}
			b := electionaudit.Batch{Reference: stored.Reference, Envelopes: []archive.Ref{}}
			for _, d := range poll.Deposits[start : start+count] {
				bytes, e := codec.Blob(d.Envelope)
				if e != nil {
					return electionaudit.Record{}, e
				}
				ref, e := h.API.Objects.Put(bytes)
				if e != nil {
					return electionaudit.Record{}, e
				}
				b.Envelopes = append(b.Envelopes, ref)
			}
			height, _ := codec.Uint(poll.Deposits[start].Height)
			native, e := reader.Source.Bundle(height)
			if e != nil {
				return electionaudit.Record{}, e
			}
			block, e := chain.Decode(native)
			if e != nil {
				return electionaudit.Record{}, e
			}
			found := false
			for txPosition, data := range block.Data.Txs {
				var c partition.Command
				if json.Unmarshal(data, &c) != nil || c.Type != "partition.admit" {
					continue
				}
				var batch partition.Batch
				if json.Unmarshal(c.Value, &batch) != nil {
					continue
				}
				ref, e := partition.Reference(f.Context, lane.LaneID, stored.Reference.Start, batch.Envelopes)
				if e != nil || ref.ID != stored.Reference.ID {
					continue
				}
				proof := block.Data.Txs.Proof(txPosition)
				raw, e := cmtjson.Marshal(proof)
				if e != nil {
					return electionaudit.Record{}, e
				}
				b.SignedHeader = native.SignedHeader
				b.TransactionProof = base64.RawURLEncoding.EncodeToString(raw)
				found = true
				break
			}
			if !found {
				return electionaudit.Record{}, fmt.Errorf("original admission transaction missing")
			}
			lane.Batches = append(lane.Batches, b)
		}
		record.Lanes = append(record.Lanes, lane)
	}
	if certificate, found := machine.Certificates[pollID]; found {
		h, _ := codec.Uint(certificate.Height)
		p, e := main.StateProof(h, "poll_certificates", pollID)
		if e == nil {
			record.Certificate = &p
		}
	}
	return record, nil
}
func mustHeight(s string) uint64 { n, _ := codec.Uint(s); return n }
func RecordReferences(r electionaudit.Record) []archive.Ref {
	refs := []archive.Ref{r.Genesis}
	seen := map[string]bool{r.Genesis.SHA256: true}
	for _, lane := range r.Lanes {
		for _, batch := range lane.Batches {
			for _, ref := range batch.Envelopes {
				if !seen[ref.SHA256] {
					refs = append(refs, ref)
					seen[ref.SHA256] = true
				}
			}
		}
	}
	return refs
}
func ExportRecord(path string, r electionaudit.Record, source archive.Store) error {
	if e := os.MkdirAll(path, 0700); e != nil {
		return e
	}
	destination := archive.Store{Directory: path}
	for _, ref := range RecordReferences(r) {
		raw, e := source.Get(ref)
		if e != nil {
			return e
		}
		if _, e = destination.Put(raw); e != nil {
			return e
		}
	}
	return replica.Persist(path, filepath.Join(path, "record.json"), codec.Must(r), nil)
}
