import assert from "node:assert/strict";
import test, { after } from "node:test";
import { readFileSync } from "node:fs";
import {
  verifyCeremony,
  verifyBallot,
  verifyTally,
  zeroAggregate,
  addAggregate,
  type ElectionContext,
} from "../src/electionguard.js";
import {
  verifyScale,
  type ScaleContext,
  type ScaleEnvelope,
  lane,
} from "../src/electionguard-scale.js";
import { decodeBundle } from "../src/electionguard-bundle.js";
interface PublicCase {
  label: string;
  context: ElectionContext;
  ceremony_bundle: string;
  ballots: string[];
  aggregate: string;
  count: string;
  tally_bundle: string;
  yes: string;
  no: string;
}
const fixture = JSON.parse(
  readFileSync("spec/fixtures/v0.4/crypto.json", "utf8"),
) as { cases: PublicCase[] };
after(async () => {
  const curves = globalThis as typeof globalThis & {
    curve_bn128?: { terminate(): Promise<void> };
  };
  if (curves.curve_bn128) await curves.curve_bn128.terminate();
});
for (const f of fixture.cases)
  test("ElectionGuard 2.0 independent SDK vector: " + f.label, async () => {
    const e = await verifyCeremony(f.context, f.ceremony_bundle);
    let aggregate = zeroAggregate();
    for (const ballot of f.ballots) {
      const b = await verifyBallot(e, ballot);
      aggregate = addAggregate(aggregate, b.ciphertext);
    }
    assert.equal(aggregate, f.aggregate);
    assert.deepEqual(await verifyTally(e, aggregate, f.count, f.tally_bundle), {
      yes: f.yes,
      no: f.no,
    });
  });
test("ElectionGuard cryptographic attack corpus agrees with Go", async () => {
  const f = fixture.cases[0]!,
    e = await verifyCeremony(f.context, f.ceremony_bundle);
  const attacks = JSON.parse(
    readFileSync("spec/fixtures/v0.4/reject-crypto.json", "utf8"),
  ) as {
    cases: {
      name: string;
      stage: string;
      encoded: string;
      aggregate?: string;
    }[];
  };
  for (const n of attacks.cases) {
    const action = async () => {
      if (n.stage === "ceremony") await verifyCeremony(f.context, n.encoded);
      else if (n.stage === "ballot") await verifyBallot(e, n.encoded);
      else await verifyTally(e, n.aggregate ?? f.aggregate, f.count, n.encoded);
    };
    await assert.rejects(action, n.name);
  }
});
test("scale_1: valid nonce knowledge, copy under another nullifier, routing and checkpoint freshness", async () => {
  const f = fixture.cases[0]!,
    e = await verifyCeremony(f.context, f.ceremony_bundle);
  const vector = JSON.parse(
    readFileSync("spec/fixtures/v0.4/scale.json", "utf8"),
  ) as {
    context: ScaleContext;
    valid: ScaleEnvelope;
    copy_attack: ScaleEnvelope;
  };
  await verifyScale(e, vector.context, vector.valid);
  await assert.rejects(
    () => verifyScale(e, vector.context, vector.copy_attack),
    /INDEPENDENCE/,
  );
  for (const change of [
    { lane_id: "999" },
    { checkpoint_id: "ff".repeat(32) },
    { protocol_epoch: "2" },
    { poll_id: "ff".repeat(32) },
    { network_id: "ff".repeat(32) },
  ])
    await assert.rejects(() =>
      verifyScale(e, vector.context, { ...vector.valid, ...change }),
    );
  for (const count of ["1", "2", "4", "32", "256"])
    assert.ok(
      BigInt(
        await lane(
          vector.context.network_id,
          vector.context.poll_id,
          vector.valid.membership.nullifier,
          count,
        ),
      ) < BigInt(count),
    );
  for (const count of ["0", "3", "257"])
    await assert.rejects(() =>
      lane(
        vector.context.network_id,
        vector.context.poll_id,
        vector.valid.membership.nullifier,
        count,
      ),
    );
});
