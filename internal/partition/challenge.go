package partition

import (
	"civora/internal/codec"
	"civora/internal/electionguard"
	"civora/internal/state"
	"encoding/json"
	"fmt"
)

type Evidence struct {
	Lane      string        `json:"lane_id"`
	Index     string        `json:"index"`
	Envelope  string        `json:"envelope"`
	Admission []string      `json:"admission_path"`
	Status    state.Witness `json:"status"`
}
type Challenge struct {
	PollID string     `json:"poll_id"`
	Kind   string     `json:"kind"`
	Items  []Evidence `json:"items"`
}
type ChallengeRecord struct {
	ID     string    `json:"id"`
	Height string    `json:"height"`
	Time   string    `json:"time_ms"`
	Proof  Challenge `json:"proof"`
}

func VerifyChallenge(f Frozen, checkpoints map[string]*Verified, c Challenge) error {
	if f.Audit == nil || c.PollID != f.Context.PollID {
		return fmt.Errorf("challenge audit context")
	}
	if e := codec.Decode(codec.Must(c), "audit_challenge", new(Challenge)); e != nil {
		return e
	}
	statuses := []string{}
	raws := [][]byte{}
	envs := []electionguard.ScaleEnvelope{}
	for _, item := range c.Items {
		cp := checkpoints[item.Lane]
		if cp == nil || !cp.Summary.ProcessingSealed {
			return fmt.Errorf("challenge final lane missing")
		}
		index, e := codec.Uint(item.Index)
		admitted, _ := codec.Uint(cp.Summary.Admitted)
		if e != nil || index >= admitted {
			return fmt.Errorf("challenge index")
		}
		raw, e := codec.Blob(item.Envelope)
		if e != nil || len(raw) > 256*1024 {
			return fmt.Errorf("challenge envelope")
		}
		if e = VerifyInclusion(cp.Summary.AdmissionRoot, raw, index, admitted, item.Admission); e != nil {
			return e
		}
		if e = state.VerifyWitness(cp.Summary.StatusRoot, item.Status); e != nil {
			return e
		}
		var status struct {
			Digest string `json:"digest"`
			Status string `json:"status"`
		}
		if e = json.Unmarshal(codec.Must(item.Status.Entry.Value), &status); e != nil {
			return e
		}
		if !item.Status.Exists || item.Status.Entry.Table != "status" || string(codec.Must(item.Status.Entry.Key)) != string(codec.Must(item.Index)) || status.Digest != digest(raw) || string(codec.Must(item.Status.Entry.Value)) != string(codec.Must(map[string]any{"digest": status.Digest, "status": status.Status})) {
			return fmt.Errorf("challenge status linkage")
		}
		var env electionguard.ScaleEnvelope
		_ = json.Unmarshal(raw, &env)
		statuses = append(statuses, status.Status)
		raws = append(raws, raw)
		envs = append(envs, env)
	}
	v, election, e := f.Election()
	if e != nil {
		return e
	}
	valid := false
	switch c.Kind {
	case "counted_invalid":
		if len(c.Items) == 1 && statuses[0] == "counted" {
			_, err := v.VerifyScale(election, f.Context, raws[0])
			valid = err != nil
		}
	case "wrong_route":
		if len(c.Items) == 1 && statuses[0] == "counted" {
			var env electionguard.ScaleEnvelope
			if codec.Decode(raws[0], "scale_envelope", &env) == nil {
				p, _ := codec.Uint(f.Context.Partitions)
				lane, err := electionguard.Lane(f.Context.NetworkID, f.Context.PollID, env.Membership.Nullifier, p)
				valid = err != nil || env.Lane != c.Items[0].Lane || fmt.Sprint(lane) != c.Items[0].Lane
			}
		}
	case "double_counted":
		if len(c.Items) == 2 && statuses[0] == "counted" && statuses[1] == "counted" && envs[0].Membership.Nullifier != "" && envs[0].Membership.Nullifier == envs[1].Membership.Nullifier {
			valid = c.Items[0].Lane != c.Items[1].Lane || c.Items[0].Index != c.Items[1].Index
		}
	case "valid_omitted":
		// A valid proof cannot be classified cryptographically invalid. A
		// "duplicate" status needs the full replay: absence of a nullifier
		// alone does not exclude reuse of a ballot ID by another envelope.
		if len(c.Items) == 1 && statuses[0] == "invalid" {
			_, err := v.VerifyScale(election, f.Context, raws[0])
			valid = err == nil
		}
	}
	if !valid {
		return fmt.Errorf("challenge does not establish an error")
	}
	return nil
}
func (m *Main) challenge(c Challenge, height, timeMS string) error {
	f, ok := m.Polls[c.PollID]
	if !ok {
		return fmt.Errorf("challenge poll")
	}
	r, ok := m.Results[c.PollID]
	if !ok {
		return fmt.Errorf("challenge before global dossier")
	}
	id := codec.Hash("audit/challenge", c)
	if old, ok := m.Challenges[c.PollID]; ok {
		if old.ID == id {
			return nil
		}
		return fmt.Errorf("poll already contested")
	}
	if e := VerifyChallenge(f, m.Checkpoints[c.PollID], c); e != nil {
		return e
	}
	r.State = "contested"
	m.Results[c.PollID] = r
	m.Challenges[c.PollID] = ChallengeRecord{id, height, timeMS, c}
	m.Engine.Governance.BlockPartitionEffect(c.PollID)
	m.emit("audit.challenge.valid", c.PollID)
	return nil
}
