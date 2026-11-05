// Package electionaudit verifies a selective public record without its node DB.
package electionaudit

import (
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/electionguard"
	"civora/internal/partition"
	"civora/internal/state"
	"fmt"
)

type Batch struct {
	Reference        partition.BatchRef `json:"reference"`
	Envelopes        []archive.Ref      `json:"envelopes"`
	SignedHeader     string             `json:"signed_header"`
	TransactionProof string             `json:"transaction_proof"`
}
type Lane struct {
	LaneID      string                 `json:"lane_id"`
	Accepted    consensus.WitnessProof `json:"accepted"`
	Checkpoints []partition.Checkpoint `json:"checkpoints"`
	Batches     []Batch                `json:"batches"`
}
type Record struct {
	Format      string                  `json:"format"`
	Genesis     archive.Ref             `json:"genesis"`
	Frozen      partition.Frozen        `json:"frozen"`
	Opening     consensus.StateProof    `json:"opening"`
	Lanes       []Lane                  `json:"lanes"`
	Result      consensus.WitnessProof  `json:"result"`
	Certificate *consensus.WitnessProof `json:"certificate,omitempty"`
	Status      consensus.WitnessProof  `json:"status"`
}
type Report struct {
	NetworkID string `json:"network_id"`
	PollID    string `json:"poll_id"`
	Dossier   string `json:"dossier_id"`
	Admitted  string `json:"admitted"`
	Counted   string `json:"counted"`
	Yes       string `json:"yes"`
	No        string `json:"no"`
	State     string `json:"state"`
	Verifier  string `json:"verifier"`
	Verified  bool   `json:"verified"`
	AsOf      string `json:"as_of_height"`
}
type Loader interface {
	Get(archive.Ref) ([]byte, error)
}

func Verify(record Record, objects Loader, expectedGenesis string) (Report, error) {
	fail := func(e error) (Report, error) { return Report{}, e }
	if expectedGenesis == "" || record.Genesis.SHA256 != expectedGenesis {
		return fail(fmt.Errorf("authenticated genesis fingerprint required"))
	}
	if e := codec.Decode(codec.Must(record), "citizen_election_record", new(Record)); e != nil {
		return fail(e)
	}
	raw, e := objects.Get(record.Genesis)
	if e != nil {
		return fail(e)
	}
	gen, e := chain.ParseGenesis(raw)
	if e != nil {
		return fail(e)
	}
	f := record.Frozen
	if e = f.Validate(gen.Doc.ChainID); e != nil {
		return fail(e)
	}
	sourceHeight, _ := codec.Uint(record.Opening.Height)
	if _, e = consensus.VerifyState(gen.TrustAt(sourceHeight+1), record.Opening); e != nil {
		return fail(e)
	}
	if record.Opening.Height != f.SourceHeight || record.Opening.Path.Entry.Table != "transport_polls" || string(codec.Must(record.Opening.Path.Entry.Key)) != string(codec.Must(f.Context.PollID)) || string(codec.Must(record.Opening.Path.Entry.Value)) != string(codec.Must(f)) {
		return fail(fmt.Errorf("opening/context linkage"))
	}
	resultH, _ := codec.Uint(record.Result.Height)
	if e = consensus.VerifyWitness(gen.TrustAt(resultH+1), record.Result); e != nil {
		return fail(e)
	}
	if !record.Result.Path.Exists || record.Result.Path.Entry.Table != "transport_results" || string(codec.Must(record.Result.Path.Entry.Key)) != string(codec.Must(f.Context.PollID)) {
		return fail(fmt.Errorf("result state path"))
	}
	var result partition.Provisional
	if e = codec.Decode(codec.Must(record.Result.Path.Entry.Value), "partition_provisional", &result); e != nil {
		return fail(e)
	}
	if len(record.Lanes) != len(f.Lanes) {
		return fail(fmt.Errorf("missing lane"))
	}
	verified := map[string]*partition.Verified{}
	admitted := uint64(0)
	for i, lane := range record.Lanes {
		if lane.LaneID != fmt.Sprint(i) || len(lane.Checkpoints) == 0 {
			return fail(fmt.Errorf("lane ordering/checkpoints"))
		}
		var previous *partition.Verified
		for _, cp := range lane.Checkpoints {
			previous, e = partition.VerifyCheckpoint(f, cp, previous)
			if e != nil {
				return fail(e)
			}
		}
		summary := previous.Summary
		if summary.Lane != lane.LaneID || !summary.ProcessingSealed {
			return fail(fmt.Errorf("lane incomplete"))
		}
		if lane.Accepted.Height != record.Result.Height {
			return fail(fmt.Errorf("accepted checkpoint height"))
		}
		if e = consensus.VerifyWitness(gen.TrustAt(resultH+1), lane.Accepted); e != nil {
			return fail(e)
		}
		if !lane.Accepted.Path.Exists || lane.Accepted.Path.Entry.Table != "transport_checkpoints" || string(codec.Must(lane.Accepted.Path.Entry.Key)) != string(codec.Must([]any{f.Context.PollID, lane.LaneID})) || string(codec.Must(lane.Accepted.Path.Entry.Value)) != string(codec.Must(previous)) {
			return fail(fmt.Errorf("checkpoint not committed by main chain"))
		}
		v, el, e := f.Election()
		if e != nil {
			return fail(e)
		}
		aggregate := v.Zero()
		spent, ballots, ciphertexts := map[string]string{}, map[string]bool{}, map[string]bool{}
		statusEntries := []state.Entry{}
		allBytes, batchBytes := [][]byte{}, [][]byte{}
		counted, position := uint64(0), uint64(0)
		lastHeight, lastPosition := uint64(0), uint64(0)
		for _, b := range lane.Batches {
			if len(b.Envelopes) == 0 {
				return fail(fmt.Errorf("empty archive batch"))
			}
			envelopes := [][]byte{}
			for _, ref := range b.Envelopes {
				data, e := objects.Get(ref)
				if e != nil {
					return fail(e)
				}
				envelopes = append(envelopes, data)
			}
			start, _ := codec.Uint(b.Reference.Start)
			if start != position {
				return fail(fmt.Errorf("batch gap/reordering"))
			}
			for j, data := range envelopes {
				path, e := partition.Inclusion(envelopes, j)
				if e != nil {
					return fail(e)
				}
				receipt := partition.Receipt{SignedHeader: b.SignedHeader, TransactionProof: b.TransactionProof, Reference: b.Reference, Envelope: partitiondevEncode(data), Index: fmt.Sprint(j), Inclusion: path}
				if e = partition.VerifyReceipt(f, receipt); e != nil {
					return fail(e)
				}
			}
			header, e := consensus.VerifyHeader(f.Lanes[i], b.SignedHeader)
			if e != nil {
				return fail(e)
			}
			txPosition, e := nativePosition(b.TransactionProof)
			if e != nil {
				return fail(e)
			}
			if uint64(header.Height) < lastHeight || uint64(header.Height) == lastHeight && txPosition <= lastPosition {
				return fail(fmt.Errorf("admission order is not native order"))
			}
			lastHeight, lastPosition = uint64(header.Height), txPosition
			for _, data := range envelopes {
				var env electionguard.ScaleEnvelope
				if e = codec.Decode(data, "scale_envelope", &env); e != nil {
					return fail(e)
				}
				if e = partition.VerifyAdmission(f, lane.LaneID, data); e != nil {
					return fail(e)
				}
				status := "invalid"
				ballot, err := v.VerifyScale(el, f.Context, data)
				if err == nil {
					cipherID := codec.Hash("vote/ciphertext", ballot.Contests[0].Selections[0].Cipher)
					if spent[env.Membership.Nullifier] != "" || ballots[ballot.ID] || ciphertexts[cipherID] {
						status = "duplicate"
					} else {
						aggregate, e = v.Add(aggregate, ballot)
						if e != nil {
							return fail(e)
						}
						spent[env.Membership.Nullifier] = archive.Digest(data)
						ballots[ballot.ID] = true
						ciphertexts[cipherID] = true
						counted++
						status = "counted"
					}
				}
				statusEntries = append(statusEntries, state.Entry{Table: "status", Key: fmt.Sprint(position), Value: map[string]any{"digest": archive.Digest(data), "status": status}})
				position++
				allBytes = append(allBytes, data)
			}
			batchBytes = append(batchBytes, codec.Must(b.Reference))
		}
		spentEntries := []state.Entry{}
		for key, value := range spent {
			spentEntries = append(spentEntries, state.Entry{Table: "spent", Key: key, Value: value})
		}
		if summary.Admitted != fmt.Sprint(position) || summary.Processed != fmt.Sprint(position) || summary.Counted != fmt.Sprint(counted) || summary.Aggregate != aggregate || summary.AdmissionRoot != partition.LogRoot(allBytes) || summary.Batches != fmt.Sprint(len(batchBytes)) || summary.BatchRoot != partition.LogRoot(batchBytes) || summary.StatusRoot != state.Root(statusEntries) || summary.SpentRoot != state.Root(spentEntries) {
			return fail(fmt.Errorf("replayed lane differs from sealed state"))
		}
		admitted += position
		verified[lane.LaneID] = previous
	}
	aggregate, count, e := partition.Global(f, verified)
	if e != nil {
		return fail(e)
	}
	if result.Aggregate != aggregate || result.Count != count {
		return fail(fmt.Errorf("global aggregate/count mismatch"))
	}
	if count == "0" {
		if result.Yes != "0" || result.No != "0" || result.Tally != "" {
			return fail(fmt.Errorf("empty result"))
		}
	} else {
		v, el, e := f.Election()
		if e != nil {
			return fail(e)
		}
		bundle, e := electionguard.Decode(result.Tally, electionguard.Tally)
		if e != nil {
			return fail(e)
		}
		yes, no, e := v.Tally(el, f.Context.PollID, aggregate, count, bundle.Files["tally.json"])
		if e != nil || yes != result.Yes || no != result.No {
			return fail(fmt.Errorf("global decryption invalid"))
		}
	}
	dossier, e := partition.Dossier(f, result, verified)
	if e != nil {
		return fail(e)
	}
	if f.Audit != nil && result.Dossier != dossier {
		return fail(fmt.Errorf("audit dossier commitment"))
	}
	if e = verifyCertificate(gen, record, result, dossier); e != nil {
		return fail(e)
	}
	status := result.State
	if record.Certificate != nil {
		status = "certified"
	}
	current := record.Status
	curH, _ := codec.Uint(current.Height)
	if curH < resultH {
		return fail(fmt.Errorf("status predates dossier"))
	}
	if e = consensus.VerifyWitness(gen.TrustAt(curH+1), current); e != nil {
		return fail(e)
	}
	if !current.Path.Exists || current.Path.Entry.Table != "transport_results" || string(codec.Must(current.Path.Entry.Key)) != string(codec.Must(f.Context.PollID)) {
		return fail(fmt.Errorf("status state path"))
	}
	var latest partition.Provisional
	if e = codec.Decode(codec.Must(current.Path.Entry.Value), "partition_provisional", &latest); e != nil {
		return fail(e)
	}
	latestState := latest.State
	latest.State = result.State
	if string(codec.Must(latest)) != string(codec.Must(result)) {
		return fail(fmt.Errorf("dossier changed"))
	}
	if latestState == "contested" || latestState == "failed_certification" {
		status = latestState
	}
	if status == "certified" && record.Certificate != nil && curH < mustUint(record.Certificate.Height) {
		return fail(fmt.Errorf("status predates certificate"))
	}
	return Report{f.Context.NetworkID, f.Context.PollID, dossier, fmt.Sprint(admitted), count, result.Yes, result.No, status, partition.GoAudit, true, current.Height}, nil
}
func mustUint(s string) uint64           { n, _ := codec.Uint(s); return n }
func partitiondevEncode(b []byte) string { return encode(b) }
