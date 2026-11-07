package partition

import (
	"civora/internal/codec"
	"civora/internal/electionguard"
	"civora/internal/recovery"
	"fmt"
	abci "github.com/cometbft/cometbft/abci/types"
	"sort"
)

func (m *Machine) TransactionPriority(raw []byte) int { return recovery.Priority(raw) }
func (m *Machine) pollHeld(f Frozen) bool {
	if m.Security == nil {
		return false
	}
	return m.Security.ModuleBlocked(electionguard.Suite) || m.Security.ModuleBlocked("semaphore-v4.14.5-depth-"+f.Context.Depth) || m.Security.Blocked("profile", m.Config.Recovery.Profile, m.Time()) || m.Security.Blocked("poll", f.Context.PollID, m.Time()) || m.Security.Blocked("effect", f.ProposalID, m.Time())
}
func (m *Machine) acceptHolds(orders []recovery.Bundle) error {
	if len(orders) > recovery.HoldBudget {
		return fmt.Errorf("ERR_SECURITY_BUDGET")
	}
	for _, b := range orders {
		if m.Security == nil {
			return fmt.Errorf("ERR_RECOVERY_NOT_CONFIGURED")
		}
		if err := m.Security.Accept(m.Config.Recovery, b, m.Time()); err != nil {
			return err
		}
		m.emitHold(b.Order)
	}
	ids := []string{}
	for id := range m.Polls {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := m.Polls[id]
		if !p.Interrupted && m.pollHeld(p.Frozen) {
			p.Interrupted = true
		}
	}
	return nil
}
func (m *Machine) emitHold(o recovery.Order) {
	// Use the same public event shape as ledger events, with a domain-bound id.
	// No endpoint, device identity or private key is included.
	m.events = append(m.events, abci.Event{Type: "security.hold", Attributes: []abci.EventAttribute{{Key: "order_id", Value: codec.Hash("recovery/order", o), Index: true}, {Key: "counter", Value: o.Counter, Index: true}}})
}
