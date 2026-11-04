import assert from "node:assert/strict";
import test, { after } from "node:test";
import { readFileSync } from "node:fs";
import { Group } from "@semaphore-protocol/group";
import { Identity } from "@semaphore-protocol/identity";
import { canonical, decode, validate } from "../src/protocol.js";
import {
  binding,
  verifyPath,
  type EligibilityPath,
  type Snapshot,
  type Membership,
  type MembershipContext,
} from "../src/eligibility.js";
import { verifyMembership } from "../src/semaphore.js";
import { stateRoot } from "../src/tree.js";
import { Engine } from "../src/core.js";
import type { Fixture as AccountFixture, State } from "../src/protocol.js";
const read = (name: string): unknown =>
  JSON.parse(readFileSync("spec/fixtures/v0.3/" + name + ".json", "utf8"));
interface Fixture {
  context: MembershipContext;
  path: EligibilityPath;
  identity_export_TEST_ONLY: string;
  ballot_base64url: string;
  membership: Membership;
}
test("LeanIMT SDK roots and compressed paths survive retirement and rotation", async () => {
  const vector = read("lean-imt") as {
    steps: {
      changes: { operation: string; index: string; commitment: string }[];
      snapshot: Snapshot;
      paths: EligibilityPath[];
    }[];
  };
  const group = new Group();
  for (const step of vector.steps) {
    for (const c of step.changes) {
      if (c.operation === "append") group.addMember(c.commitment);
      else group.removeMember(Number(c.index));
    }
    assert.equal(group.root.toString(), step.snapshot.root);
    decode("eligibility_snapshot", canonical(step.snapshot));
    for (const p of step.paths) {
      decode("eligibility_path", canonical(p));
      await verifyPath(p, step.snapshot, p.leaf);
      assert.equal(
        String(group.generateMerkleProof(Number(p.leaf_index)).index),
        p.index,
      );
    }
  }
});
for (const depth of ["16", "26"])
  test(
    "Semaphore " +
      depth +
      ": public fixture and Go-compatible contextual verification",
    async () => {
      const f = read("membership-" + depth) as Fixture;
      const identity = Identity.import(f.identity_export_TEST_ONLY);
      assert.equal(identity.commitment.toString(), f.path.leaf);
      await verifyPath(f.path, f.path.snapshot, f.path.leaf);
      validate("membership_v03", f.membership);
      validate("membership_context", f.context);
      const ballot = Buffer.from(f.ballot_base64url, "base64url");
      const bound = await binding(
        f.context.network_id,
        f.context.poll_id,
        ballot,
      );
      assert.equal(bound.scope, f.membership.scope);
      assert.equal(bound.message, f.membership.message);
      assert.equal(
        await verifyMembership(f.context, ballot, f.membership),
        true,
      );
      for (const c of [
        { ...f.context, poll_id: "aa".repeat(32) },
        { ...f.context, network_id: "bb".repeat(32) },
        { ...f.context, group_root: "1" },
        { ...f.context, suite_id: "unknown" },
      ])
        assert.equal(await verifyMembership(c, ballot, f.membership), false);
      assert.equal(
        await verifyMembership(
          f.context,
          Buffer.concat([ballot, Buffer.from([1])]),
          f.membership,
        ),
        false,
      );
      const bad = structuredClone(f.membership);
      bad.points[0] = "1";
      assert.equal(await verifyMembership(f.context, ballot, bad), false);
    },
  );
test("forged path, wrong snapshot and wrong identity are refused before proving", async () => {
  const f = read("membership-16") as Fixture;
  const bad = structuredClone(f.path);
  bad.siblings[0] = "1";
  await assert.rejects(
    verifyPath(bad, f.path.snapshot, f.path.leaf),
    /ERR_PATH/,
  );
  await assert.rejects(
    verifyPath(f.path, f.path.snapshot, "1"),
    /ERR_IDENTITY_PATH/,
  );
  const snapshot = { ...f.path.snapshot, height: "2" };
  await assert.rejects(
    verifyPath(f.path, snapshot, f.path.leaf),
    /ERR_SNAPSHOT_ID/,
  );
  const wrong = structuredClone(f.path);
  wrong.index = wrong.leaf_index;
  await assert.rejects(
    verifyPath(wrong, f.path.snapshot, f.path.leaf),
    /ERR_PATH/,
  );
});
after(async () => {
  const global = globalThis as typeof globalThis & {
    curve_bn128?: { terminate(): Promise<void> };
  };
  if (global.curve_bn128) await global.curve_bn128.terminate();
});

test("account-tree fixture preserves strict state schemas and a Go/TS state root", () => {
  const fixture = read("accounts") as AccountFixture & {
    expected_state: State;
  };
  decode("fixture", canonical(fixture));
  const final = fixture.expected_state;
  assert.equal(stateRoot(final.entries), fixture.expected_roots.at(-1));
  assert.throws(
    () => new Engine(canonical(fixture.genesis)),
    /ERR_ELIGIBILITY_ORACLE_UNAVAILABLE/,
  );
});
