// Package upgrade provides development version gates, never update authority.
package upgrade

import (
	"civora/internal/codec"
	"fmt"
	"strconv"
)

type Module struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Operations []string `json:"operations"`
	Parameters struct {
		Format string `json:"format"`
	} `json:"parameters"`
	ImplementationSHA256 string   `json:"implementation_sha256"`
	Artifacts            []string `json:"artifacts"`
	Dependencies         []string `json:"dependencies"`
	RetirementPolicy     string   `json:"retirement_policy"`
}
type Registry struct {
	Epoch              string   `json:"protocol_epoch"`
	StateSchema        string   `json:"state_schema"`
	TransactionVersion string   `json:"transaction_version"`
	Modules            []Module `json:"modules"`
}
type Artifact struct {
	Target string `json:"target"`
	SHA256 string `json:"sha256"`
	Size   string `json:"size"`
}
type Plan struct {
	NetworkID               string     `json:"network_id"`
	FromEpoch               string     `json:"from_epoch"`
	ToEpoch                 string     `json:"to_epoch"`
	FromStateSchema         string     `json:"from_state_schema"`
	ToStateSchema           string     `json:"to_state_schema"`
	HashSuite               string     `json:"hash_suite"`
	Modules                 []Module   `json:"modules"`
	Artifacts               []Artifact `json:"artifacts"`
	ProvenanceSHA256        string     `json:"provenance_sha256"`
	SBOMSHA256              string     `json:"sbom_sha256"`
	ReproducibleBuildSHA256 string     `json:"reproducible_build_sha256"`
	ConformanceSHA256       string     `json:"conformance_sha256"`
	Chains                  []string   `json:"chains"`
	Activation              struct {
		NotBeforeMS  string `json:"not_before_ms"`
		ExpiresAtMS  string `json:"expires_at_ms"`
		NoticeMS     string `json:"notice_ms"`
		Prerequisite string `json:"prerequisite"`
	} `json:"activation"`
	Migration struct {
		ID                  string `json:"id"`
		PreconditionSHA256  string `json:"precondition_sha256"`
		PostconditionSHA256 string `json:"postcondition_sha256"`
		ReplaySHA256        string `json:"replay_sha256"`
		StopPoint           string `json:"stop_point"`
		FailureProcedure    string `json:"failure_procedure"`
	} `json:"migration"`
	OnCall             string `json:"on_call"`
	PublicNotice       string `json:"public_notice"`
	TransactionVersion string `json:"transaction_version"`
}
type Height struct {
	ChainID string `json:"chain_id"`
	Height  string `json:"height"`
}
type Instance struct {
	PlanID           string   `json:"plan_id"`
	Heights          []Height `json:"heights"`
	ArmWindowStartMS string   `json:"arm_window_start_ms"`
	ArmWindowEndMS   string   `json:"arm_window_end_ms"`
	ExpiresAtMS      string   `json:"expires_at_ms"`
}
type Armed struct {
	PlanID        string   `json:"plan_id"`
	Plan          Plan     `json:"plan"`
	InstanceID    string   `json:"instance_id"`
	Instance      Instance `json:"instance"`
	Authorization string   `json:"authorization"`
}

func (r Registry) Validate() error {
	e, err := codec.Uint(r.Epoch)
	if err != nil || e == 0 {
		return fmt.Errorf("invalid epoch")
	}
	if r.StateSchema != "1" || (r.TransactionVersion != "1" && r.TransactionVersion != "2") || (r.TransactionVersion == "1" && e != 1) {
		return fmt.Errorf("incompatible transaction/state version")
	}
	ids := map[string]bool{}
	active := map[string]bool{}
	for _, m := range r.Modules {
		if ids[m.ID] {
			return fmt.Errorf("duplicate module")
		}
		ids[m.ID] = true
		if m.Status == "active" {
			for _, op := range m.Operations {
				if active[op] {
					return fmt.Errorf("ambiguous active module")
				}
				active[op] = true
			}
		}
	}
	for _, m := range r.Modules {
		deps := map[string]bool{}
		for _, d := range m.Dependencies {
			if !ids[d] || d == m.ID || deps[d] {
				return fmt.Errorf("invalid dependency")
			}
			deps[d] = true
		}
	}
	// The development core only implements SHA-256 state commitments. A
	// registry must never silently choose it after its authorization is lost.
	hashActive := false
	for _, m := range r.Modules {
		for _, op := range m.Operations {
			if op == "state.hash" && m.Status == "active" {
				if m.Parameters.Format != "sha256" {
					return fmt.Errorf("ERR_UNSUPPORTED_HASH_SUITE")
				}
				hashActive = true
			}
		}
	}
	if !hashActive {
		return fmt.Errorf("ERR_SUITE: no active state hash")
	}
	return nil
}
func (r Registry) Authorize(epoch, operation, moduleID string, historical bool) error {
	if epoch != r.Epoch {
		return fmt.Errorf("ERR_EPOCH")
	}
	for _, m := range r.Modules {
		if m.ID != moduleID {
			continue
		}
		for _, op := range m.Operations {
			if op == operation && (m.Status == "active" || historical && m.Status == "legacy_verify_only") {
				return nil
			}
		}
	}
	return fmt.Errorf("ERR_SUITE")
}
func (r Registry) AccountSuite() (string, error) {
	for _, m := range r.Modules {
		for _, op := range m.Operations {
			if op == "account.sign" && m.Status == "active" {
				if m.Parameters.Format != "ed25519" {
					return "", fmt.Errorf("unsupported account suite")
				}
				return m.ID, nil
			}
		}
	}
	return "", fmt.Errorf("ERR_SUITE")
}

func (r Registry) Next(p Plan) (Registry, error) {
	from, e := codec.Uint(p.FromEpoch)
	if e != nil {
		return r, e
	}
	to, e := codec.Uint(p.ToEpoch)
	if e != nil || from == ^uint64(0) || to != from+1 || p.FromEpoch != r.Epoch || p.FromStateSchema != r.StateSchema {
		return r, fmt.Errorf("ERR_EPOCH")
	}
	next := Registry{p.ToEpoch, p.ToStateSchema, p.TransactionVersion, p.Modules}
	if e = next.Validate(); e != nil {
		return r, e
	}
	byID := map[string]Module{}
	for _, m := range next.Modules {
		byID[m.ID] = m
	}
	for _, old := range r.Modules {
		m, ok := byID[old.ID]
		if !ok {
			return r, fmt.Errorf("module history removed")
		}
		a, b := old, m
		a.Status = ""
		b.Status = ""
		if string(codec.Must(a)) != string(codec.Must(b)) {
			return r, fmt.Errorf("immutable module changed")
		}
		if old.Status == "revoked" && m.Status != "revoked" || old.Status == "legacy_verify_only" && (m.Status == "active" || m.Status == "prepared") || old.Status == "active" && m.Status == "prepared" {
			return r, fmt.Errorf("ERR_REVOKED_ROLLBACK")
		}
	}
	return next, nil
}

// CheckFixture checks structural preconditions, not governance/quorum proofs.
func (a Armed) CheckFixture(network, chain string) error {
	if a.Authorization != "development_fixture" || a.Plan.NetworkID != network || a.PlanID != codec.Hash("upgrade/plan", a.Plan) || a.Instance.PlanID != a.PlanID || a.InstanceID != codec.Hash("upgrade/instance", a.Instance) {
		return fmt.Errorf("invalid fixture activation identity")
	}
	if len(a.Plan.Chains) != 1 || a.Plan.Chains[0] != chain || len(a.Instance.Heights) != 1 || a.Instance.Heights[0].ChainID != chain {
		return fmt.Errorf("v0.1 fixtures support one chain")
	}
	h, e := codec.Uint(a.Instance.Heights[0].Height)
	if e != nil || h == 0 {
		return fmt.Errorf("invalid activation height")
	}
	values := []string{a.Plan.Activation.NotBeforeMS, a.Plan.Activation.ExpiresAtMS, a.Plan.Activation.NoticeMS, a.Instance.ArmWindowStartMS, a.Instance.ArmWindowEndMS, a.Instance.ExpiresAtMS}
	v := make([]uint64, len(values))
	for i, s := range values {
		v[i], e = codec.Uint(s)
		if e != nil {
			return e
		}
	}
	if v[0] >= v[1] || v[3] > v[4] || v[4] >= v[5] || v[5] > v[1] || v[3] < v[0] || v[2] > v[4]-v[3] {
		return fmt.Errorf("invalid activation window")
	}
	for _, art := range a.Plan.Artifacts {
		if _, e = codec.Uint(art.Size); e != nil {
			return e
		}
	}
	return nil
}
func (a Armed) At(height uint64) bool {
	return a.Instance.Heights[0].Height == strconv.FormatUint(height, 10)
}
