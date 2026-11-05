import assert from "node:assert/strict";
import test, { after } from "node:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  auditRecord,
  verifyPublicState,
  type ElectionRecord,
  type Ref,
} from "../src/citizen-audit.js";
import { decode } from "../src/protocol.js";
const directory = "spec/fixtures/v0.6/record",
  record = decode<ElectionRecord>(
    "citizen_election_record",
    readFileSync(join(directory, "record.json"), "utf8"),
  );
const object = (ref: Ref) =>
  readFileSync(join(directory, "objects", ref.sha256.slice(0, 2), ref.sha256));
const manifest = JSON.parse(
  readFileSync("spec/fixtures/v0.6/network.json", "utf8"),
) as { main: { genesis: Ref } };
after(async () => {
  const curves = globalThis as typeof globalThis & {
    curve_bn128?: { terminate(): Promise<void> };
  };
  if (curves.curve_bn128) await curves.curve_bn128.terminate();
});
test("selective archive independently replays all admitted bytes, empty lanes and certification", async () => {
  const report = await auditRecord(
    record,
    object,
    manifest.main.genesis.sha256,
  );
  assert.equal(report.state, "certified");
  assert.equal(report.admitted, "6");
  assert.equal(report.counted, "5");
  assert.equal(report.yes, "3");
  assert.equal(report.no, "2");
  assert.equal(report.as_of_height, "14");
  assert.equal(
    report.dossier_id,
    "68b715384c3c5806959f8c17b82723a2f811af2472bde6a30d2cdec4d3cff0b6",
  );
});
test("selective audit rejects missing lanes, omitted batches, corrupt objects and untrusted genesis", async () => {
  for (const mutate of [
    (r: ElectionRecord) => {
      r.lanes.pop();
    },
    (r: ElectionRecord) => {
      r.lanes.find((l) => l.batches.length)!.batches = [];
    },
    (r: ElectionRecord) => {
      r.certificate!.height = "1";
    },
    (r: ElectionRecord) => {
      r.status = r.result;
    },
  ]) {
    const bad = structuredClone(record);
    mutate(bad);
    await assert.rejects(() =>
      auditRecord(bad, object, manifest.main.genesis.sha256),
    );
  }
  await assert.rejects(() => auditRecord(record, object, "0".repeat(64)));
  await assert.rejects(() =>
    auditRecord(
      record,
      () => Buffer.from("corrupted"),
      manifest.main.genesis.sha256,
    ),
  );
});
test("public state proofs require H+1 and leaf-to-root siblings", () => {
  const gen = JSON.parse(object(record.genesis).toString()) as {
      chain_id: string;
      validators: { pub_key: { value: string } }[];
    },
    trust = {
      chain_id: gen.chain_id,
      keys: gen.validators.map((v) =>
        Buffer.from(v.pub_key.value, "base64").toString("hex"),
      ),
    };
  verifyPublicState(trust, record.result);
  const wrong = structuredClone(record.result);
  wrong.path.siblings.reverse();
  assert.throws(() => verifyPublicState(trust, wrong));
  wrong.path = record.result.path;
  wrong.height = String(BigInt(wrong.height) + 1n);
  assert.throws(() => verifyPublicState(trust, wrong));
});
