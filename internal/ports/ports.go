// Package ports isolates future adapters from deterministic account execution.
// None of these interfaces is called through network I/O during a transition.
package ports

import "context"

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
