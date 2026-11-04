import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { canonical } from "../src/codec.js";
import { validate } from "../src/protocol.js";
import {
  checkCeremonyLink,
  decodeBundle,
  decodeBundleBytes,
  inspectBundle,
  maxBundleBytes,
  maxFileBytes,
  maxTotalFileBytes,
  type BundleKind,
} from "../src/electionguard-bundle.js";
const fixture = JSON.parse(
  readFileSync("spec/fixtures/v0.4/envelopes.json", "utf8"),
) as {
  context: { network_id: string; poll_id: string; proposal_id: string };
  vectors: {
    name: string;
    kind: BundleKind;
    encoded: string;
    accept: boolean;
    check_link: boolean;
    inspection?: unknown;
  }[];
};
test("ElectionGuard qualification gate fails closed independently of envelope inspection", () => {
  const result = spawnSync(
    process.execPath,
    ["scripts/review-electionguard.mjs", "--require-qualified"],
    { encoding: "utf8" },
  );
  assert.equal(result.status, 2, result.stderr);
  const review = JSON.parse(result.stdout) as {
    qualified: boolean;
    status: string;
    source_integrity: string;
  };
  assert.equal(review.qualified, false);
  assert.equal(review.status, "blocked_interoperability");
  assert.equal(review.source_integrity, "not_checked");
});
for (const v of fixture.vectors)
  test("EG envelope only: " + v.name, async () => {
    const check = async () => {
      const d = decodeBundle(v.encoded, v.kind);
      validate("eg_bundle", d.bundle);
      if (v.check_link)
        await checkCeremonyLink(
          d,
          fixture.context.network_id,
          fixture.context.poll_id,
          fixture.context.proposal_id,
        );
      return d;
    };
    if (!v.accept) {
      await assert.rejects(check);
      return;
    }
    const d = await check();
    assert.deepEqual(await inspectBundle(d, v.kind), v.inspection);
    assert.equal((await inspectBundle(d, v.kind)).cryptography_verified, false);
    if (v.kind === "ceremony")
      assert.equal(
        new TextDecoder().decode(d.files.get("context.json")),
        '{"n":5, "k":3}\n',
      );
  });
test("EG envelope allocation bounds, individual and cumulative file sizes", () => {
  assert.throws(() =>
    decodeBundle("A".repeat(Math.ceil((maxBundleBytes * 8) / 6) + 1), "ballot"),
  );
  assert.throws(() =>
    decodeBundleBytes(new Uint8Array(maxBundleBytes + 1), "ballot"),
  );
  const data = (n: number) => Buffer.alloc(n, "x").toString("base64url");
  const bundle = (files: { path: string; bytes: string }[]) =>
    new TextEncoder().encode(canonical({ suite: "eg-2.0-civic-1", files }));
  decodeBundleBytes(
    bundle([{ path: "ballot.json", bytes: data(maxFileBytes) }]),
    "ballot",
  );
  assert.throws(() =>
    decodeBundleBytes(
      bundle([{ path: "ballot.json", bytes: data(maxFileBytes + 1) }]),
      "ballot",
    ),
  );
  const names = [
    "context.json",
    ...Array.from({ length: 5 }, (_, i) => `guardians/0${i + 1}.json`),
    "link.json",
    "manifest.json",
  ];
  const total = (size: number) =>
    bundle(
      names.map((path, i) => ({
        path,
        bytes: data(i < 7 ? 12000 : size - 7 * 12000),
      })),
    );
  assert.ok(total(maxTotalFileBytes + 1).length < maxBundleBytes);
  decodeBundleBytes(total(maxTotalFileBytes), "ceremony");
  assert.throws(() =>
    decodeBundleBytes(total(maxTotalFileBytes + 1), "ceremony"),
  );
});
