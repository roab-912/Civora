package governance

import (
	"civora/internal/codec"
	"civora/internal/ports"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
)

type due struct {
	id    string
	at    uint64
	grant bool
}

func ordered(items []due) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].at != items[j].at {
			return items[i].at < items[j].at
		}
		a, b := items[i].id, items[j].id
		if items[i].grant {
			a = grantID(a)
		}
		if items[j].grant {
			b = grantID(b)
		}
		return a < b
	})
}
func (s *State) setState(x Execution, id, status string) {
	p := s.Proposals[id]
	p.State = status
	if p.PollID != nil {
		s.Polls[*p.PollID].State = status
	}
	x.Emit("proposal."+status, id)
}

// Advance runs each phase once. The eligibility provider receives accounts
// from H-1 even when account transactions later change the current block.
func (s *State) Advance(x Execution) error {
	items := []due{}
	for id, p := range s.Proposals {
		if p.State == "open" {
			poll := s.Polls[*p.PollID]
			if num(*poll.ClosesAtMS) <= x.Time {
				items = append(items, due{id, num(*poll.ClosesAtMS), false})
			}
		}
	}
	ordered(items)
	for _, item := range items {
		p := s.Proposals[item.id]
		poll := s.Polls[*p.PollID]
		poll.ClosedHeight = ptr(x.Height)
		poll.AggregateDigest = ptr(codec.Hash("poll/aggregate", []any{x.NetworkID, *p.PollID, x.Height, poll.Count, poll.Aggregate}))
		s.setState(x, item.id, "tallying")
		if poll.Count == "0" {
			poll.Result = &Result{"0", "0", true, x.Height, str(x.Time)}
			s.setState(x, item.id, "rejected")
			p.Reason = ptr("empty")
			x.Emit("poll.empty", *p.PollID)
		}
	}
	items = []due{}
	for id, p := range s.Proposals {
		var deadline *string
		switch p.State {
		case "collecting":
			deadline = &p.CollectionDueMS
		case "qualified":
			deadline = p.QueueDueMS
		case "preparing":
			deadline = p.SetupDueMS
		case "tallying":
			deadline = s.Polls[*p.PollID].TallyDueAtMS
		}
		if deadline != nil && num(*deadline) <= x.Time {
			items = append(items, due{id, num(*deadline), false})
		}
	}
	for key, g := range s.Grants {
		if g.Status == "active" && num(g.UntilMS) <= x.Time {
			items = append(items, due{key, num(g.UntilMS), true})
		}
	}
	ordered(items)
	for _, item := range items {
		if item.grant {
			g := s.Grants[item.id]
			g.Status = "expired"
			g.EndedAtMS = ptr(str(x.Time))
			x.Emit("grant.expired", grantID(item.id))
			continue
		}
		p := s.Proposals[item.id]
		status := "expired"
		if p.State == "preparing" {
			status = "failed_setup"
		}
		if p.State == "tallying" {
			status = "failed_tally"
		}
		s.setState(x, item.id, status)
	}
	items = []due{}
	for id, p := range s.Proposals {
		if p.State == "adopted" && num(*p.EffectiveAtMS) <= x.Time {
			items = append(items, due{id, num(*p.EffectiveAtMS), false})
		}
	}
	ordered(items)
	for _, item := range items {
		p := s.Proposals[item.id]
		if e := s.applyEffect(x, item.id, p); e != nil {
			p.Reason = ptr(e.Error())
			s.setState(x, item.id, "conflict")
		} else {
			s.setState(x, item.id, "applied")
		}
	}
	items = []due{}
	for id, p := range s.Proposals {
		if p.State == "scheduled" && num(*s.Polls[*p.PollID].OpensAtMS) <= x.Time {
			items = append(items, due{id, num(*s.Polls[*p.PollID].OpensAtMS), false})
		}
	}
	ordered(items)
	for _, item := range items {
		p := s.Proposals[item.id]
		poll := s.Polls[*p.PollID]
		if x.Time >= num(*poll.ClosesAtMS) {
			s.setState(x, item.id, "failed_window")
			continue
		}
		if x.Eligibility == nil {
			return fmt.Errorf("ERR_ELIGIBILITY_ADAPTER")
		}
		accounts := []ports.EligibilityAccount{}
		for id, a := range x.PreviousAccounts {
			if a.Active {
				accounts = append(accounts, ports.EligibilityAccount{ID: id, Commitment: a.Commitment})
			}
		}
		sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
		group, e := x.Eligibility.Snapshot(ports.EligibilityRequest{NetworkID: x.NetworkID, Epoch: poll.ProtocolEpoch, PollID: *p.PollID, Height: x.PreviousHeight, Accounts: accounts})
		if e != nil {
			return e
		}
		if len(group.Commitments) != len(accounts) || (len(accounts) == 0) != (group.Root == nil) {
			return fmt.Errorf("ERR_ELIGIBILITY_SNAPSHOT")
		}
		for i, a := range accounts {
			if group.Commitments[i] != a.Commitment {
				return fmt.Errorf("ERR_ELIGIBILITY_SNAPSHOT")
			}
		}
		poll.GroupRoot = group.Root
		poll.Commitments = append([]string{}, group.Commitments...)
		poll.EligibleCount = str(uint64(len(group.Commitments)))
		poll.SnapshotHeight = ptr(x.PreviousHeight)
		s.setState(x, item.id, "open")
	}
	// Queue admission is deliberately after openings and expiration. A proposal
	// qualified at the end of this block cannot be admitted until the next one.
	items = []due{}
	for id, p := range s.Proposals {
		if p.State == "qualified" {
			items = append(items, due{id, num(*p.QualifiedHeight), false})
		}
	}
	ordered(items)
	slots := 16 - s.LivePolls()
	for _, item := range items {
		if slots <= 0 {
			break
		}
		p := s.Proposals[item.id]
		deadline, e := add(x.Time, num(p.Policy.SetupMS))
		if e != nil {
			return e
		}
		p.PollID = ptr(codec.Hash("poll", []any{x.NetworkID, item.id}))
		p.SetupDueMS = ptr(str(deadline))
		s.Polls[*p.PollID] = &Poll{ProposalID: item.id, State: "preparing", ProtocolEpoch: x.Epoch, EligibilitySuite: x.Config.EligibilitySuite, BallotSuite: x.Config.BallotSuite, TallySuite: x.Config.TallySuite, EligibleCount: "0", Commitments: []string{}, Aggregate: "", Count: "0"}
		s.setState(x, item.id, "preparing")
		slots--
	}
	return nil
}

func grantID(key string) string {
	var parts []string
	_ = json.Unmarshal([]byte(key), &parts)
	return parts[1]
}

// Adopted uses widened integer arithmetic, including the inclusive two-thirds
// constitutional threshold and the frozen proposal's turnout requirement.
func Adopted(yes, no, eligible string, policy Policy, constitutional bool) (bool, error) {
	y, e := codec.Uint(yes)
	if e != nil {
		return false, e
	}
	n, e := codec.Uint(no)
	if e != nil {
		return false, e
	}
	voters, e := codec.Uint(eligible)
	if e != nil {
		return false, e
	}
	if y > ^uint64(0)-n || y+n > voters {
		return false, fmt.Errorf("ERR_TALLY_COUNT")
	}
	count := y + n
	if count == 0 {
		return false, nil
	}
	Y, C, E := new(big.Int).SetUint64(y), new(big.Int).SetUint64(count), new(big.Int).SetUint64(voters)
	q := num(policy.OrdinaryQuorumBPS)
	approved := new(big.Int).Mul(Y, big.NewInt(2)).Cmp(C) > 0
	if constitutional {
		q = num(policy.ConstitutionalQuorumBPS)
		approved = new(big.Int).Mul(new(big.Int).SetUint64(y), big.NewInt(3)).Cmp(new(big.Int).Mul(new(big.Int).SetUint64(count), big.NewInt(2))) >= 0
	}
	turnout := new(big.Int).Mul(new(big.Int).SetUint64(count), big.NewInt(10000))
	required := new(big.Int).Mul(E, new(big.Int).SetUint64(q))
	return approved && turnout.Cmp(required) >= 0, nil
}
