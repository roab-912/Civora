// Local qualification prerequisites only. No external target is contacted.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { resolve, join } from "node:path";
const args = process.argv.slice(2),
  index = args.indexOf("--fuzz-seconds"),
  seconds = index < 0 ? 20 : Number(args[index + 1]);
if (!Number.isInteger(seconds) || seconds < 1 || seconds > 3600)
  throw Error("--fuzz-seconds must be 1..3600");
const directory = resolve(".tools/qualification/v0.9-" + randomUUID());
mkdirSync(directory, { recursive: true });
const go = existsSync(".tools/go/bin/go.exe")
  ? resolve(".tools/go/bin/go.exe")
  : "go";
const env = {
  ...process.env,
  GOPATH: resolve(".tools/gopath"),
  GOMODCACHE: resolve(".tools/gomodcache"),
  GOCACHE: resolve(".tools/gocache"),
  GOFLAGS: "-buildvcs=false",
};
const steps = [];
async function run(name, command, argv, expected = 0) {
  const start = performance.now();
  console.log("Qualification prerequisite: " + name);
  const child = spawn(command, argv, {
    env,
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  });
  let output = "";
  child.stdout.on("data", (b) => (output += b));
  child.stderr.on("data", (b) => (output += b));
  const heartbeat = setInterval(
    () => console.log("Still running: " + name),
    30000,
  );
  heartbeat.unref();
  const code = await new Promise((yes, no) => {
    child.once("error", no);
    child.once("exit", yes);
  }).finally(() => clearInterval(heartbeat));
  writeFileSync(join(directory, name + ".log"), output);
  assert.equal(code, expected, output);
  steps.push({
    name,
    passed: true,
    elapsed_ms: String(Math.ceil(performance.now() - start)),
    exit_code: String(code),
    log: name + ".log",
  });
  return output;
}
if (!args.includes("--skip-unit"))
  await run("go-recovery-and-receipts", go, [
    "test",
    "./internal/recovery",
    "./internal/qualification",
    "-count=1",
    "-timeout=120s",
  ]);
await run("onchain-holds-priority-effects-partition-and-SIGKILL", go, [
  "test",
  "./internal/core",
  "./internal/consensus",
  "./internal/partition",
  "-run=^TestSecurity",
  "-count=1",
  "-timeout=150s",
]);
if (!args.includes("--skip-native"))
  await run("native-seven-node-recovery-hold", process.execPath, [
    "scripts/test-recovery-native.mjs",
  ]);
for (const [pkg, target] of [
  ["codec", "FuzzCanonical"],
  ["electionguard", "FuzzCompactBundle"],
  ["partition", "FuzzLogProofs"],
  ["recovery", "FuzzRecoveryCanonical"],
]) {
  if (args.includes("--skip-fuzz")) break;
  await run("fuzz-" + pkg, go, [
    "test",
    "./internal/" + pkg,
    "-run=^$",
    "-fuzz=^" + target + "$",
    "-fuzztime=" + seconds + "s",
    "-parallel=2",
    "-timeout=" + (seconds + 120) + "s",
  ]);
}
const exe = join(
  directory,
  "civrecovery" + (process.platform === "win32" ? ".exe" : ""),
);
await run("build-independent-verifier", go, [
  "build",
  "-o",
  exe,
  "./cmd/civrecovery",
]);
const root = "spec/fixtures/v0.9/root.TEST_ONLY.json",
  bundle = "spec/fixtures/v0.9/hold.TEST_ONLY.json",
  pin = createHash("sha3-384").update(readFileSync(root)).digest("hex"),
  db = join(directory, "counter.db"),
  now = "1793970000000";
const options = [
  "-root",
  root,
  "-root-pin",
  pin,
  "-bundle",
  bundle,
  "-store",
  db,
  "-allow-test-keys",
  "-now-ms",
  now,
];
const applied = JSON.parse(
  await run("offline-hold-no-consensus", exe, ["apply", ...options]),
);
assert.equal(applied.read_only, true);
assert.equal(applied.last_counter, "1");
await run(
  "offline-replay-refused-after-process-restart",
  exe,
  ["apply", ...options],
  1,
);
const status = JSON.parse(
  await run("offline-counter-still-durable", exe, ["status", ...options]),
);
assert.equal(status.last_counter, "1");
assert.equal(status.orders.length, 1);
await run(
  "test-root-refused-without-explicit-opt-in",
  exe,
  ["verify", "-root", root, "-root-pin", pin, "-bundle", bundle],
  1,
);
const receiptExe = join(
  directory,
  "civqual" + (process.platform === "win32" ? ".exe" : ""),
);
await run("build-receipt-collector", go, [
  "build",
  "-o",
  receiptExe,
  "./cmd/civqual",
]);
const contextFile = "spec/fixtures/v0.9/frozen.TEST_ONLY.json",
  contextPin = createHash("sha3-384")
    .update(readFileSync(contextFile))
    .digest("hex"),
  reportFile = join(directory, "receipts.TEST_ONLY.json");
await run(
  "collect-authenticated-receipts-with-duplicates-and-failures",
  receiptExe,
  [
    "-context",
    contextFile,
    "-context-pin",
    contextPin,
    "-samples",
    "spec/fixtures/v0.9/receipts.TEST_ONLY.jsonl",
    "-start-ms",
    "1000",
    "-end-ms",
    "4000",
    "-output",
    reportFile,
  ],
);
const measurements = JSON.parse(readFileSync(reportFile, "utf8"));
assert.equal(measurements.verified_unique_receipts, "1");
assert.equal(measurements.duplicates, "1");
assert.equal(measurements.failed_attempts, "2");
assert.equal(measurements.minimum_receipts_per_second, "0");
assert.equal(measurements.public_qualified, false);
await run(
  "collector-refuses-unauthenticated-context",
  receiptExe,
  [
    "-context",
    contextFile,
    "-context-pin",
    "0".repeat(96),
    "-samples",
    "spec/fixtures/v0.9/receipts.TEST_ONLY.jsonl",
    "-start-ms",
    "1000",
    "-end-ms",
    "4000",
  ],
  1,
);
await run(
  "campaign-cannot-qualify-missing-measurements",
  process.execPath,
  [
    "dist/services/qualification/cli.js",
    "--output",
    join(directory, "blocked-campaign.json"),
  ],
  2,
);
const report = {
  milestone: "v0.9",
  scope: "local_prerequisites_TEST_ONLY_not_public_qualification",
  passed: true,
  public_qualified: false,
  fuzz_seconds_per_target: args.includes("--skip-fuzz") ? "0" : String(seconds),
  steps,
  root_sha3_384: pin,
  limitations: [
    "No ten-million ballot/WAN campaign or external audit was performed.",
    "On-chain holds are exercised only in opt-in PUBLIC_TEST_ONLY genesis/configuration. Emergency reserve activation remains unavailable.",
    "The existing development genesis has no installed recovery root; no root was silently added to the running workshop.",
    "Normal root rotation is exercised with a TEST_ONLY constitutional approval adapter. A real authenticated approval adapter remains required.",
  ],
};
writeFileSync(
  join(directory, "report.json"),
  JSON.stringify(report, null, 2) + "\n",
);
writeFileSync(".tools/qualification/latest-v0.9.txt", directory + "\n");
console.log(JSON.stringify({ ...report, directory }, null, 2));
