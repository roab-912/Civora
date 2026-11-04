package eligibility

import "civora/internal/ports"

// InlineProvider reconstructs the sorted opening group specified by §7.2.
// The incremental account tree is separate: it preserves insertion positions
// and tombstones for the future scale_1 profile (§14.3).
type InlineProvider struct{}

func (InlineProvider) Snapshot(r ports.EligibilityRequest) (ports.EligibilityGroup, error) {
	t, _ := New(16)
	values := make([]string, len(r.Accounts))
	for i, a := range r.Accounts {
		values[i] = a.Commitment
	}
	if e := t.Append(values...); e != nil {
		return ports.EligibilityGroup{}, e
	}
	return ports.EligibilityGroup{Root: t.Root(), Commitments: values}, nil
}

type Config struct {
	CircuitDepth string `json:"circuit_depth"`
	SuiteID      string `json:"suite_id"`
}
