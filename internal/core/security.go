package core

import (
	"civora/internal/codec"
	"civora/internal/recovery"
	"fmt"
	"sort"
)

func (e *Engine) CheckSecurity(raw []byte) error {
	b, err := recovery.DecodeCommand(raw)
	if err != nil {
		return err
	}
	return e.Clone().AcceptSecurity([]recovery.Bundle{b}, e.Time, func(string, string) {})
}

// AcceptSecurity runs before the block's deadline transitions and transactions.
func (e *Engine) AcceptSecurity(orders []recovery.Bundle, now uint64, emit func(string, string)) error {
	if len(orders) > recovery.HoldBudget {
		return fmt.Errorf("ERR_SECURITY_BUDGET")
	}
	for _, b := range orders {
		o := b.Order
		if o.ScopeKind == "module" {
			found := false
			for _, m := range e.Config.Registry.Modules {
				if m.ID == o.ScopeID {
					found = true
					for _, op := range m.Operations {
						if op == "state.hash" || op == "consensus.sign" {
							return fmt.Errorf("ERR_SECURITY_OFFCHAIN_REQUIRED")
						}
					}
				}
			}
			if !found {
				return fmt.Errorf("ERR_SECURITY_UNKNOWN_MODULE")
			}
		}
		if e.Security == nil {
			return fmt.Errorf("ERR_RECOVERY_NOT_CONFIGURED")
		}
		if err := e.Security.Accept(e.Config.Recovery, b, now); err != nil {
			return err
		}
		emit("security.hold", codec.Hash("recovery/order", o))
	}
	e.InterruptAffected(now, emit)
	return nil
}
func (e *Engine) InterruptAffected(now uint64, emit func(string, string)) {
	if e.Security == nil || e.Governance == nil {
		return
	}
	ids := []string{}
	for id := range e.Governance.Proposals {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := e.Governance.Proposals[id]
		blocked := e.Security.Blocked("profile", e.Config.VotingProfile, now) || e.Security.Blocked("effect", id, now)
		suites := []string{e.Config.Governance.EligibilitySuite, e.Config.Governance.BallotSuite, e.Config.Governance.TallySuite}
		if p.PollID != nil {
			poll := e.Governance.Polls[*p.PollID]
			if poll != nil {
				suites = []string{poll.EligibilitySuite, poll.BallotSuite, poll.TallySuite}
			}
			blocked = blocked || e.Security.Blocked("poll", *p.PollID, now)
		}
		for _, suite := range suites {
			blocked = blocked || e.Security.ModuleBlocked(suite)
		}
		if blocked && e.Governance.Interrupt(id, "security.hold: pending independent safe decision") {
			emit("proposal.interrupted", id)
		}
	}
}
func (e *Engine) SecurityCheckTransaction(tx Transaction) error {
	if e.Security == nil {
		return nil
	}
	if !isProof(tx.Body.Type) {
		if suite, err := e.Config.Registry.AccountSuite(); err == nil && e.Security.ModuleBlocked(suite) {
			return fmt.Errorf("ERR_SECURITY_MODULE_REVIEW")
		}
	}
	switch tx.Body.Type {
	case "proposal.submit", "proposal.endorse", "poll.configure":
		if e.Security.Blocked("profile", e.Config.VotingProfile, e.Time) {
			return fmt.Errorf("ERR_SECURITY_HOLD")
		}
		if e.Config.Governance != nil {
			for _, suite := range []string{e.Config.Governance.EligibilitySuite, e.Config.Governance.BallotSuite, e.Config.Governance.TallySuite} {
				if e.Security.ModuleBlocked(suite) {
					return fmt.Errorf("ERR_SECURITY_MODULE_REVIEW")
				}
			}
		}
	}
	return nil
}
