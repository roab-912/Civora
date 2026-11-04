import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import {
  canonical,
  decode,
  domain,
  validate,
  type State,
} from "../src/protocol.js";
import { Engine } from "../src/core.js";
import {
  decisionThreshold,
  validatePolicy,
  type GovernanceFixture,
  type GovernanceTransaction,
} from "../src/governance.js";
import { stateRoot } from "../src/tree.js";
const raw = readFileSync("spec/fixtures/v0.2/governance.json");
const fixture = decode<GovernanceFixture>("fixture", raw);
test("v0.2: strict fixture and final Merkle root agree with Go", () => {
  assert.equal(fixture.expected_state.height, "99");
  assert.equal(fixture.blocks.length, 99);
  decode<State>("state", canonical(fixture.expected_state));
  assert.equal(
    stateRoot(fixture.expected_state.entries),
    fixture.expected_roots.at(-1),
  );
  for (const [i, events] of fixture.expected_events.entries())
    for (const [j, event] of events.entries())
      assert.equal(
        event.id,
        domain("event", [fixture.genesis.network_id, String(i + 1), String(j)]),
      );
});
test("v0.2: proof-authorized envelopes preserve null actor/nonce and proof-only auth", () => {
  let count = 0;
  for (const block of fixture.blocks)
    for (const value of block.transactions) {
      const tx = value as GovernanceTransaction;
      if (
        ["poll.configure", "vote.cast", "poll.tally"].includes(tx.body.type)
      ) {
        assert.equal(tx.body.actor_id, null);
        assert.equal(tx.body.nonce, null);
        assert.deepEqual(tx.auth, { scheme: "proof" });
        count++;
        const malformed = structuredClone(tx) as unknown as {
          body: { actor_id: unknown };
        };
        malformed.body.actor_id = "00".repeat(32);
        assert.throws(() => validate("transaction", malformed));
      }
    }
  assert.ok(count > 0);
});
test("v0.2: schemas reject arbitrary capabilities, scripts and mutable fields", () => {
  const tx = fixture.blocks
    .flatMap((b) => b.transactions)
    .find(
      (t) => (t as GovernanceTransaction).body.type === "proposal.submit",
    ) as GovernanceTransaction;
  const clone = structuredClone(tx) as unknown as {
    body: { payload: Record<string, unknown> };
  };
  clone.body.payload.changes = { script: "grant-all" };
  assert.throws(() => validate("transaction", clone));
  clone.body.payload.action = "role.create";
  clone.body.payload.changes = {
    label: "Administrator",
    description: "No",
    capabilities: ["modify_result"],
    scope_id: null,
  };
  assert.throws(() => validate("transaction", clone));
  clone.body.payload.action = "network.rotate_validator";
  assert.throws(() => validate("transaction", clone));
});
test("v0.2: constitutional equality and quorum use exact integers", () => {
  const p = fixture.genesis.governance.policy;
  validatePolicy(p);
  assert.equal(decisionThreshold("2", "1", "8", p, true), true);
  assert.equal(decisionThreshold("3", "2", "8", p, true), false);
  assert.equal(decisionThreshold("2", "1", "20", p, true), false);
  assert.equal(decisionThreshold("1", "1", "8", p, false), false);
  assert.equal(decisionThreshold("0", "0", "8", p, false), false);
  assert.throws(() =>
    decisionThreshold(
      "18446744073709551615",
      "1",
      "18446744073709551615",
      p,
      false,
    ),
  );
  assert.throws(() =>
    validatePolicy({ ...p, constitutional_quorum_bps: "1999" }),
  );
});
test("v0.2: the v0.1 reference oracle explicitly refuses governance genesis", () => {
  assert.throws(
    () => new Engine(canonical(fixture.genesis)),
    /ERR_GOVERNANCE_ORACLE_UNAVAILABLE/,
  );
});
