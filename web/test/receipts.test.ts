import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { Ajv2020 } from "ajv/dist/2020.js";
import { installValidators } from "../src/pwa/protocol.js";
import {
  receiptDocumentType,
  inspectResultReport,
  compareResultReport,
} from "../src/pwa/receipts.js";
const ajv = new Ajv2020({ strict: true });
ajv.addSchema(
  JSON.parse(readFileSync("spec/schemas/protocol.schema.json", "utf8")),
);
ajv.addSchema(
  JSON.parse(
    readFileSync("spec/schemas/pwa_result_report.schema.json", "utf8"),
  ),
);
installValidators({
  v_pwa_result_report: ajv.getSchema(
    "https://civora.invalid/v0.1/pwa_result_report",
  )!,
});
const network = "11".repeat(32),
  genesis = "22".repeat(32),
  poll = "33".repeat(32);
const legacy = {
  network_id: network,
  poll_id: poll,
  yes: "3",
  no: "2",
  as_of_height: "15",
};

test("receipt import distinguishes individual ballots, result summaries, state proofs and account backups", () => {
  assert.equal(receiptDocumentType({ ballot: { raw: "test" } }), "ballot");
  assert.equal(receiptDocumentType(legacy), "result");
  assert.equal(
    receiptDocumentType({ ...legacy, format: "civora-result-report-1" }),
    "result",
  );
  assert.throws(
    () => receiptDocumentType({ proof: { path: {} } }),
    /STATE_PROOF/,
  );
  assert.throws(
    () => receiptDocumentType({ header: {}, ciphertext: "encrypted" }),
    /BACKUP/,
  );
  assert.throws(() => receiptDocumentType({ verified: true }), /FORMAT/);
  assert.throws(
    () => receiptDocumentType({ format: "future-format", ballot: {} }),
    /FORMAT/,
  );
});
test("result report validation accepts legacy and explicit exports and rejects foreign or malformed data", () => {
  assert.deepEqual(inspectResultReport(legacy, network, genesis), legacy);
  const explicit = {
    ...legacy,
    format: "civora-result-report-1",
    genesis_sha256: genesis,
  };
  assert.deepEqual(inspectResultReport(explicit, network, genesis), explicit);
  for (const change of [
    { yes: 3 },
    { yes: "03" },
    { extra: true },
    { as_of_height: "0" },
    { yes: "18446744073709551615", no: "1" },
    { verified: true },
  ])
    assert.throws(() =>
      inspectResultReport({ ...explicit, ...change }, network, genesis),
    );
  assert.throws(
    () =>
      inspectResultReport(
        { ...explicit, network_id: "ff".repeat(32) },
        network,
        genesis,
      ),
    /NETWORK/,
  );
  assert.throws(
    () =>
      inspectResultReport(
        { ...explicit, genesis_sha256: "ff".repeat(32) },
        network,
        genesis,
      ),
    /NETWORK/,
  );
});
test("uploaded report totals and metadata must match the independently checked report", () => {
  const uploaded = {
    ...legacy,
    format: "civora-result-report-1",
    genesis_sha256: genesis,
  };
  compareResultReport(uploaded, legacy);
  for (const change of [
    { yes: "4" },
    { poll_id: "ff".repeat(32) },
    { network_id: "ff".repeat(32) },
  ])
    assert.throws(
      () => compareResultReport({ ...uploaded, ...change }, legacy),
      /MISMATCH/,
    );
  assert.throws(
    () => compareResultReport({ ...uploaded, as_of_height: "14" }, legacy),
    /CHANGED/,
  );
  const partition = {
    ...legacy,
    dossier_id: "44".repeat(32),
    admitted: "6",
    counted: "5",
    state: "certified",
    verifier: "civic-typescript-v0.6",
    verified: true,
  };
  inspectResultReport(partition, network, genesis);
  compareResultReport(partition, partition);
  assert.throws(
    () => inspectResultReport({ ...partition, counted: "4" }, network, genesis),
    /MISMATCH/,
  );
  assert.throws(
    () =>
      compareResultReport({ ...partition, state: "provisional" }, partition),
    /MISMATCH/,
  );
});
