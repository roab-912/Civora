package governance

// Interrupt preserves ballots, results and planned dates as audit evidence.
// Neither elapsed time nor an emergency key can resume or rewrite this poll.
func (s *State) Interrupt(id, reason string) bool {
	p := s.Proposals[id]
	if p == nil {
		return false
	}
	switch p.State {
	case "applied", "rejected", "expired", "conflict", "failed_setup", "failed_window", "failed_tally", "failed_certification", "interrupted":
		return false
	}
	p.State = "interrupted"
	p.Reason = ptr(reason)
	if p.PollID != nil {
		if poll := s.Polls[*p.PollID]; poll != nil {
			poll.State = "interrupted"
		}
	}
	return true
}
