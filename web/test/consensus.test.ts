import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { blob, canonical, decode, sha, validate } from "../src/protocol.js";
import {
  logRoot,
  verifyConsistency,
  verifyHeader,
  verifyInclusion,
  verifyState,
  type Trust,
  type StateProof,
} from "../src/consensus.js";
interface BatchProof {
  reference: { root: string; count: string; start: string };
  inclusion: string[];
}
interface Checkpoint {
  proof: StateProof;
  extension: BatchProof[];
  admission_consistency: string[];
  batch_consistency: string[];
}
interface Run {
  genesis: { source: Trust };
  blocks: { transactions: string[] }[];
}
const f = JSON.parse(
  readFileSync("spec/fixtures/v0.5/transport.json", "utf8"),
) as {
  opening: StateProof;
  frozen: { lanes: Trust[] };
  lanes: Run[];
  early: Checkpoint[];
  final: Checkpoint[];
};
test("v0.5: independent native commit and H+1 sparse proofs", () => {
  const source = f.lanes[0]!.genesis.source;
  assert.equal(verifyState(source, f.opening).height, "6");
  for (let i = 0; i < f.early.length; i++) {
    verifyState(f.frozen.lanes[i]!, f.early[i]!.proof);
    verifyState(f.frozen.lanes[i]!, f.final[i]!.proof);
  }
  assert.throws(() => verifyState(source, { ...f.opening, height: "4" }));
  assert.throws(() =>
    verifyState({ ...source, chain_id: "foreign" }, f.opening),
  );
  const bad = structuredClone(f.opening);
  bad.path.siblings[3] = "00".repeat(32);
  assert.throws(() => verifyState(source, bad));
});
test("v0.5: native ZIP-215 accepts an unreduced R encoding and binds its exact bytes", () => {
  const f = JSON.parse(
    readFileSync("spec/fixtures/v0.5/zip215.json", "utf8"),
  ) as { trust: Trust; signed_header: string };
  assert.equal(verifyHeader(f.trust, f.signed_header).height, "9");
  const sh = JSON.parse(blob(f.signed_header).toString()) as {
    commit: { signatures: { signature: string }[] };
  };
  const s = sh.commit.signatures[0]!,
    signature = Buffer.from(s.signature, "base64");
  signature.fill(0, 0, 32);
  signature[0] = 1;
  s.signature = signature.toString("base64");
  assert.throws(() =>
    verifyHeader(
      f.trust,
      Buffer.from(JSON.stringify(sh)).toString("base64url"),
    ),
  );
});
test("v0.5: native signatures, quorum and header hash cannot be relabelled", () => {
  const trust = f.lanes[0]!.genesis.source;
  const original = JSON.parse(blob(f.opening.signed_header).toString()) as {
    header: { app_hash: string };
    commit: {
      signatures: {
        block_id_flag: number;
        validator_address: string;
        signature: string | null;
        timestamp: string;
      }[];
    };
  };
  for (const attack of ["header", "signature", "quorum"]) {
    const sh = structuredClone(original);
    if (attack === "header") sh.header.app_hash = "00".repeat(32);
    else if (attack === "signature")
      sh.commit.signatures.find((s) => s.block_id_flag === 2)!.signature =
        Buffer.alloc(64).toString("base64");
    else {
      const s = sh.commit.signatures.find((s) => s.block_id_flag === 2)!;
      s.block_id_flag = 1;
      s.validator_address = "";
      s.signature = null;
      s.timestamp = "0001-01-01T00:00:00Z";
    }
    assert.throws(
      () =>
        verifyHeader(
          trust,
          Buffer.from(JSON.stringify(sh)).toString("base64url"),
        ),
      attack,
    );
  }
});
test("v0.5: RFC9162 roots and batch paths agree with Go fixtures", () => {
  for (let lane = 0; lane < f.lanes.length; lane++) {
    const cp = f.early[lane]!,
      summary = cp.proof.path.entry.value as {
        admission_root: string;
        batch_root: string;
        admitted_count: string;
        batch_count: string;
      };
    const batches = f.lanes[lane]!.blocks[1]!.transactions.map(
      (tx) =>
        decode<{ type: string; value: { envelopes: string[] } }>(
          "partition_tx",
          blob(tx),
        ).value,
    );
    const items = batches.flatMap((b) => b.envelopes.map(blob));
    assert.equal(logRoot(items), summary.admission_root);
    for (let j = 0; j < cp.extension.length; j++) {
      const b = cp.extension[j]!;
      assert.equal(logRoot(batches[j]!.envelopes.map(blob)), b.reference.root);
      verifyInclusion(
        summary.batch_root,
        Buffer.from(canonical(b.reference)),
        BigInt(j),
        BigInt(summary.batch_count),
        b.inclusion,
      );
    }
    verifyConsistency(
      logRoot([]),
      summary.admission_root,
      0n,
      BigInt(summary.admitted_count),
      cp.admission_consistency,
    );
    verifyConsistency(
      summary.admission_root,
      summary.admission_root,
      BigInt(summary.admitted_count),
      BigInt(summary.admitted_count),
      f.final[lane]!.admission_consistency,
    );
  }
});
test("v0.5: RFC9162 verifies nonempty extension and refuses a fork", () => {
  const xs = ["a", "b", "c"].map((s) => Buffer.from(s)),
    hash = xs.map((x) => sha(Buffer.from([0]), x).toString("hex"));
  verifyConsistency(logRoot(xs.slice(0, 2)), logRoot(xs), 2n, 3n, [hash[2]!]);
  verifyConsistency(logRoot(xs.slice(0, 1)), logRoot(xs), 1n, 3n, [
    hash[1]!,
    hash[2]!,
  ]);
  verifyInclusion(logRoot(xs), xs[1]!, 1n, 3n, [hash[0]!, hash[2]!]);
  assert.throws(() =>
    verifyConsistency(logRoot(xs.slice(0, 2)), logRoot(xs), 2n, 3n, [hash[1]!]),
  );
  assert.throws(() =>
    verifyInclusion(logRoot(xs), xs[2]!, 1n, 3n, [hash[0]!, hash[2]!]),
  );
});
test("v0.5: transport, genesis and proof schemas reject extensions", () => {
  validate(
    "partition_frozen",
    JSON.parse(readFileSync("spec/fixtures/v0.5/transport.json", "utf8"))
      .frozen,
  );
  for (const p of [...f.early, ...f.final]) validate("partition_checkpoint", p);
  for (const file of ["activation", "rotation"]) {
    const g = JSON.parse(
      readFileSync(`spec/fixtures/v0.5/${file}.app.json`, "utf8"),
    );
    validate("network_genesis", g);
    assert.throws(() =>
      validate("network_genesis", {
        ...g,
        data: { ...g.data, unrecognised: true },
      }),
    );
  }
  assert.throws(() =>
    validate("consensus_state_proof", { ...f.opening, unrecognised: true }),
  );
  assert.throws(() =>
    validate("partition_checkpoint", { ...f.early[0], previous_id: "0" }),
  );
});
