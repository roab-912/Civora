import { spawn, spawnSync } from "node:child_process";
import {
  readFileSync,
  mkdirSync,
  openSync,
  closeSync,
  writeFileSync,
} from "node:fs";
import { resolve, join } from "node:path";
import { randomUUID } from "node:crypto";
import { createServer } from "node:net";
import assert from "node:assert/strict";
const root = resolve("."),
  dir = join(root, ".tools", "acceptance", "network-" + randomUUID());
mkdirSync(dir, { recursive: true });
const exe = resolve(".tools/civnet.exe"),
  env = {
    ...process.env,
    GOPATH: join(root, ".tools/gopath"),
    GOMODCACHE: join(root, ".tools/gomodcache"),
    GOCACHE: join(root, ".tools/gocache"),
  };
const go =
  process.platform === "win32" ? resolve(".tools/go/bin/go.exe") : "go";
function run(args) {
  const r = spawnSync(go, args, { env, encoding: "utf8", windowsHide: true });
  if (r.status !== 0) throw Error(r.stderr || r.stdout);
}
run(["build", "-buildvcs=false", "-o", exe, "./cmd/civnet"]);
const servers = [];
let port;
for (let attempt = 0; attempt < 30; attempt++) {
  port = 28000 + Math.floor(Math.random() * 12000);
  try {
    for (let i = 0; i < 14; i++) {
      const s = createServer();
      servers.push(s);
      await new Promise((yes, no) =>
        s.once("error", no).listen(port + i, "127.0.0.1", yes),
      );
    }
    break;
  } catch {
    for (const s of servers) s.close();
    servers.length = 0;
    port = undefined;
  }
}
if (!port) throw Error("no free loopback port range");
await Promise.all(servers.map((s) => new Promise((done) => s.close(done))));
const networkDir = join(dir, "ledger");
let result = spawnSync(
  exe,
  ["init", "-dir", networkDir, "-port", String(port)],
  { encoding: "utf8", windowsHide: true },
);
if (result.status !== 0) throw Error(result.stderr);
const processes = new Map();
const pause = (ms) => new Promise((r) => setTimeout(r, ms));
let currentDirectory = networkDir;
let epochForSeat = () => 2;
let generationForSeat = () => 0;
function start(i) {
  const fd = openSync(join(dir, `node${i}-${Date.now()}.log`), "a"),
    p = spawn(
      exe,
      [
        "serve",
        "-home",
        join(currentDirectory, `node${i}`),
        "-supported-epoch",
        String(epochForSeat(i)),
        "-key-generation",
        String(generationForSeat(i)),
        "-verbose",
      ],
      { env, stdio: ["ignore", fd, fd], windowsHide: true },
    );
  closeSync(fd);
  processes.set(i, p);
  return p;
}
async function stop(i) {
  const p = processes.get(i);
  if (!p) return;
  processes.delete(i);
  if (p.exitCode !== null) return;
  const done = new Promise((r) => p.once("exit", r));
  p.kill("SIGKILL");
  await done;
}
async function rpc(i, method, params = {}) {
  const r = await fetch(`http://127.0.0.1:${port + 2 * i + 1}`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method, params }),
    signal: AbortSignal.timeout(2000),
  });
  const v = await r.json();
  if (v.error) throw Error(JSON.stringify(v.error));
  return v.result;
}
async function height(i = 0) {
  return Number((await rpc(i, "status")).sync_info.latest_block_height);
}
async function until(check, description, timeout = 45000) {
  const end = Date.now() + timeout;
  let last;
  while (Date.now() < end) {
    try {
      if (await check()) return;
    } catch (e) {
      last = e.message;
    }
    await pause(200);
  }
  throw Error(`${description}: ${last ?? "timeout"}; logs in ${dir}`);
}
const report = {
  version: "0.5-development",
  keys: "PUBLIC_TEST_KEYS_ONLY",
  quorum: "5/7",
  directory: dir,
  checks: [],
};
try {
  for (let i = 0; i < 7; i++) start(i);
  await until(async () => (await height()) >= 3, "seven nodes finalize");
  report.checks.push("seven-process-finality");
  const f = JSON.parse(readFileSync("spec/fixtures/accounts.json", "utf8"));
  const canonical = (v) =>
    v === null || typeof v !== "object"
      ? JSON.stringify(v)
      : Array.isArray(v)
        ? `[${v.map(canonical).join(",")}]`
        : `{${Object.keys(v)
            .sort()
            .map((k) => JSON.stringify(k) + ":" + canonical(v[k]))
            .join(",")}}`;
  const tx = Buffer.from(canonical(f.blocks[0].transactions[0])).toString(
    "base64",
  );
  const b = await rpc(0, "broadcast_tx_sync", { tx });
  assert.equal(b.code, 0);
  await until(async () => {
    const q = await rpc(0, "abci_query", { path: "/state" });
    return JSON.parse(Buffer.from(q.response.value, "base64")).entries.some(
      (e) => e.table === "accounts",
    );
  }, "registration commits");
  const invalid = await rpc(0, "broadcast_tx_sync", {
    tx: Buffer.from("{}").toString("base64"),
  });
  assert.notEqual(invalid.code, 0);
  report.checks.push("signed-account-and-invalid-transaction");
  const h = await height();
  await until(
    async () =>
      Math.min(
        ...(await Promise.all(Array.from({ length: 7 }, (_, i) => height(i)))),
      ) >= h,
    "replicas catch up",
  );
  const blocks = await Promise.all(
    Array.from({ length: 7 }, (_, i) => rpc(i, "block", { height: String(h) })),
  );
  assert.equal(new Set(blocks.map((b) => b.block_id.hash)).size, 1);
  assert.equal(new Set(blocks.map((b) => b.block.header.app_hash)).size, 1);
  report.checks.push("same-block-and-apphash-on-seven-replicas");
  const commit = await rpc(0, "commit", { height: String(h) });
  writeFileSync(join(dir, "commit.json"), JSON.stringify(commit));
  const chain = f.genesis.chain_id;
  run([
    "run",
    "-buildvcs=false",
    "./cmd/civnet",
    "verify-commit",
    "-in",
    join(dir, "commit.json"),
    "-chain",
    chain,
  ]);
  report.checks.push("native-commit-signatures-verified");
  await stop(5);
  await stop(6);
  const before = await height();
  await until(
    async () => (await height()) >= before + 3,
    "five nodes continue",
  );
  report.checks.push("two-offline-finality");
  await stop(4);
  await pause(4000);
  const stalled = await height();
  await pause(5000);
  assert.equal(await height(), stalled);
  report.checks.push("four-nodes-cannot-finalize");
  const signerPath = join(networkDir, "node4/data/priv_validator_state.json"),
    saved = JSON.parse(readFileSync(signerPath, "utf8"));
  start(4);
  await until(
    async () => (await height()) >= stalled + 3,
    "restarted signer catches up",
  );
  const after = JSON.parse(readFileSync(signerPath, "utf8"));
  assert.ok(BigInt(after.height) >= BigInt(saved.height));
  report.checks.push("abrupt-restart-with-persistent-sign-state");
  start(5);
  start(6);
  await until(
    async () =>
      Math.min(
        ...(await Promise.all(Array.from({ length: 7 }, (_, i) => height(i)))),
      ) >=
      stalled + 3,
    "seven replicas recover",
  );
  report.checks.push("seven-replicas-recover");
  await Promise.all([...processes.keys()].map(stop));
  const fixture = JSON.parse(
      readFileSync("spec/fixtures/v0.5/transport.json", "utf8"),
    ),
    lane = fixture.lanes.find((r) => r.blocks[1].transactions.length > 0),
    app = { kind: "partition", data: lane.genesis };
  currentDirectory = join(dir, "partition");
  writeFileSync(join(dir, "partition-app.json"), canonical(app));
  result = spawnSync(
    exe,
    [
      "init",
      "-dir",
      currentDirectory,
      "-port",
      String(port),
      "-app",
      join(dir, "partition-app.json"),
      "-chain",
      lane.genesis.chain_id,
    ],
    { encoding: "utf8", windowsHide: true },
  );
  if (result.status !== 0) throw Error(result.stderr);
  for (let i = 0; i < 7; i++) start(i);
  await until(async () => (await height()) >= 3, "partition finalizes");
  const opening = Buffer.from(
    lane.blocks[0].transactions[0],
    "base64url",
  ).toString("base64");
  assert.equal((await rpc(0, "broadcast_tx_sync", { tx: opening })).code, 0);
  await until(async () => {
    const q = await rpc(0, "abci_query", { path: "/state" });
    return !!JSON.parse(Buffer.from(q.response.value, "base64")).polls[
      fixture.frozen.context.poll_id
    ];
  }, "partition imports authenticated opening");
  // The 1970 replay fixture is closed under the live BFT clock: late deposit
  // must fail even with authentic eligibility and an archive certificate.
  const late = Buffer.from(
    lane.blocks[1].transactions[0],
    "base64url",
  ).toString("base64");
  assert.notEqual((await rpc(0, "broadcast_tx_sync", { tx: late })).code, 0);
  const q = await rpc(0, "abci_query", {
    path: "/proof",
    data: Buffer.from(
      canonical({
        table: "lane_checkpoint",
        key: fixture.frozen.context.poll_id,
      }),
    ).toString("hex"),
  });
  assert.equal(q.response.code, 0);
  const stateHeight = Number(q.response.height);
  await until(
    async () => (await height()) >= stateHeight + 1,
    "partition AppHash next header",
  );
  const signed = (await rpc(0, "commit", { height: String(stateHeight + 1) }))
    .signed_header;
  const stateProof = {
    height: String(stateHeight),
    signed_header: Buffer.from(JSON.stringify(signed)).toString("base64url"),
    path: JSON.parse(Buffer.from(q.response.value, "base64")),
  };
  writeFileSync(join(dir, "partition-state-proof.json"), canonical(stateProof));
  run([
    "run",
    "-buildvcs=false",
    "./cmd/civnet",
    "verify-state",
    "-chain",
    lane.genesis.chain_id,
    "-in",
    join(dir, "partition-state-proof.json"),
  ]);
  const trust = fixture.frozen.lanes.find(
    (t) => t.chain_id === lane.genesis.chain_id,
  );
  writeFileSync(join(dir, "partition-trust.json"), canonical(trust));
  const ts = spawnSync(
    process.execPath,
    [
      "scripts/verify-consensus-state.mjs",
      join(dir, "partition-trust.json"),
      join(dir, "partition-state-proof.json"),
    ],
    { encoding: "utf8", windowsHide: true },
  );
  assert.equal(ts.status, 0, ts.stderr);
  report.checks.push(
    "native-partition-opening-deadline-and-Go-TypeScript-apphash-proof",
  );
  await stop(0);
  start(0);
  await until(
    async () => (await height(0)) > stateHeight + 1,
    "partition abrupt restart",
  );
  report.checks.push("native-partition-abrupt-restart");
  await Promise.all([...processes.keys()].map(stop));
  const rotationPath = resolve("spec/fixtures/v0.5/rotation.app.json"),
    rotation = JSON.parse(readFileSync(rotationPath, "utf8"));
  currentDirectory = join(dir, "rotation");
  result = spawnSync(
    exe,
    [
      "init",
      "-dir",
      currentDirectory,
      "-port",
      String(port),
      "-app",
      rotationPath,
      "-chain",
      rotation.data.chain_id,
    ],
    { encoding: "utf8", windowsHide: true },
  );
  if (result.status !== 0) throw Error(result.stderr);
  for (let i = 0; i < 7; i++) start(i);
  await until(async () => (await height()) >= 8, "native key transition");
  const keysAt = async (h) =>
    (await rpc(0, "validators", { height: String(h) })).validators
      .map((v) => Buffer.from(v.pub_key.value, "base64").toString("hex"))
      .sort();
  const oldKeys = await keysAt(5),
    newKeys = await keysAt(6);
  assert.deepEqual(newKeys, rotation.consensus_rotations[0].keys.toSorted());
  assert.equal(oldKeys.filter((k) => !newKeys.includes(k)).length, 1);
  const rotatedCommit = await rpc(0, "commit", { height: "6" });
  writeFileSync(
    join(dir, "rotated-commit.json"),
    JSON.stringify(rotatedCommit),
  );
  result = spawnSync(
    exe,
    [
      "verify-commit",
      "-chain",
      rotation.data.chain_id,
      "-app",
      rotationPath,
      "-in",
      join(dir, "rotated-commit.json"),
    ],
    { encoding: "utf8", windowsHide: true },
  );
  assert.equal(result.status, 0, result.stderr);
  result = spawnSync(
    exe,
    [
      "verify-commit",
      "-chain",
      rotation.data.chain_id,
      "-in",
      join(dir, "rotated-commit.json"),
    ],
    { encoding: "utf8", windowsHide: true },
  );
  assert.notEqual(result.status, 0);
  report.checks.push("native-validator-rotation-at-H-plus-2");
  await stop(6);
  await stop(4);
  await stop(5);
  await pause(2000);
  const beforeNewSigner = await height();
  generationForSeat = (i) => (i === 6 ? 1 : 0);
  start(6);
  await until(
    async () => (await height()) >= beforeNewSigner + 3,
    "new validator identity signs with its own persistent state",
  );
  const newSigner = JSON.parse(
    readFileSync(
      join(
        currentDirectory,
        `node6/data/priv_validator_state.${rotation.consensus_rotations[0].keys[6]}.json`,
      ),
      "utf8",
    ),
  );
  assert.ok(BigInt(newSigner.height) > BigInt(beforeNewSigner));
  report.checks.push("native-new-key-signs-without-resetting-old-key-state");
  await Promise.all([...processes.keys()].map(stop));
  const activationPath = resolve("spec/fixtures/v0.5/activation.app.json"),
    activation = JSON.parse(readFileSync(activationPath, "utf8"));
  currentDirectory = join(dir, "activation");
  result = spawnSync(
    exe,
    [
      "init",
      "-dir",
      currentDirectory,
      "-port",
      String(port),
      "-app",
      activationPath,
      "-chain",
      activation.data.chain_id,
    ],
    { encoding: "utf8", windowsHide: true },
  );
  if (result.status !== 0) throw Error(result.stderr);
  epochForSeat = (i) => (i < 5 ? 2 : 1);
  generationForSeat = () => 0;
  for (let i = 0; i < 7; i++) start(i);
  await until(
    async () => (await height()) >= 6,
    "five upgraded nodes activate",
  );
  for (const i of [5, 6]) {
    const signer = JSON.parse(
      readFileSync(
        join(currentDirectory, `node${i}/data/priv_validator_state.json`),
        "utf8",
      ),
    );
    assert.ok(
      BigInt(signer.height) <= 2n,
      "old node signed past the epoch barrier",
    );
  }
  report.checks.push(
    "native-mixed-versions-old-signers-stop-before-activation",
  );
  await stop(5);
  await stop(6);
  epochForSeat = () => 2;
  start(5);
  start(6);
  await until(
    async () => Math.min(await height(5), await height(6)) >= 6,
    "upgraded nodes resume after barrier",
  );
  report.checks.push("native-upgraded-nodes-resume-from-durable-state");
  report.passed = true;
  writeFileSync(
    join(dir, "report.json"),
    JSON.stringify(report, null, 2) + "\n",
  );
  console.log(JSON.stringify(report, null, 2));
} finally {
  await Promise.all([...processes.keys()].map(stop));
}
