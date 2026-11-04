// Package effects contains pure adapters for decision effects.
package effects

import (
	"civora/internal/codec"
	"civora/internal/ports"
)

type LedgerOnly struct{}

func (LedgerOnly) Evaluate(r ports.EffectRequest) (ports.EffectReceipt, error) {
	return ports.EffectReceipt{Profile: "ledger_only_v1", Reference: codec.Hash("effect/ledger", []any{r.NetworkID, r.ProposalID, r.Action, r.EffectiveMS})}, nil
}
