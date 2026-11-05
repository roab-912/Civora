import { spawn, spawnSync } from "node:child_process";
import {
  readFileSync,
  writeFileSync,
  mkdirSync,
  openSync,
  closeSync,
  readdirSync,
} from "node:fs";
import { join, resolve } from "node:path";
import { createServer as httpServer } from "node:http";
import { createServer } from "node:net";
import { randomUUID } from "node:crypto";
import assert from "node:assert/strict";
import { canonical, sha } from "../dist/web/src/protocol.js";
import { verifyPublicState } from "../dist/web/src/citizen-audit.js";

const root = resolve("."),
  dir = join(root, ".tools/acceptance", "citizen-" + randomUUID());
mkdirSync(dir, { recursive: true });
const env = {
  ...process.env,
  GOPATH: join(root, ".tools/gopath"),
  GOMODCACHE: join(root, ".tools/gomodcache"),
  GOCACHE: join(root, ".tools/gocache"),
};
const go =
    process.platform === "win32" ? resolve(".tools/go/bin/go.exe") : "go",
  node = join(dir, "civnode.exe"),
  net = join(dir, "civnet.exe");
function run(exe, args) {
  const r = spawnSync(exe, args, { env, windowsHide: true, encoding: "utf8" });
  if (r.status !== 0) throw Error(r.stderr || r.stdout);
  return r.stdout;
}
run(go, ["build", "-buildvcs=false", "-o", node, "./cmd/civnode"]);
run(go, ["build", "-buildvcs=false", "-o", net, "./cmd/civnet"]);
let port;
for (let trial = 0; trial < 30; trial++) {
  const candidate = 30000 + Math.floor(Math.random() * 25000),
    ls = [];
  try {
    for (let i = 0; i < 32; i++) {
      const l = createServer();
      ls.push(l);
      await new Promise((y, n) =>
        l.once("error", n).listen(candidate + i, "127.0.0.1", y),
      );
    }
    port = candidate;
  } catch {
  } finally {
    await Promise.all(
      ls.filter((l) => l.listening).map((l) => new Promise((y) => l.close(y))),
    );
  }
  if (port) break;
}
if (!port) throw Error("free ports unavailable");
const processes = new Set();
function start(exe, args, name) {
  const fd = openSync(join(dir, name + ".log"), "a"),
    p = spawn(exe, args, { env, windowsHide: true, stdio: ["ignore", fd, fd] });
  closeSync(fd);
  processes.add(p);
  return p;
}
async function stop(p) {
  processes.delete(p);
  if (p.exitCode !== null) return;
  const done = new Promise((y) => p.once("exit", y));
  p.kill("SIGKILL");
  await done;
}
const pause = (ms) => new Promise((y) => setTimeout(y, ms));
async function until(fn, description) {
  let last;
  for (let i = 0; i < 200; i++) {
    try {
      if (await fn()) return;
    } catch (e) {
      last = e.message;
    }
    await pause(200);
  }
  throw Error(description + ": " + last + "; " + dir);
}
const fixture = JSON.parse(
    readFileSync("spec/fixtures/v0.6/archive.json", "utf8"),
  ),
  manifest = structuredClone(fixture.manifest);
manifest.lanes = manifest.lanes.slice(0, 1);
const genesis = new Map(),
  servers = [];
try {
  for (let i = 0; i < 2; i++) {
    const runFixture = fixture.runs[i],
      app = JSON.parse(
        Buffer.from(runFixture.genesis, "base64url").toString(),
      ).app_state,
      appPath = join(dir, `app${i}.json`),
      network = join(dir, `network${i}`);
    writeFileSync(appPath, canonical(app));
    run(net, [
      "init",
      "-dir",
      network,
      "-chain",
      runFixture.chain_id,
      "-app",
      appPath,
      "-port",
      String(port + 14 * i),
    ]);
    const raw = readFileSync(join(network, "node0/config/genesis.json"));
    genesis.set(runFixture.chain_id, raw);
    const descriptor = i === 0 ? manifest.main : manifest.lanes[0];
    descriptor.genesis = {
      sha256: sha(raw).toString("hex"),
      size: String(raw.length),
    };
    descriptor.peers = JSON.parse(
      readFileSync(join(network, "node0/development.json"), "utf8"),
    ).peers;
    for (let seat = 0; seat < 7; seat++)
      start(
        net,
        ["serve", "-home", join(network, `node${seat}`)],
        `validator-${i}-${seat}`,
      );
  }
  for (let i = 0; i < 2; i++) {
    const server = httpServer((req, res) => {
      const id = req.url.split("/")[3],
        raw = genesis.get(id);
      if (raw && req.url.endsWith("/genesis")) {
        res.end(raw);
      } else {
        res.statusCode = 404;
        res.end();
      }
    });
    await new Promise((y) => server.listen(0, "127.0.0.1", y));
    servers.push(server);
  }
  const origins = servers.map((s) => ({
    api: `http://127.0.0.1:${s.address().port}`,
    rpc: `http://127.0.0.1:${port + 1}`,
  }));
  manifest.main.origins = origins;
  manifest.lanes[0].origins = origins;
  const manifestPath = join(dir, "network.json"),
    genesisPath = join(dir, "genesis.json"),
    home = join(dir, "observer");
  writeFileSync(manifestPath, canonical(manifest));
  writeFileSync(genesisPath, genesis.get(manifest.main.chain_id));
  run(node, [
    "init",
    "--home",
    home,
    "--manifest",
    manifestPath,
    "--genesis",
    genesisPath,
  ]);
  let observer;
  const launch = () =>
    start(
      node,
      [
        "start",
        "--home",
        home,
        "--mode",
        "archive",
        "--lanes",
        "0",
        "--p2p-port",
        String(port + 28),
        "--listen",
        "127.0.0.1:0",
        "--workers=false",
      ],
      "observer",
    );
  observer = launch();
  async function api(path) {
    const url = JSON.parse(readFileSync(join(home, "listen.json"), "utf8")).url,
      res = await fetch(url + path, { signal: AbortSignal.timeout(5000) });
    if (!res.ok) throw Error("HTTP " + res.status + " " + (await res.text()));
    return res.json();
  }
  await until(async () => {
    const n = await api("/v1/network");
    return (
      n.chains.length === 2 && n.chains.every((c) => Number(c.height) >= 3)
    );
  }, "anonymous native sync");
  const before = await api("/v1/network");
  for (const c of before.chains) {
    const p = await api(
        `/v1/chains/${c.chain_id}/proofs/state?height=1&table=accounts&key=${Buffer.from(canonical("absent")).toString("base64url")}`,
      ),
      g = JSON.parse(genesis.get(c.chain_id));
    verifyPublicState(
      {
        chain_id: c.chain_id,
        keys: g.validators.map((v) =>
          Buffer.from(v.pub_key.value, "base64").toString("hex"),
        ),
      },
      p,
    );
    assert.equal(p.path.exists, false);
  }
  assert.match(
    run(node, ["verify", "--home", home, "--from", "genesis"]),
    /"replayed":true/,
  );
  await stop(observer);
  observer = launch();
  await until(async () => {
    const n = await api("/v1/network");
    return (
      n.chains.length === 2 &&
      n.chains.every(
        (c) =>
          Number(c.height) >=
          Number(before.chains.find((b) => b.chain_id === c.chain_id).height),
      )
    );
  }, "abrupt observer restart");
  const files = readdirSync(home, { recursive: true }).map(String);
  assert(
    !files.some(
      (f) => f.includes("priv_validator") || f.includes("account_key"),
    ),
  );
  const report = {
    version: "0.6-development",
    keys: "PUBLIC_TEST_ONLY",
    passed: true,
    checks: [
      "fourteen-native-validator-processes",
      "anonymous-main-and-lane-P2P-sync",
      "historical-absence-verified-independently-in-TypeScript",
      "genesis-replay",
      "abrupt-observer-restart-and-index-rebuild",
      "no-account-or-consensus-private-key",
    ],
    directory: dir,
  };
  writeFileSync(join(dir, "report.json"), JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report, null, 2));
} finally {
  await Promise.all([...processes].map(stop));
  await Promise.all(servers.map((s) => new Promise((y) => s.close(y))));
}
