import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { auditRecord } from "../dist/web/src/citizen-audit.js";
import {
  canonical,
  decode,
  domain,
  signDigest,
  testKey,
} from "../dist/web/src/protocol.js";
const args = process.argv.slice(2),
  get = (name, defaultValue = "") => {
    const i = args.indexOf(name);
    return i < 0 ? defaultValue : args[i + 1];
  },
  input = get("-input"),
  manifestPath = get("-manifest"),
  output = get("-output"),
  keyPath = get("-attest-key");
if (!input || !manifestPath)
  throw Error(
    "usage: audit-citizen -input record-directory -manifest authenticated-network.json [-attest-key TEST_ONLY.json] [-output file]",
  );
try {
  const manifest = decode(
      "citizen_manifest",
      readFileSync(manifestPath, "utf8"),
    ),
    record = decode(
      "citizen_election_record",
      readFileSync(join(input, "record.json"), "utf8"),
    );
  const report = await auditRecord(
    record,
    (ref) =>
      readFileSync(join(input, "objects", ref.sha256.slice(0, 2), ref.sha256)),
    manifest.main.genesis.sha256,
  );
  let result = report;
  if (keyPath) {
    if (!["provisional", "certified"].includes(report.state))
      throw Error("cannot attest a contested or expired dossier");
    const key = decode("citizen_test_key", readFileSync(keyPath, "utf8"));
    if (
      testKey(key.seed).publicKey !== key.public_key ||
      !record.frozen.audit?.keys.includes(key.public_key)
    )
      throw Error("auditor key is not authorized");
    const statement = {
      poll_id: report.poll_id,
      dossier_id: report.dossier_id,
      implementation: "civic-typescript-v0.6",
      signer: key.public_key,
    };
    const message = domain("audit/attest", [
      record.frozen.context.network_id,
      report.poll_id,
      record.frozen.context.protocol_epoch,
      report.dossier_id,
      statement.implementation,
    ]);
    result = { ...statement, signature: signDigest(message, key.seed) };
  }
  const bytes = canonical(result);
  if (output) writeFileSync(output, bytes);
  else console.log(bytes);
} finally {
  if (globalThis.curve_bn128) await globalThis.curve_bn128.terminate();
}
