package partition

import (
	"civora/internal/codec"
	"civora/internal/electionguard"
	"civora/internal/semaphore"
	"fmt"
)

// VerifyAdmission is the public, pure eligibility gate, separate from the
// deferred ElectionGuard and nonce-knowledge checks.
func VerifyAdmission(f Frozen, lane string, raw []byte) error {
	if len(raw) > electionguard.MaxScaleEnvelopeBytes {
		return fmt.Errorf("envelope bound")
	}
	var env electionguard.ScaleEnvelope
	if e := codec.Decode(raw, "scale_envelope", &env); e != nil {
		return e
	}
	c := f.Context
	if env.NetworkID != c.NetworkID || env.PollID != c.PollID || env.Epoch != c.Epoch || env.Checkpoint != c.Checkpoint || env.Lane != lane {
		return fmt.Errorf("admission context")
	}
	p, _ := codec.Uint(c.Partitions)
	route, e := electionguard.Lane(c.NetworkID, c.PollID, env.Membership.Nullifier, p)
	if e != nil || fmt.Sprint(route) != lane {
		return fmt.Errorf("admission route")
	}
	d, e := electionguard.Decode(env.Ballot, electionguard.Ballot)
	if e != nil {
		return e
	}
	scope, message := electionguard.ScaleBinding(c, digest(d.Files["ballot.json"]), digest(codec.Must(env.Independence)))
	s, e := semaphore.New()
	if e != nil {
		return e
	}
	return s.VerifyBound(semaphore.Context{NetworkID: c.NetworkID, PollID: c.PollID, Epoch: c.Epoch, SuiteID: "semaphore-v4.14.5-depth-" + c.Depth, Depth: c.Depth, Root: c.Root}, env.Membership, scope, message)
}
