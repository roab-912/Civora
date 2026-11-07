import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import {
  assessCampaign,
  artifactReader,
  qualificationJSON,
} from "./campaign.js";
const args = process.argv.slice(2),
  get = (key: string, fallback = "") => {
    const i = args.indexOf(key);
    if (i >= 0 && !args[i + 1]) throw Error("Missing " + key);
    return i < 0 ? fallback : args[i + 1]!;
  };
const file = resolve(
    get("--campaign", "spec/qualification/campaign.example.json"),
  ),
  policyFile = get("--policy"),
  pin = get("--policy-pin"),
  output = get("--output");
const suite = JSON.parse(
  readFileSync("spec/crypto/electionguard-v2.lock.json", "utf8"),
);
const policyBytes = policyFile ? readFileSync(policyFile) : undefined;
if (
  policyBytes &&
  (!/^[0-9a-f]{64}$/.test(pin) ||
    createHash("sha256").update(policyBytes).digest("hex") !== pin)
)
  throw Error("Independently obtained qualification policy pin required");
const report = assessCampaign(qualificationJSON(readFileSync(file)), {
  load: artifactReader(dirname(file)),
  suiteQualified: suite.public_pilot_qualified === true,
  ...(policyBytes
    ? {
        policy: qualificationJSON(policyBytes),
        policyPinVerified: true,
      }
    : {}),
});
const text = JSON.stringify(report, null, 2) + "\n";
if (output) writeFileSync(output, text);
process.stdout.write(text);
if (!report.public_qualified) process.exitCode = 2;
