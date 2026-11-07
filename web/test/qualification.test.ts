import test from "node:test";
import assert from "node:assert/strict";
import {
  readFileSync,
  mkdtempSync,
  writeFileSync,
  rmSync,
  rmdirSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createHash } from "node:crypto";
import { canonical } from "../src/protocol.js";
import {
  assessCampaign,
  artifactReader,
  scenarios,
  qualificationJSON,
  type Campaign,
} from "../../services/qualification/campaign.js";
const campaign = () =>
  JSON.parse(
    readFileSync("spec/qualification/campaign.example.json", "utf8"),
  ) as Campaign;
test("qualification stays blocked without real ten-million measurements and independent audits", () => {
  const c = campaign(),
    report = assessCampaign(c, {
      load: () => {
        throw Error("missing");
      },
      suiteQualified: false,
    });
  assert.equal(report.public_qualified, false);
  for (const name of [
    "real-campaign",
    "cryptographic-suite-audited-and-qualified",
    "independent-qualification-attestations",
    "external-cryptographic-and-application-audits",
    "physical-mobile-generation-p95",
  ])
    assert.equal(report.checks.find((c) => c.name === name)?.passed, false);
  assert.equal(
    report.checks.filter((c) => c.name.startsWith("scenario:")).length,
    scenarios.length,
  );
});
test("a high finding accepted as residual risk still blocks qualification", () => {
  const c = campaign();
  c.findings.push({
    id: "secret-leak",
    severity: "high",
    status: "accepted",
    remediation: null,
  });
  const report = assessCampaign(c, {
    load: () => new Uint8Array(),
    suiteQualified: true,
  });
  assert.equal(
    report.checks.find((c) => c.name === "finding:secret-leak")?.passed,
    false,
  );
  assert.equal(report.public_qualified, false);
});
test("throughput checks reject forged counters, omitted seconds and short spikes", () => {
  for (const corrupt of [false, true]) {
    const c = campaign();
    c.mode = "real";
    const stats = {
      format: "civic-receipt-measurements-1",
      network_id: c.network_id,
      poll_id: "11".repeat(32),
      window_start_ms: "1000",
      window_end_ms: "3000",
      partitions: "32",
      attempts: "10000",
      verified_unique_receipts: "10000",
      duplicates: "0",
      failed_attempts: "0",
      receipt_p95_ms: "1",
      receipt_p99_ms: "1",
      receipt_max_ms: "1",
      histogram_cap_ms: "60000",
      minimum_receipts_per_second: corrupt ? "5000" : "0",
      per_second: ["10000", "0"],
      per_lane: ["10000", ...Array(31).fill("0")],
      average_envelope_bytes: "8000",
      maximum_envelope_bytes: "8000",
      public_qualified: false,
    };
    c.receipts.peak = { path: "peak.json", sha256: "00".repeat(32), size: "1" };
    const report = assessCampaign(c, {
      load: () => Buffer.from(canonical(stats)),
      suiteQualified: true,
    });
    assert.equal(report.public_qualified, false);
    assert.ok(
      report.checks.some(
        (x) =>
          !x.passed &&
          x.name ===
            (corrupt
              ? "receipts:peak:consistent"
              : "5000-per-second-for-900-seconds"),
      ),
    );
  }
});
test("evidence reader pins bytes and refuses traversal, missing files and changed content", () => {
  const dir = mkdtempSync(join(tmpdir(), "civora-evidence-"));
  try {
    const bytes = Buffer.from("TEST_ONLY evidence");
    writeFileSync(join(dir, "artifact.txt"), bytes);
    const a = {
        path: "artifact.txt",
        sha256: createHash("sha256").update(bytes).digest("hex"),
        size: String(bytes.length),
      },
      read = artifactReader(dir);
    assert.deepEqual(Buffer.from(read(a)), bytes);
    assert.equal(read(a, false).length, 0);
    assert.throws(() => read({ ...a, sha256: "00".repeat(32) }));
    assert.throws(() => read({ ...a, path: "../external.txt" }));
    assert.throws(() => read({ ...a, size: "0" }));
  } finally {
    rmSync(join(dir, "artifact.txt"));
    rmdirSync(dir);
  }
});

test("qualification JSON refuses duplicate properties, numbers and ambiguous encodings", () => {
  for (const text of [
    '{"a":"1","a":"2"}',
    '{"count":10}',
    '{ "count":"10"}',
    '{"count":"10"}\n\n',
  ])
    assert.throws(() => qualificationJSON(Buffer.from(text)));
  assert.deepEqual(qualificationJSON(Buffer.from('{"count":"10"}\n')), {
    count: "10",
  });
});
