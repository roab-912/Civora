// Rebuilds a real ten-million-leaf tree, then checks MVCC mutation, restart,
// HTTP paths and proof generation in both browser engines. No vote is counted.
import {
  readFileSync,
  writeFileSync,
  mkdirSync,
  existsSync,
  readdirSync,
  statSync,
} from "node:fs";
import { resolve } from "node:path";
import { spawn, spawnSync } from "node:child_process";
import { randomUUID } from "node:crypto";
const go = existsSync(".tools/go/bin/go.exe")
  ? resolve(".tools/go/bin/go.exe")
  : "go";
const env = {
  ...process.env,
  GOPATH: resolve(".tools/gopath"),
  GOMODCACHE: resolve(".tools/gomodcache"),
  GOCACHE: resolve(".tools/gocache"),
};
mkdirSync(".tools/bin", { recursive: true });
mkdirSync(".tools/qualification", { recursive: true });
const binary = resolve(
  ".tools/bin/civeligibility" + (process.platform === "win32" ? ".exe" : ""),
);
const build = spawnSync(
  go,
  ["build", "-buildvcs=false", "-o", binary, "./cmd/civeligibility"],
  { env, encoding: "utf8" },
);
if (build.status !== 0) throw Error(build.stderr);
const directory = resolve(".tools/qualification/scale-" + randomUUID());
const fixture = JSON.parse(
  readFileSync("spec/fixtures/v0.3/membership-26.json", "utf8"),
);
const run = (args) =>
  new Promise((resolve, reject) => {
    let output = "";
    const child = spawn(binary, args, { stdio: ["ignore", "pipe", "inherit"] });
    child.stdout.on("data", (data) => (output += data));
    child.on("error", reject);
    child.on("exit", (code) => {
      if (code !== 0) reject(Error("Eligibility command failed: " + code));
      else {
        try {
          resolve(JSON.parse(output));
        } catch (error) {
          reject(error);
        }
      }
    });
  });
const tree = await run([
  "build-fixture",
  "-store",
  directory,
  "-count",
  "10000000",
  "-depth",
  "26",
  "-special-leaf",
  fixture.path.leaf,
]);
const exercise = await run(["exercise-fixture", "-store", directory]);
const report = {
  build: tree,
  exercise,
  base_bytes: String(
    readdirSync(directory + "/base").reduce(
      (sum, name) => sum + statSync(directory + "/base/" + name).size,
      0,
    ),
  ),
  notes:
    "Eligibility tree and membership only; not a mass-voting or physical-device qualification.",
};
writeFileSync(
  ".tools/qualification/scale-v0.3.json",
  JSON.stringify(report, null, 2) + "\n",
);
const browsers = spawnSync(
  process.execPath,
  ["scripts/test-eligibility-browsers.mjs"],
  { env: { ...env, CIVORA_SCALE_STORE: directory }, stdio: "inherit" },
);
if (browsers.status !== 0) throw Error("Scale-path browser proofs failed");
writeFileSync(
  ".tools/qualification/latest-scale-directory.txt",
  directory + "\n",
);
console.log(
  JSON.stringify({
    active_leaves: tree.snapshot.active_count,
    root: tree.snapshot.root,
    reopened: exercise.reopened,
    report: ".tools/qualification/scale-v0.3.json",
  }),
);
