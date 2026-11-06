import { spawn, spawnSync } from "node:child_process";
import {
  readFileSync,
  writeFileSync,
  mkdirSync,
  existsSync,
  createReadStream,
} from "node:fs";
import { join, resolve, extname } from "node:path";
import { createServer } from "node:http";
import { randomUUID } from "node:crypto";
import { canonical, domain, signDigest } from "../dist/web/src/protocol.js";
import { actors, ceremony, tally } from "./pwa-guardians.TEST_ONLY.mjs";
import { NotificationService } from "../dist/services/notifications/service.js";
import {
  validatePushKeys,
  endpointURL,
} from "../dist/services/notifications/transport.js";
const args = process.argv.slice(2),
  get = (flag, fallback) => {
    const i = args.indexOf(flag);
    return i < 0 ? fallback : args[i + 1];
  },
  port = Number(get("--port", "8790"));
const root = resolve("."),
  dir = resolve(".tools/pwa/sessions/" + randomUUID());
mkdirSync(dir, { recursive: true });
const env = {
    ...process.env,
    GOPATH: resolve(".tools/gopath"),
    GOMODCACHE: resolve(".tools/gomodcache"),
    GOCACHE: resolve(".tools/gocache"),
  },
  go = existsSync(".tools/go/bin/go.exe")
    ? resolve(".tools/go/bin/go.exe")
    : "go",
  children = [];
process.on("exit", () => {
  for (const p of children) if (p.exitCode === null) p.kill("SIGKILL");
});
const binaries = {};
for (const name of ["civpwadev", "civnode"]) {
  const path = join(dir, name + ".exe"),
    r = spawnSync(
      go,
      ["build", "-buildvcs=false", "-o", path, "./cmd/" + name],
      { env, windowsHide: true, encoding: "utf8" },
    );
  if (r.status !== 0) throw Error(r.stderr);
  binaries[name] = path;
}
async function launch(exe, argv, pattern, name) {
  const p = spawn(exe, argv, {
    env,
    windowsHide: true,
    stdio: ["ignore", "pipe", "pipe"],
  });
  children.push(p);
  let log = "";
  return new Promise((yes, no) => {
    const timer = setTimeout(
      () => no(Error(name + " startup timeout: " + log)),
      60000,
    );
    const append = (b) => {
      log += b.toString();
      writeFileSync(join(dir, name + ".log"), log);
      const m = pattern.exec(log);
      if (m) {
        clearTimeout(timer);
        yes(m[1]);
      }
    };
    p.stdout.on("data", append);
    p.stderr.on("data", append);
    p.on("error", no);
    p.on("exit", (code) => {
      clearTimeout(timer);
      no(Error(name + " exited " + code + ": " + log));
    });
  });
}
const live = await launch(
  binaries.civpwadev,
  ["--home", join(dir, "ledger"), "--listen", "127.0.0.1:0"],
  /PUBLIC_TEST_ONLY_PWA_API (http:\/\/127\.0\.0\.1:\d+)/,
  "ledger",
);
const archive = await launch(
  binaries.civnode,
  [
    "serve",
    "--home",
    join(dir, "historical"),
    "--fixture",
    "spec/fixtures/v0.6/archive.json",
    "--listen",
    "127.0.0.1:0",
  ],
  /Citizen archive API: (127\.0\.0\.1:\d+)/,
  "archive",
).then((v) => "http://" + v);
async function api(path, body) {
  const r = await fetch(live + path, {
    method: body ? "POST" : "GET",
    ...(body ? { body: canonical(body) } : {}),
  });
  const raw = await r.text();
  if (!r.ok) throw Error("atelier " + r.status + ": " + raw);
  return JSON.parse(raw);
}
const manifest = await api("/v1/network/manifest"),
  network = manifest.network_id,
  people = actors(network),
  ceremonies = new Map();
async function state() {
  const v = await api("/__dev/state");
  const values = (table) =>
    v.entries
      .filter((e) => e.table === table)
      .map((e) => ({ id: e.key, ...e.value }));
  return {
    ...v,
    proposals: values("proposals"),
    polls: values("polls"),
    accounts: values("accounts"),
  };
}
async function commit(type, payload, person) {
  let body;
  if (person) {
    const s = await state(),
      a = s.accounts.find((a) => a.id === person.account_id);
    body = {
      v: "1",
      network_id: network,
      type,
      actor_id: person.account_id,
      nonce: a.next_nonce,
      payload,
    };
  } else
    body = {
      v: "1",
      network_id: network,
      type,
      actor_id: null,
      nonce: null,
      payload,
    };
  const auth = person
    ? {
        scheme: "ed25519",
        signature: signDigest(domain("tx", body), person.signing.seed),
      }
    : { scheme: "proof" };
  return api("/v1/transactions", { body, auth });
}
async function settle() {
  let s = await state();
  for (const p of s.proposals) {
    if (p.state !== "collecting") continue;
    for (const person of people.slice(0, 2)) {
      if (
        person.account_id !== p.author &&
        !p.endorsements.includes(person.account_id)
      )
        await commit("proposal.endorse", { proposal_id: p.id }, person);
    }
  }
  s = await state();
  for (const poll of s.polls) {
    if (poll.state !== "preparing") continue;
    const proposal = s.proposals.find((p) => p.id === poll.proposal_id),
      c = await ceremony(network, poll.id, proposal.id, proposal.guardians);
    await commit("poll.configure", {
      poll_id: poll.id,
      ceremony_bundle: c.encoded,
      guardian_acks: c.guardian_acks,
    });
    ceremonies.set(poll.id, c);
  }
}
async function advance(id) {
  let s = await state(),
    p = s.proposals.find((p) => p.id === id || p.poll_id === id);
  if (!p) throw Error("unknown proposal");
  let poll = s.polls.find((poll) => poll.id === p.poll_id),
    at;
  if (p.state === "scheduled") at = poll.opens_at_ms;
  else if (p.state === "open") at = poll.closes_at_ms;
  else if (p.state === "adopted") at = p.effective_at_ms;
  else throw Error("phase cannot advance: " + p.state);
  await api("/__dev/advance", { at_ms: at });
  s = await state();
  poll = s.polls.find((v) => v.id === p.poll_id);
  if (poll?.state === "tallying" && poll.count !== "0") {
    const c = ceremonies.get(poll.id);
    if (!c) throw Error("guardian session missing");
    const t = await tally(c, poll.ceremony_bundle, poll.aggregate, poll.count);
    await commit("poll.tally", {
      poll_id: poll.id,
      aggregate_digest: poll.aggregate_digest,
      ...t,
    });
  }
  return { advanced: true };
}
const historical = JSON.parse(
    readFileSync("spec/fixtures/v0.6/network.json", "utf8"),
  ),
  choices = [
    {
      name: "Atelier civique",
      manifest,
      api_roots: ["/api/atelier"],
      development_clock: true,
    },
    {
      name: "Archive certifiée v0.6",
      manifest: historical,
      api_roots: ["/api/archive"],
      development_clock: false,
    },
  ];
const notificationMessages = [],
  testNotifications = args.includes("--notifications-test-only"),
  notifications = new NotificationService({
    directory: join(dir, "notifications"),
    contact: get(
      "--push-contact",
      process.env.CIVORA_PUSH_CONTACT ??
        "mailto:civora-development@example.invalid",
    ),
    watches: choices.map((c, i) => ({
      ...c,
      api_roots: [i === 0 ? live : archive],
    })),
    ...(testNotifications
      ? {
          sender: {
            async send(_subscription, message) {
              notificationMessages.push(message);
              return { status: 201 };
            },
          },
          async validateEndpoint(keys, endpoint) {
            await validatePushKeys(keys);
            if (endpointURL(endpoint).hostname !== "push.example.test")
              throw Error("ERR_TEST_ENDPOINT");
          },
        }
      : {}),
  });
let serial = Promise.resolve();
function exclusive(fn) {
  const next = serial.then(fn);
  serial = next.catch(() => {});
  return next;
}
const mime = {
    ".html": "text/html; charset=utf-8",
    ".js": "text/javascript; charset=utf-8",
    ".css": "text/css; charset=utf-8",
    ".svg": "image/svg+xml",
    ".png": "image/png",
    ".json": "application/json",
    ".webmanifest": "application/manifest+json",
  },
  assets = resolve(".tools/pwa/v0.8/public");
const server = createServer(async (req, res) => {
  res.setHeader("X-Content-Type-Options", "nosniff");
  res.setHeader("Referrer-Policy", "no-referrer");
  res.setHeader("Cache-Control", "no-store");
  res.setHeader(
    "Permissions-Policy",
    "camera=(), microphone=(), geolocation=()",
  );
  res.setHeader(
    "Content-Security-Policy",
    "default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self'; img-src 'self'; connect-src 'self'; worker-src 'self' blob:; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'",
  );
  try {
    const u = new URL(req.url, "http://127.0.0.1"),
      path = u.pathname;
    if (
      req.headers.origin &&
      req.headers.origin !== `http://${req.headers.host}` &&
      req.method !== "GET"
    ) {
      res.writeHead(403);
      res.end();
      return;
    }
    if (await notifications.handle(req, res, path)) return;
    if (
      testNotifications &&
      path === "/__dev/notifications" &&
      req.method === "GET"
    ) {
      res.setHeader("Content-Type", "application/json");
      res.end(canonical({ test_only: true, messages: notificationMessages }));
      return;
    }
    if (path === "/pwa-config.json") {
      res.setHeader("Content-Type", "application/json");
      res.end(
        JSON.stringify({
          version: "0.8.0",
          notifications: { api_root: "/notifications" },
          networks: choices,
          development: true,
        }),
      );
      return;
    }
    if (path === "/__dev/advance" && req.method === "POST") {
      let data = "";
      for await (const c of req) {
        data += c;
        if (data.length > 4096) throw Error("capacity");
      }
      const result = await exclusive(() => advance(JSON.parse(data).id));
      res.setHeader("Content-Type", "application/json");
      res.end(canonical(result));
      return;
    }
    if (path.startsWith("/api/")) {
      const m = /^\/api\/(atelier|archive)(\/v1\/.*)$/.exec(path);
      if (!m || !["GET", "POST", "HEAD"].includes(req.method)) {
        res.writeHead(404);
        res.end();
        return;
      }
      const body = [];
      for await (const c of req) {
        body.push(c);
        if (body.reduce((s, b) => s + b.length, 0) > 262144)
          throw Error("capacity");
      }
      const base = m[1] === "atelier" ? live : archive,
        raw = Buffer.concat(body),
        forward = async () => {
          const response = await fetch(base + m[2] + u.search, {
              method: req.method,
              headers: { "Content-Type": "application/json" },
              ...(req.method === "POST" ? { body: raw } : {}),
            }),
            bytes = Buffer.from(await response.arrayBuffer());
          if (response.ok && req.method === "POST" && m[1] === "atelier") {
            let tx;
            try {
              tx = JSON.parse(raw);
            } catch {}
            if (tx?.body?.type === "proposal.submit") await settle();
          }
          res.writeHead(response.status, {
            "Content-Type":
              response.headers.get("Content-Type") ?? "application/json",
          });
          res.end(bytes);
        };
      await (req.method === "POST" ? exclusive(forward) : forward());
      return;
    }
    const crypto =
      /^\/crypto\/semaphore\/(16|26)\/(semaphore\.(?:wasm|zkey)|verification-key\.json)$/.exec(
        path,
      );
    if (crypto) {
      const p =
        crypto[2] === "verification-key.json"
          ? `spec/crypto/semaphore-${crypto[1]}.verification-key.json`
          : `.tools/semaphore/${crypto[1]}/${crypto[2]}`;
      res.setHeader(
        "Content-Type",
        crypto[2].endsWith("wasm")
          ? "application/wasm"
          : "application/octet-stream",
      );
      createReadStream(p)
        .on("error", () => {
          res.statusCode = 404;
          res.end();
        })
        .pipe(res);
      return;
    }
    const name = path === "/" ? "index.html" : path.slice(1);
    if (!/^[a-zA-Z0-9._-]+$/.test(name) || !existsSync(join(assets, name))) {
      res.writeHead(404);
      res.end();
      return;
    }
    res.setHeader(
      "Content-Type",
      mime[extname(name)] ?? "application/octet-stream",
    );
    res.end(readFileSync(join(assets, name)));
  } catch (e) {
    res.writeHead(503, { "Content-Type": "application/json" });
    res.end(
      canonical({
        code: "ERR_DEVELOPMENT",
        message: e instanceof Error ? e.message : "Unavailable",
        retryable: true,
      }),
    );
  }
});
await new Promise((y) => server.listen(port, "127.0.0.1", y));
console.log(`PUBLIC_TEST_ONLY_PWA http://127.0.0.1:${server.address().port}`);
console.log(
  "Synthetic linked commits, real Go transitions and cryptographic proofs. Session: " +
    dir,
);
let closing = false;
async function close() {
  if (closing) return;
  closing = true;
  server.closeAllConnections();
  server.close();
  await notifications.close();
  await Promise.all(
    children.map(
      (p) =>
        new Promise((y) => {
          if (p.exitCode !== null) {
            y();
            return;
          }
          p.once("exit", y);
          p.kill("SIGKILL");
        }),
    ),
  );
  process.exit(0);
}
process.on("SIGINT", close);
process.on("SIGTERM", close);
process.on("message", (message) => {
  if (message?.type === "shutdown") void close();
});
if (process.send) process.on("disconnect", () => void close());
