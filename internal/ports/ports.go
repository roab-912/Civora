// Package ports isolates future adapters from deterministic account execution.
// None of these interfaces is called through network I/O during a transition.
package ports

import "context"

// Governance adapters are pure: no clock, network, files or external effects
// may be consulted during deterministic validation.
type EligibilityAccount struct {
	ID         string
	Commitment string
}
type EligibilityRequest struct {
	NetworkID, Epoch, PollID, Height string
	Accounts                         []EligibilityAccount
}
type EligibilityGroup struct {
	Root        *string
	Commitments []string
}
type EligibilityProvider interface {
	Snapshot(EligibilityRequest) (EligibilityGroup, error)
}
type PollContext struct {
	NetworkID, PollID, ProposalID, Epoch, EligibilitySuite, BallotSuite, TallySuite string
	GroupRoot                                                                       *string
	Commitments                                                                     []string
	CeremonyDigest                                                                  string
}
type AcceptedBallot struct{ Nullifier, BallotID, CiphertextDigest, ConfirmationCode, Aggregate string }
type TallyResult struct{ Yes, No string }
type VotingCrypto interface {
	Configure(PollContext, []byte) (string, error)
	Accept(PollContext, string, []byte) (AcceptedBallot, error)
	Tally(PollContext, string, string, []byte) (TallyResult, error)
}
type EffectRequest struct {
	NetworkID, ProposalID, Action, EffectiveMS string
	Payload                                    []byte
}
type EffectReceipt struct {
	Profile   string `json:"profile"`
	Reference string `json:"reference"`
}
type EffectAdapter interface {
	Evaluate(EffectRequest) (EffectReceipt, error)
}

type SnapshotRef struct{ NetworkID, Height, Root, SuiteID, Epoch string }
type EligibilitySnapshot interface {
	Freeze(previousHeight string) (SnapshotRef, error)
	Path(snapshot SnapshotRef, commitment string) ([]byte, error)
}
type BulletinContext struct{ NetworkID, PollID, Epoch, EligibilitySuite, BallotSuite, TallySuite string }
type BulletinReceipt struct {
	EnvelopeDigest string
	Proof          []byte
}
type BulletinTransport interface {
	Submit(context.Context, BulletinContext, []byte) (BulletinReceipt, error)
}
type ResultProof interface {
	Verify(BulletinContext, []byte, []byte) error
}
type ProtocolVersion interface {
	BeforeBlock(height string) error
	Authorize(epoch, operation, moduleID string, historical bool) error
}
type LedgerStore interface {
	Height() string
	AppHash() string
	Commit(block []byte) error
}
