package governance

import (
	"civora/internal/codec"
	"fmt"
)

// StagePartitionResult is called only after the coordinator has authenticated
// every final lane seal and verified the single global decryption. It grants
// no certification, adoption or effect. Inline tallies never call this port.
func (s *State) StagePartitionResult(network, pollID, aggregate, count string, now uint64) error {
	poll, p, e := s.poll(pollID, "processing")
	if e != nil {
		return e
	}
	n, e := codec.Uint(count)
	if e != nil || n > num(poll.EligibleCount) || poll.ClosedHeight == nil || poll.TallyDueAtMS == nil || now >= num(*poll.TallyDueAtMS) {
		return fmt.Errorf("ERR_PARTITION_RESULT_PHASE")
	}
	poll.Aggregate = aggregate
	poll.Count = count
	poll.AggregateDigest = ptr(codec.Hash("poll/aggregate", []any{network, pollID, *poll.ClosedHeight, count, aggregate}))
	poll.State = "provisional"
	p.State = "provisional"
	return nil
}
func (s *State) CertifyPartitionResult(pollID, yes, no, height string, now uint64) error {
	poll, p, e := s.poll(pollID, "provisional")
	if e != nil {
		return e
	}
	y, e := codec.Uint(yes)
	if e != nil {
		return e
	}
	n, e := codec.Uint(no)
	if e != nil || y > ^uint64(0)-n || y+n != num(poll.Count) || now >= num(*poll.TallyDueAtMS) {
		return fmt.Errorf("ERR_AUDIT_RESULT_COUNT")
	}
	adopted, e := Adopted(yes, no, poll.EligibleCount, p.Policy, p.Action == "policy.update")
	if e != nil {
		return e
	}
	poll.Result = &Result{Yes: yes, No: no, Empty: y+n == 0, CertifiedHeight: height, CertifiedMS: str(now)}
	if !adopted {
		p.State = "rejected"
		poll.State = "rejected"
		return nil
	}
	delay := num(p.Policy.OrdinaryExecutionDelayMS)
	if p.Action == "policy.update" {
		delay = num(p.Policy.ConstitutionalExecutionDelayMS)
	}
	at, e := add(now, delay)
	if e != nil {
		return e
	}
	if p.RequestedEffectMS != nil && num(*p.RequestedEffectMS) > at {
		at = num(*p.RequestedEffectMS)
	}
	p.EffectiveAtMS = ptr(str(at))
	p.State = "adopted"
	poll.State = "adopted"
	return nil
}
func (s *State) BlockPartitionEffect(pollID string) {
	poll := s.Polls[pollID]
	if poll == nil {
		return
	}
	p := s.Proposals[poll.ProposalID]
	if p.State != "applied" {
		p.State = "contested"
		poll.State = "contested"
	}
}
