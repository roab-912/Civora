//go:build civora_sim

package simulation

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/governance"
	"civora/internal/state/store"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func fresh(t *testing.T) *Builder {
	t.Helper()
	b, e := NewBuilder(t.Name())
	must(t, e)
	must(t, b.Register())
	return b
}
func reject(t *testing.T, b *Builder, at uint64, code string, txs ...core.Transaction) {
	t.Helper()
	before := codec.Must(b.Engine.Snapshot())
	_, _, e := b.Engine.Apply(codec.Must(core.Block{Height: fmt.Sprint(b.Engine.Height + 1), TimeMS: fmt.Sprint(at), Transactions: txs}))
	if e == nil || !strings.Contains(e.Error(), code) {
		t.Fatalf("want %s got %v", code, e)
	}
	if !bytes.Equal(before, codec.Must(b.Engine.Snapshot())) {
		t.Fatal("rejected block changed state")
	}
}
func state(t *testing.T, b *Builder, id, want string) {
	t.Helper()
	if p := b.Engine.Governance.Proposals[id]; p.State != want {
		t.Fatalf("want %s got %s (%v)", want, p.State, p.Reason)
	}
}
func openLaw(t *testing.T) (*Builder, string) {
	t.Helper()
	b := fresh(t)
	id, e := b.Open(Law("Loi de test"))
	must(t, e)
	return b, id
}
func schemaState(t *testing.T, b *Builder) {
	t.Helper()
	v, e := codec.Parse(codec.Must(b.Engine.Snapshot()))
	must(t, e)
	must(t, codec.Validate("state", v))
}

func TestLifecycleFixtureReplayAndResume(t *testing.T) {
	b, e := GovernanceScenario()
	must(t, e)
	schemaState(t, b)
	if b.Engine.Governance.Network.PolicyRevision != "2" {
		t.Fatal("policy not applied")
	}
	root := b.Engine.Root()
	engine, e := New(codec.Must(b.Fixture.Genesis), 1)
	must(t, e)
	for i, block := range b.Fixture.Blocks {
		next, events, e := engine.Apply(codec.Must(block))
		must(t, e)
		engine = next
		if engine.Root() != b.Fixture.ExpectedRoots[i] || !bytes.Equal(codec.Must(events), codec.Must(b.Fixture.ExpectedEvents[i])) {
			t.Fatal("nondeterministic replay", i)
		}
		v, e := codec.Parse(codec.Must(engine.Snapshot()))
		must(t, e)
		must(t, codec.Validate("state", v))
	}
	if engine.Root() != root {
		t.Fatal("final root")
	}
	dir := t.TempDir()
	s, e := store.OpenWithFactory(dir, codec.Must(b.Fixture.Genesis), 1, New)
	must(t, e)
	for _, block := range b.Fixture.Blocks {
		must(t, s.Commit(codec.Must(block)))
	}
	s, e = store.OpenWithFactory(dir, codec.Must(b.Fixture.Genesis), 1, New)
	must(t, e)
	if s.AppHash() != root {
		t.Fatal("resume root")
	}
	if _, e = core.New(codec.Must(b.Fixture.Genesis), 1); e == nil || !strings.Contains(e.Error(), "ERR_SIMULATION_DISABLED") {
		t.Fatal("normal node loaded simulation", e)
	}
}
func TestCollectionAndQualification(t *testing.T) {
	b := fresh(t)
	tx := b.Signed(5, "proposal.submit", Law("Expiration"))
	id := core.TxID(tx.Body)
	must(t, b.Tick(tx))
	reject(t, b, b.Time+1, "ERR_AUTHOR_ENDORSEMENT", b.Signed(5, "proposal.endorse", map[string]any{"proposal_id": id}))
	endorse := b.Signed(6, "proposal.endorse", map[string]any{"proposal_id": id})
	must(t, b.Tick(endorse))
	reject(t, b, b.Time+1, "ERR_DUPLICATE_ENDORSEMENT", b.Signed(6, "proposal.endorse", map[string]any{"proposal_id": id}))
	due, _ := codec.Uint(b.Engine.Governance.Proposals[id].CollectionDueMS)
	must(t, b.Block(due))
	state(t, b, id, "expired")
	newID, e := b.Submit(Law("Réexamen du dossier clos " + id))
	must(t, e)
	if newID == id {
		t.Fatal("same proposal reopened")
	}
	state(t, b, newID, "qualified")
	if b.Engine.Governance.Proposals[newID].PollID != nil {
		t.Fatal("admission happened in qualification block")
	}
	must(t, b.Tick())
	state(t, b, newID, "preparing")
}
func TestTerminalTransitionsAtDeadlines(t *testing.T) {
	t.Run("failed_setup", func(t *testing.T) {
		b := fresh(t)
		id, e := b.Submit(Law("Sans cérémonie"))
		must(t, e)
		must(t, b.Tick())
		p := b.Engine.Governance.Proposals[id]
		due, _ := codec.Uint(*p.SetupDueMS)
		reject(t, b, due, "ERR_PHASE", b.Configuration(*p.PollID))
		must(t, b.Block(due))
		state(t, b, id, "failed_setup")
	})
	t.Run("failed_window", func(t *testing.T) {
		b := fresh(t)
		id, e := b.Submit(Law("Fenêtre manquée"))
		must(t, e)
		must(t, b.Tick())
		p := b.Engine.Governance.Proposals[id]
		must(t, b.Tick(b.Configuration(*p.PollID)))
		close, _ := codec.Uint(*b.Engine.Governance.Polls[*p.PollID].ClosesAtMS)
		must(t, b.Block(close))
		state(t, b, id, "failed_window")
	})
	t.Run("zero_votes", func(t *testing.T) {
		b, id := openLaw(t)
		must(t, b.Close(id))
		state(t, b, id, "rejected")
		result := b.Engine.Governance.Polls[*b.Engine.Governance.Proposals[id].PollID].Result
		if result == nil || !result.Empty || result.Yes != "0" || result.No != "0" {
			t.Fatal("empty result not explicit")
		}
	})
	t.Run("failed_tally_and_last_ballot", func(t *testing.T) {
		b, id := openLaw(t)
		pollID := *b.Engine.Governance.Proposals[id].PollID
		poll := b.Engine.Governance.Polls[pollID]
		close, _ := codec.Uint(*poll.ClosesAtMS)
		vote := b.Proof("vote.cast", Vote(b.Context(pollID), b.Actors[0].Commitment, "1", codec.Hash("test/ballot", "last")))
		must(t, b.Block(close-1, vote))
		reject(t, b, close, "ERR_PHASE", b.Proof("vote.cast", Vote(b.Context(pollID), b.Actors[1].Commitment, "1", codec.Hash("test/ballot", "late"))))
		must(t, b.Close(id))
		state(t, b, id, "tallying")
		poll = b.Engine.Governance.Polls[pollID]
		due, _ := codec.Uint(*poll.TallyDueAtMS)
		req := Tally(b.Context(pollID), poll.Aggregate, poll.Count, *poll.AggregateDigest)
		reject(t, b, due, "ERR_PHASE", b.Proof("poll.tally", req))
		must(t, b.Block(due))
		state(t, b, id, "failed_tally")
	})
	t.Run("tie", func(t *testing.T) {
		b := fresh(t)
		id, e := b.Decide(Law("Égalité"), 1, 1)
		must(t, e)
		state(t, b, id, "rejected")
		if len(b.Engine.Governance.Objects) != 0 {
			t.Fatal("tie produced an effect")
		}
	})
}
func TestQueueCapacityAndExpiration(t *testing.T) {
	b, e := NewBuilder(t.Name())
	must(t, e)
	b.Fixture.Genesis.Governance.Policy.SetupMS = fmt.Sprint(governance.Day)
	b.Fixture.Genesis.Governance.Policy.QueueMS = fmt.Sprint(governance.Hour)
	b.Engine, e = New(codec.Must(b.Fixture.Genesis), 1)
	must(t, e)
	must(t, b.Register())
	ids := []string{}
	for i := 0; i < 17; i++ {
		id, e := b.Submit(Law(fmt.Sprint("File ", i)))
		must(t, e)
		ids = append(ids, id)
	}
	if b.Engine.Governance.LivePolls() != 16 {
		t.Fatal("live poll capacity")
	}
	last := ids[16]
	state(t, b, last, "qualified")
	due, _ := codec.Uint(*b.Engine.Governance.Proposals[last].QueueDueMS)
	must(t, b.Block(due))
	state(t, b, last, "expired")
	if b.Engine.Governance.LivePolls() != 16 {
		t.Fatal("live polls expired unexpectedly")
	}
}
func TestEffectDelayRequestedTimeAndConflict(t *testing.T) {
	b := fresh(t)
	req := Law("Effet différé")
	requested := fmt.Sprint(b.Time + 10*governance.Day)
	req.RequestedEffectMS = &requested
	id, e := b.Open(req)
	must(t, e)
	must(t, b.Votes(id, 2, 1))
	must(t, b.Close(id))
	must(t, b.Count(id))
	state(t, b, id, "adopted")
	at, _ := codec.Uint(*b.Engine.Governance.Proposals[id].EffectiveAtMS)
	must(t, b.Block(at-1))
	if len(b.Engine.Governance.Objects) != 0 {
		t.Fatal("early effect")
	}
	must(t, b.Block(at))
	state(t, b, id, "applied")
	lawID := codec.Hash("object", []any{"law", id})
	a, e := b.Submit(Request("law.amend", &lawID, pointer("1"), governance.LawContent{Title: "A", Text: "Amendement A", Language: "fr"}))
	must(t, e)
	c, e := b.Submit(Request("law.amend", &lawID, pointer("1"), governance.LawContent{Title: "B", Text: "Amendement B", Language: "fr"}))
	must(t, e)
	must(t, b.Tick())
	pa, pc := b.Engine.Governance.Proposals[a], b.Engine.Governance.Proposals[c]
	must(t, b.Tick(b.Configuration(*pa.PollID), b.Configuration(*pc.PollID)))
	opens, _ := codec.Uint(*b.Engine.Governance.Polls[*pa.PollID].OpensAtMS)
	must(t, b.Block(opens))
	must(t, b.Votes(a, 2, 1))
	must(t, b.Votes(c, 2, 1))
	must(t, b.Close(a))
	qa, qc := b.Engine.Governance.Polls[*pa.PollID], b.Engine.Governance.Polls[*pc.PollID]
	must(t, b.Tick(b.Proof("poll.tally", Tally(b.Context(*pa.PollID), qa.Aggregate, qa.Count, *qa.AggregateDigest)), b.Proof("poll.tally", Tally(b.Context(*pc.PollID), qc.Aggregate, qc.Count, *qc.AggregateDigest))))
	at, _ = codec.Uint(*b.Engine.Governance.Proposals[a].EffectiveAtMS)
	must(t, b.Block(at))
	winner, loser := a, c
	if c < a {
		winner, loser = c, a
	}
	state(t, b, winner, "applied")
	state(t, b, loser, "conflict")
	if reason := *b.Engine.Governance.Proposals[loser].Reason; reason != "ERR_REVISION" {
		t.Fatal(reason)
	}
	if len(b.Engine.Governance.Revisions) != 2 {
		t.Fatal("history changed or conflict wrote revision")
	}
}
func TestSnapshotUsesPreviousBlockAndOldVotingRights(t *testing.T) {
	b := fresh(t)
	id, e := b.Submit(Law("Instantané"))
	must(t, e)
	must(t, b.Tick())
	pollID := *b.Engine.Governance.Proposals[id].PollID
	must(t, b.Tick(b.Configuration(pollID)))
	opens, _ := codec.Uint(*b.Engine.Governance.Polls[pollID].OpensAtMS)
	newSeed, newPK := seed("late signing")
	_, newRecovery := seed("late recovery")
	newID := codec.Hash("account", []any{NetworkID(), newPK})
	reg := Sign(core.Body{V: "1", NetworkID: NetworkID(), Type: "account.register", ActorID: newID, Nonce: "0", Payload: codec.Must(core.Payload{SigningPK: newPK, RecoveryPK: newRecovery, Commitment: "10"})}, newSeed)
	retire := Sign(core.Body{V: "1", NetworkID: NetworkID(), Type: "account.retire", ActorID: b.Actors[0].ID, Nonce: b.Engine.Accounts[b.Actors[0].ID].NextNonce, Payload: codec.Must(map[string]any{})}, b.Actors[0].RecoverySeed)
	_, rotatePK := seed("rotate signing")
	_, rotateRecovery := seed("rotate recovery")
	rotate := Sign(core.Body{V: "1", NetworkID: NetworkID(), Type: "account.rotate", ActorID: b.Actors[1].ID, Nonce: b.Engine.Accounts[b.Actors[1].ID].NextNonce, Payload: codec.Must(core.Payload{SigningPK: rotatePK, RecoveryPK: rotateRecovery, Commitment: "9"})}, b.Actors[1].RecoverySeed)
	previous := fmt.Sprint(b.Engine.Height)
	must(t, b.Block(opens, reg, retire, rotate))
	poll := b.Engine.Governance.Polls[pollID]
	if poll.EligibleCount != "8" || *poll.SnapshotHeight != previous {
		t.Fatal("snapshot height/electorate")
	}
	ctx := b.Context(pollID)
	for _, c := range []string{"10", "9"} {
		reject(t, b, b.Time+1, "ERR_SIMULATION_MEMBER", b.Proof("vote.cast", Vote(ctx, c, "1", codec.Hash("test/ballot", c))))
	}
	must(t, b.Votes(id, 2, 0))
	if b.Engine.Governance.Polls[pollID].Count != "2" {
		t.Fatal("old voting rights lost after retirement/rotation")
	}
}
func TestProofAndReplayRefusals(t *testing.T) {
	b, id := openLaw(t)
	pollID := *b.Engine.Governance.Proposals[id].PollID
	reject(t, b, b.Time+1, "ERR_SIMULATION_BALLOT", b.Proof("vote.cast", Vote(b.Context(pollID), b.Actors[0].Commitment, "1", strings.Repeat("z", 64))))
	vote := Vote(b.Context(pollID), b.Actors[0].Commitment, "1", codec.Hash("test/ballot", "one"))
	bad := vote
	bad.Membership.Points = append([]string{}, vote.Membership.Points...)
	bad.Membership.Points[1] = "1"
	reject(t, b, b.Time+1, "ERR_SIMULATION_PROOF", b.Proof("vote.cast", bad))
	tx := b.Proof("vote.cast", vote)
	reject(t, b, b.Time+1, "ERR_NULLIFIER_SPENT", tx, tx)
	must(t, b.Tick(tx))
	reject(t, b, b.Time+1, "ERR_NULLIFIER_SPENT", tx)
	other := vote
	other.PollID = strings.Repeat("0", 64)
	reject(t, b, b.Time+1, "ERR_POLL", b.Proof("vote.cast", other))
	foreign := tx
	foreign.Body.NetworkID = strings.Repeat("f", 64)
	reject(t, b, b.Time+1, "ERR_NETWORK", foreign)
	must(t, b.Close(id))
	poll := b.Engine.Governance.Polls[pollID]
	req := Tally(b.Context(pollID), poll.Aggregate, poll.Count, *poll.AggregateDigest)
	req.Yes = "0"
	req.No = "1"
	reject(t, b, b.Time+1, "ERR_SIMULATION_TALLY", b.Proof("poll.tally", req))
	must(t, b.Count(id))
	state(t, b, id, "adopted")
}
func TestRoleAuthorityHistoryAndPolicySnapshot(t *testing.T) {
	b, e := GovernanceScenario()
	must(t, e)
	var roleID, lawID string
	for k, obj := range b.Engine.Governance.Objects {
		var parts []string
		must(t, json.Unmarshal([]byte(k), &parts))
		if obj.Kind == "role" {
			roleID = parts[1]
		} else {
			lawID = parts[1]
		}
	}
	publication := governance.Publication{RoleID: roleID, Capability: "publish_notice", TargetID: lawID, Content: "Forbidden after amendment"}
	reject(t, b, b.Time+1, "ERR_CAPABILITY", b.Signed(5, "role.publish", publication))
	publication.Capability = "report_implementation"
	reject(t, b, b.Time+1, "ERR_GRANT", b.Signed(6, "role.publish", publication))
	objectsBefore := codec.Must(b.Engine.Governance.Objects)
	revisionsBefore := codec.Must(b.Engine.Governance.Revisions)
	publication.Content = "Texte public, sans pouvoir de modifier les lois."
	must(t, b.Tick(b.Signed(5, "role.publish", publication)))
	if !bytes.Equal(objectsBefore, codec.Must(b.Engine.Governance.Objects)) || !bytes.Equal(revisionsBefore, codec.Must(b.Engine.Governance.Revisions)) {
		t.Fatal("role publication mutated laws/roles")
	}
	oldPolicies := 0
	for _, p := range b.Engine.Governance.Proposals {
		if p.PolicyRevision == "1" && p.Policy.OrdinaryQuorumBPS == "0" {
			oldPolicies++
		}
	}
	if oldPolicies == 0 {
		t.Fatal("policy history erased")
	}
	id, e := b.Submit(Law("Nouvelle politique"))
	must(t, e)
	p := b.Engine.Governance.Proposals[id]
	if p.PolicyRevision != "2" || p.Policy.OrdinaryQuorumBPS != "1000" {
		t.Fatal("new proposal did not get new policy")
	}
	for _, g := range b.Engine.Governance.Grants {
		if g.Status == "active" {
			until, _ := codec.Uint(g.UntilMS)
			must(t, b.Block(until))
			publication.Content = "Expired grant"
			reject(t, b, b.Time+1, "ERR_GRANT", b.Signed(5, "role.publish", publication))
			break
		}
	}
}
func TestRoleRestoreNeverResurrectsGrants(t *testing.T) {
	b := fresh(t)
	role, e := b.Decide(Request("role.create", nil, nil, governance.RoleContent{Label: "Rôle", Description: "Test", Capabilities: []string{"annotate_proposal"}, ScopeID: nil}), 2, 1)
	must(t, e)
	roleID := codec.Hash("object", []any{"role", role})
	grant, e := b.Decide(Request("role.grant", &roleID, pointer("1"), map[string]any{"account_id": b.Actors[5].ID, "until_ms": fmt.Sprint(b.Time + 10*governance.Day)}), 2, 1)
	must(t, e)
	_, e = b.Decide(Request("role.retire", &roleID, pointer("1"), map[string]any{}), 2, 1)
	must(t, e)
	_, e = b.Decide(Request("role.restore", &roleID, pointer("2"), map[string]any{"source_revision": "1"}), 2, 1)
	must(t, e)
	for _, g := range b.Engine.Governance.Grants {
		if g.Status != "revoked" {
			t.Fatal("grant resurrected")
		}
	}
	reject(t, b, b.Time+1, "ERR_GRANT", b.Signed(5, "role.publish", governance.Publication{RoleID: roleID, Capability: "annotate_proposal", TargetID: grant, Content: "No grant"}))
}
func TestSizeBoundsAndOverflow(t *testing.T) {
	b := fresh(t)
	req := Law(strings.Repeat("😀", 161))
	reject(t, b, b.Time+1, "schema", b.Signed(5, "proposal.submit", req))
	req = Law("UTF-8 size")
	req.Rationale = strings.Repeat("é", 9000)
	reject(t, b, b.Time+1, "ERR_RATIONALE", b.Signed(5, "proposal.submit", req))
	reject(t, b, ^uint64(0), "ERR_TIME_OVERFLOW", b.Signed(5, "proposal.submit", Law("Overflow")))
}
func TestCheckedInFixtures(t *testing.T) {
	must(t, GenerateFixtures(filepath.Join("..", "..", "spec", "fixtures", "v0.2"), true))
	raw, e := os.ReadFile(filepath.Join("..", "..", "spec", "fixtures", "v0.2", "governance.json"))
	must(t, e)
	var fixture Fixture
	must(t, codec.Decode(raw, "fixture", &fixture))
	if fixture.ExpectedState.Height != "99" {
		t.Fatal("unexpected demo height")
	}
}

func TestEndorsementBoundaryAndBlockBudgets(t *testing.T) {
	for _, atDeadline := range []bool{false, true} {
		b := fresh(t)
		tx := b.Signed(5, "proposal.submit", Law("Dernier soutien"))
		id := core.TxID(tx.Body)
		must(t, b.Tick(tx))
		due, _ := codec.Uint(b.Engine.Governance.Proposals[id].CollectionDueMS)
		endorsements := []core.Transaction{b.Signed(6, "proposal.endorse", map[string]any{"proposal_id": id}), b.Signed(7, "proposal.endorse", map[string]any{"proposal_id": id})}
		if atDeadline {
			reject(t, b, due, "ERR_PHASE", endorsements...)
			must(t, b.Block(due))
			state(t, b, id, "expired")
		} else {
			must(t, b.Block(due-1, endorsements...))
			state(t, b, id, "qualified")
		}
	}
	b, id := openLaw(t)
	pollID := *b.Engine.Governance.Proposals[id].PollID
	tx := b.Proof("vote.cast", Vote(b.Context(pollID), b.Actors[0].Commitment, "1", codec.Hash("test/ballot", "budget")))
	many := []core.Transaction{}
	for range 33 {
		many = append(many, tx)
	}
	reject(t, b, b.Time+1, "ERR_BLOCK_BUDGET", many...)
	many = []core.Transaction{}
	for range 5 {
		many = append(many, b.Configuration(pollID))
	}
	reject(t, b, b.Time+1, "ERR_BLOCK_BUDGET", many...)
}
func TestProposalCapacity(t *testing.T) {
	b := fresh(t)
	for i := 0; i < 128; i++ {
		must(t, b.Tick(b.Signed(5, "proposal.submit", Law(fmt.Sprint("Capacité ", i)))))
	}
	reject(t, b, b.Time+1, "ERR_CAPACITY", b.Signed(5, "proposal.submit", Law("Une de trop")))
}
func TestGuardianKeysArePinnedAtSubmission(t *testing.T) {
	b := fresh(t)
	id, e := b.Submit(Law("Clés épinglées"))
	must(t, e)
	_, pk := seed("guardian rotated signing")
	actor := b.Actors[0]
	rotate := Sign(core.Body{V: "1", NetworkID: NetworkID(), Type: "account.rotate", ActorID: actor.ID, Nonce: b.Engine.Accounts[actor.ID].NextNonce, Payload: codec.Must(core.Payload{SigningPK: pk, RecoveryPK: actor.RecoveryKey, Commitment: actor.Commitment})}, actor.RecoverySeed)
	must(t, b.Tick(rotate))
	p := b.Engine.Governance.Proposals[id]
	if p.Guardians[0].SigningPK != actor.PublicKey {
		t.Fatal("pinned key changed")
	}
	must(t, b.Tick(b.Configuration(*p.PollID)))
	state(t, b, id, "scheduled")
	next, e := b.Submit(Law("Nouvelles clés"))
	must(t, e)
	must(t, b.Tick())
	pollID := *b.Engine.Governance.Proposals[next].PollID
	reject(t, b, b.Time+1, "ERR_GUARDIAN_ACKS", b.Configuration(pollID))
}
func TestScopedRoleCannotPublishElsewhere(t *testing.T) {
	b, e := GovernanceScenario()
	must(t, e)
	var roleID string
	for key, obj := range b.Engine.Governance.Objects {
		if obj.Kind == "role" {
			var parts []string
			must(t, json.Unmarshal([]byte(key), &parts))
			roleID = parts[1]
		}
	}
	id, e := b.Decide(Law("Hors portée"), 2, 1)
	must(t, e)
	otherLaw := codec.Hash("object", []any{"law", id})
	reject(t, b, b.Time+1, "ERR_SCOPE", b.Signed(5, "role.publish", governance.Publication{RoleID: roleID, Capability: "report_implementation", TargetID: otherLaw, Content: "Hors portée"}))
}
