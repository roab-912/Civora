// Opt-in PUBLIC_TEST_ONLY recovery genesis, seven actual loopback CometBFT
// processes. This is a local exercise, not WAN/operator/production qualification.
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import {
  mkdirSync,
  readFileSync,
  writeFileSync,
  openSync,
  closeSync,
} from "node:fs";
import { resolve, join } from "node:path";
import { randomUUID } from "node:crypto";
import { createServer } from "node:net";
import { canonical } from "../dist/web/src/codec.js";
const directory = resolve(
  ".tools/qualification/recovery-native-" + randomUUID(),
);
mkdirSync(directory, { recursive: true });
const go =
    process.platform === "win32" ? resolve(".tools/go/bin/go.exe") : "go",
  exe = join(
    directory,
    "civnet" + (process.platform === "win32" ? ".exe" : ""),
  );
const env = {
  ...process.env,
  GOPATH: resolve(".tools/gopath"),
  GOMODCACHE: resolve(".tools/gomodcache"),
  GOCACHE: resolve(".tools/gocache"),
  GOFLAGS: "-buildvcs=false",
};
function run(command, args, overrides = {}) {
  const r = spawnSync(command, args, {
    env: { ...env, ...overrides },
    encoding: "utf8",
    windowsHide: true,
  });
  if (r.status !== 0) throw Error(r.stderr || r.stdout);
  return r.stdout;
}
const fixturePath = join(directory, "fixture.TEST_ONLY.json");
run(
  go,
  ["test", "./internal/core", "-run=^TestSecurityNativeFixture$", "-count=1"],
  { CIVORA_NATIVE_SAFETY_OUTPUT: fixturePath },
);
const f = JSON.parse(readFileSync(fixturePath, "utf8")),
  app = join(directory, "app.TEST_ONLY.json");
writeFileSync(app, canonical(f.app));
run(go, ["build", "-o", exe, "./cmd/civnet"]);
let port;
for (let attempt = 0; attempt < 40; attempt++) {
  const base = 28000 + Math.floor(Math.random() * 12000),
    servers = [];
  try {
    for (let i = 0; i < 14; i++) {
      const s = createServer();
      servers.push(s);
      await new Promise((yes, no) =>
        s.once("error", no).listen(base + i, "127.0.0.1", yes),
      );
    }
    port = base;
  } catch {
  } finally {
    await Promise.all(
      servers.map((s) => new Promise((r) => (s.listening ? s.close(r) : r()))),
    );
  }
  if (port) break;
}
if (!port) throw Error("no free loopback port range");
const homes = join(directory, "network");
run(exe, [
  "init",
  "-dir",
  homes,
  "-app",
  app,
  "-chain",
  f.app.data.chain_id,
  "-port",
  String(port),
]);
const children = new Map(),
  pause = (ms) => new Promise((r) => setTimeout(r, ms));
function start(i) {
  const fd = openSync(
    join(directory, "node-" + i + "-" + Date.now() + ".log"),
    "a",
  );
  const child = spawn(
    exe,
    ["serve", "-home", join(homes, "node" + i), "-supported-epoch", "2"],
    { env, stdio: ["ignore", fd, fd], windowsHide: true },
  );
  closeSync(fd);
  children.set(i, child);
}
async function stop(i) {
  const child = children.get(i);
  if (!child) return;
  children.delete(i);
  if (child.exitCode !== null) return;
  const done = new Promise((r) => child.once("exit", r));
  child.kill("SIGKILL");
  await done;
}
async function rpc(i, method, params = {}) {
  const r = await fetch(`http://127.0.0.1:${port + 2 * i + 1}`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method, params }),
    signal: AbortSignal.timeout(3000),
  });
  const v = await r.json();
  if (v.error) throw Error(JSON.stringify(v.error));
  return v.result;
}
const height = async (i) =>
  Number((await rpc(i, "status")).sync_info.latest_block_height);
async function until(fn, label) {
  const end = Date.now() + 60000;
  let error;
  while (Date.now() < end) {
    try {
      if (await fn()) return;
    } catch (e) {
      error = e.message;
    }
    await pause(200);
  }
  throw Error(label + ": " + (error ?? "timeout") + "; " + directory);
}
async function state(i) {
  const q = await rpc(i, "abci_query", { path: "/state" });
  return JSON.parse(Buffer.from(q.response.value, "base64").toString());
}
const send = (i, tx) =>
  rpc(i, "broadcast_tx_sync", {
    tx: Buffer.from(canonical(tx)).toString("base64"),
  });
async function refused(i, tx) {
  try {
    return (await send(i, tx)).code !== 0;
  } catch (e) {
    if (String(e).includes("tx already exists in cache")) return true;
    throw e;
  }
}
const checks = [];
try {
  for (let i = 0; i < 7; i++) start(i);
  await until(
    async () =>
      Math.min(
        ...(await Promise.all(Array.from({ length: 7 }, (_, i) => height(i)))),
      ) >= 3,
    "seven validators finalize recovery genesis",
  );
  checks.push(
    "seven-real-native-validators-finalize-explicit-test-recovery-genesis",
  );
  const registrations = f.transactions
    .flatMap((b) => b.transactions)
    .filter((t) => t.body.type === "account.register");
  assert.ok(registrations.length >= 1);
  const followup = f.transactions
    .flatMap((b) => b.transactions)
    .find((t) => t.body.type === "account.rotate");
  assert.ok(followup);
  assert.equal((await send(0, registrations[0])).code, 0);
  await until(
    async () => (await state(0)).entries.some((e) => e.table === "accounts"),
    "registration commits before hold",
  );
  const order = { type: "security.hold", value: f.hold };
  assert.equal((await send(0, order)).code, 0);
  await until(
    async () =>
      (await state(0)).entries.some(
        (e) => e.table === "security" && e.value.last_counter === "1",
      ),
    "hold commits",
  );
  checks.push("ML-DSA-dual-quorum-hold-commits-on-native-consensus");
  assert.notEqual((await send(0, followup)).code, 0);
  assert.equal(await refused(0, order), true);
  checks.push("new-account-signatures-and-order-replay-refused-after-hold");
  const common = await height(0);
  await until(
    async () =>
      Math.min(
        ...(await Promise.all(Array.from({ length: 7 }, (_, i) => height(i)))),
      ) >= common,
    "seven replicas catch up",
  );
  const blocks = await Promise.all(
    Array.from({ length: 7 }, (_, i) =>
      rpc(i, "block", { height: String(common) }),
    ),
  );
  assert.equal(new Set(blocks.map((b) => b.block_id.hash)).size, 1);
  assert.equal(new Set(blocks.map((b) => b.block.header.app_hash)).size, 1);
  checks.push("seven-replicas-agree-on-block-and-AppHash-after-hold");
  const commitPath = join(directory, "commit.json");
  writeFileSync(
    commitPath,
    JSON.stringify(await rpc(0, "commit", { height: String(common) })),
  );
  run(exe, ["verify-commit", "-in", commitPath, "-chain", f.app.data.chain_id]);
  checks.push("native-commit-signatures-verified-independently");
  await stop(6);
  start(6);
  await until(
    async () => (await height(6)) >= common,
    "abruptly restarted validator recovers",
  );
  assert.ok(
    (await state(6)).entries.some(
      (e) => e.table === "security" && e.value.last_counter === "1",
    ),
  );
  assert.notEqual((await send(6, followup)).code, 0);
  assert.equal(await refused(6, order), true);
  checks.push("SIGKILL-restart-preserves-hold-counter-and-signing-state");
  const report = {
    milestone: "v0.9",
    scope: "PUBLIC_TEST_ONLY_LOCAL_NATIVE_CONSENSUS",
    passed: true,
    public_qualified: false,
    checks,
    limitations: [
      "Seven local processes share one operator/host; WAN, physical key custody and independent operators are not qualified.",
      "Root configuration and keys are explicit development fixtures; no existing workshop genesis is changed.",
    ],
  };
  writeFileSync(
    join(directory, "report.json"),
    JSON.stringify(report, null, 2) + "\n",
  );
  writeFileSync(
    ".tools/qualification/latest-recovery-native.txt",
    directory + "\n",
  );
  console.log(JSON.stringify({ ...report, directory }, null, 2));
} finally {
  await Promise.all([...children.keys()].map(stop));
}
