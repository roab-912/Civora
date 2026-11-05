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
