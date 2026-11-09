// Local frontend host for existing public nodes. No development guardians,
// synthetic quorum, private keys or ledger controls are loaded by this command.
import { createServer } from "node:http";
import { readFileSync, existsSync, createReadStream } from "node:fs";
import { resolve, dirname, join, extname } from "node:path";
import { NotificationService } from "../dist/services/notifications/service.js";
import { NetworkService } from "./network-service.mjs";
const args = process.argv.slice(2),
  get = (n, d = "") => {
    const i = args.indexOf(n);
    return i < 0 ? d : args[i + 1];
  },
  file = resolve(get("--config", "pwa-networks.json")),
  config = JSON.parse(readFileSync(file, "utf8")),
  routes = new Map();
const networks = config.networks.slice(0, 1).map((entry, i) => {
  const manifest =
      typeof entry.manifest === "string"
        ? JSON.parse(
            readFileSync(resolve(dirname(file), entry.manifest), "utf8"),
          )
        : entry.manifest,
    origins = entry.api_roots ?? manifest.main.origins.map((o) => o.api);
  return {
    name: entry.name ?? manifest.name,
    manifest,
    development_clock:
      config.development === true && entry.development_clock === true,
    api_roots: origins.map((origin, j) => {
      const url = new URL(origin);
      if (
        !["http:", "https:"].includes(url.protocol) ||
        url.username ||
        url.password ||
        url.search ||
        url.hash
      )
        throw Error("Invalid operator-configured API origin");
      const key = `n${i}s${j}`;
      routes.set(key, origin.replace(/\/$/, ""));
      return "/api/" + key;
    }),
  };
});
const networkService =
  config.development === true &&
  networks[0]?.manifest.voting_profile === "inline_dev"
    ? new NetworkService({
        choice: networks[0],
        api: routes.get("n0s0"),
        directory: resolve(
          config.participation_directory ?? ".tools/network/host",
        ),
      })
    : null;
const notifications = new NotificationService({
  directory: resolve(
    config.notifications?.directory ?? ".dev-host/pwa-notifications",
  ),
  watches: networks.map((n, i) => ({
    ...n,
    api_roots: n.api_roots.map((_p, j) => routes.get(`n${i}s${j}`)),
  })),
  ...(config.notifications?.contact
    ? { contact: config.notifications.contact }
    : {}),
});
const directory = resolve(get("--public-dir", ".tools/pwa/v0.8/public")),
  mime = {
    ".html": "text/html; charset=utf-8",
    ".js": "text/javascript; charset=utf-8",
    ".css": "text/css; charset=utf-8",
    ".svg": "image/svg+xml",
    ".png": "image/png",
    ".json": "application/json",
    ".webmanifest": "application/manifest+json",
  };
const developmentUpstream =
  config.development === true && config.development_upstream
    ? new URL(config.development_upstream)
    : null;
if (
  developmentUpstream &&
  (developmentUpstream.protocol !== "http:" ||
    developmentUpstream.hostname !== "127.0.0.1" ||
    developmentUpstream.username ||
    developmentUpstream.password)
)
  throw Error(
    "Development upstream must be the explicitly selected loopback workshop",
  );
const server = createServer(async (req, res) => {
  res.setHeader("Cache-Control", "no-store");
  res.setHeader("X-Content-Type-Options", "nosniff");
  res.setHeader("Referrer-Policy", "no-referrer");
  res.setHeader(
    "Permissions-Policy",
    "camera=(), microphone=(), geolocation=()",
  );
  res.setHeader(
    "Content-Security-Policy",
    "default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self'; img-src 'self'; connect-src 'self'; worker-src 'self' blob:; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'",
  );
  try {
    const url = new URL(req.url, "http://127.0.0.1");
    if (
      req.headers.origin &&
      req.headers.origin !== `http://${req.headers.host}` &&
      req.method !== "GET"
    ) {
      res.writeHead(403);
      res.end();
      return;
    }
    if (await notifications.handle(req, res, url.pathname)) return;
    if (networkService && (await networkService.handle(req, res, url))) return;
    if (
      developmentUpstream &&
      url.pathname === "/__dev/advance" &&
      req.method === "POST"
    ) {
      const chunks = [];
      let size = 0;
      for await (const c of req) {
        size += c.length;
        if (size > 4096) throw Error("capacity");
        chunks.push(c);
      }
      const response = await fetch(
        new URL("/__dev/advance", developmentUpstream),
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: Buffer.concat(chunks),
          redirect: "error",
          signal: AbortSignal.timeout(60000),
        },
      );
      res.statusCode = response.status;
      res.setHeader("Content-Type", "application/json");
      res.end(Buffer.from(await response.arrayBuffer()));
      return;
    }
    if (url.pathname === "/pwa-config.json") {
      res.setHeader("Content-Type", "application/json");
      res.end(
        JSON.stringify({
          version: "0.8.0",
          networks,
          development: config.development === true,
          participation: networkService !== null,
          notifications: { api_root: "/notifications" },
        }),
      );
      return;
    }
    const api = /^\/api\/(n\d+s\d+)(\/v1\/.*)$/.exec(url.pathname);
    if (api) {
      const base = routes.get(api[1]);
      if (
        !base ||
        !["GET", "HEAD", "POST"].includes(req.method) ||
        (req.method === "POST" &&
          !/^\/v1\/(transactions|lanes\/\d+\/ballots)$/.test(api[2]))
      ) {
        res.writeHead(405);
        res.end();
        return;
      }
      const chunks = [];
      let size = 0;
      for await (const c of req) {
        size += c.length;
        if (size > 262144) {
          res.writeHead(413);
          res.end();
          return;
        }
        chunks.push(c);
      }
      const response = await fetch(base + api[2] + url.search, {
        method: req.method,
        headers: {
          "Content-Type": "application/json",
          ...(req.headers.range ? { Range: req.headers.range } : {}),
          ...(req.headers["if-range"]
            ? { "If-Range": req.headers["if-range"] }
            : {}),
        },
        ...(req.method === "POST" ? { body: Buffer.concat(chunks) } : {}),
        redirect: "error",
        signal: AbortSignal.timeout(30000),
      });
      res.statusCode = response.status;
      for (const k of [
        "content-type",
        "content-range",
        "accept-ranges",
        "etag",
      ])
        if (response.headers.has(k)) res.setHeader(k, response.headers.get(k));
      let received = 0;
      for await (const chunk of response.body ?? []) {
        received += chunk.length;
        if (received > 33554432) throw Error("capacity");
        res.write(chunk);
      }
      res.end();
      return;
    }
    const artifact =
      /^\/crypto\/semaphore\/(16|26)\/(semaphore\.(?:wasm|zkey)|verification-key\.json)$/.exec(
        url.pathname,
      );
    if (artifact) {
      const path =
        artifact[2] === "verification-key.json"
          ? `spec/crypto/semaphore-${artifact[1]}.verification-key.json`
          : join(
              get("--artifact-dir", ".tools/semaphore"),
              artifact[1],
              artifact[2],
            );
      res.setHeader(
        "Content-Type",
        artifact[2].endsWith(".wasm")
          ? "application/wasm"
          : "application/octet-stream",
      );
      createReadStream(path)
        .on("error", () => {
          res.statusCode = 404;
          res.end();
        })
        .pipe(res);
      return;
    }
    const name = url.pathname === "/" ? "index.html" : url.pathname.slice(1);
    if (!/^[a-zA-Z0-9._-]+$/.test(name) || !existsSync(join(directory, name))) {
      res.writeHead(404);
      res.end();
      return;
    }
    res.setHeader(
      "Content-Type",
      mime[extname(name)] ?? "application/octet-stream",
    );
    res.end(readFileSync(join(directory, name)));
  } catch {
    if (!res.headersSent)
      res.writeHead(503, { "Content-Type": "application/json" });
    res.end(
      '{"code":"ERR_SOURCE_UNAVAILABLE","message":"Source unavailable","retryable":true}',
    );
  }
});
await new Promise((y) =>
  server.listen(Number(get("--port", "8792")), "127.0.0.1", y),
);
console.log("Civora PWA http://127.0.0.1:" + server.address().port);
