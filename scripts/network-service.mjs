import { randomBytes, createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import {
  readFileSync,
  writeFileSync,
  mkdirSync,
  existsSync,
  createReadStream,
  renameSync,
} from "node:fs";
import { resolve, join } from "node:path";
import { isIP } from "node:net";
import { Ajv2020 } from "ajv/dist/2020.js";
import presenceSchema from "../spec/network/presence.schema.json" with { type: "json" };
import observerSchema from "../spec/network/observer.schema.json" with { type: "json" };
import {
  canonical,
  parse,
  domain,
  verifyDigest,
} from "../dist/web/src/protocol.js";
import { buildParticipantBinaries } from "./build-participant.mjs";

const sha = (raw) => createHash("sha256").update(raw).digest("hex");
const validatePresence = new Ajv2020({ strict: true }).compile(presenceSchema);
const validateObserver = new Ajv2020({ strict: true }).compile(observerSchema);
export function localOrigin(value) {
  const url = new URL(value);
  if (
    !["http:", "https:"].includes(url.protocol) ||
    url.username ||
    url.password ||
    url.pathname !== "/" ||
    url.search ||
    url.hash
  )
    throw Error("ERR_JOIN_ORIGIN");
  const host = url.hostname;
  if (
    host !== "localhost" &&
    !(
      isIP(host) === 4 &&
      /^(127\.|10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/.test(host)
    )
  )
    throw Error("ERR_JOIN_LOCAL_ONLY");
  return url.origin;
}

export class PresenceRegistry {
  constructor(
    network,
    {
      now = Date.now,
      random = () => randomBytes(32).toString("hex"),
      capacity = 4096,
    } = {},
  ) {
    this.network = network;
    this.now = now;
    this.random = random;
    this.capacity = capacity;
    this.challenges = new Map();
    this.nodes = new Map();
  }
  prune() {
    const now = this.now();
    for (const [node, value] of this.challenges)
      if (value.until <= now) this.challenges.delete(node);
    for (const [node, value] of this.nodes)
      if (value.until <= now) this.nodes.delete(node);
  }
  challenge(node) {
    this.prune();
    if (!/^[0-9a-f]{64}$/.test(node)) throw Error("ERR_NODE_ID");
    if (!this.challenges.has(node) && this.challenges.size >= this.capacity)
      throw Error("ERR_PRESENCE_CAPACITY");
    const value = this.random();
    this.challenges.set(node, { value, until: this.now() + 30000 });
    return { network_id: this.network, challenge: value };
  }
  async announce(envelope, verifyBlock) {
    this.prune();
    if (!validatePresence(envelope)) throw Error("ERR_PRESENCE_FORMAT");
    const body = envelope.body,
      expected = this.challenges.get(body.node_id);
    if (
      body.network_id !== this.network ||
      !expected ||
      body.challenge !== expected.value
    )
      throw Error("ERR_PRESENCE_CHALLENGE");
    if (
      !verifyDigest(
        domain("development/participant-presence", body),
        envelope.signature,
        body.node_id,
      )
    )
      throw Error("ERR_PRESENCE_SIGNATURE");
    this.challenges.delete(body.node_id);
    if (BigInt(body.height) > 18446744073709551615n)
      throw Error("ERR_PRESENCE_HEIGHT");
    if (body.action === "offline") {
      this.nodes.delete(body.node_id);
      return;
    }
    if (!this.nodes.has(body.node_id) && this.nodes.size >= this.capacity)
      throw Error("ERR_PRESENCE_CAPACITY");
    await verifyBlock(body);
    const previous = this.nodes.get(body.node_id);
    if (previous && BigInt(body.height) < BigInt(previous.height))
      throw Error("ERR_PRESENCE_ROLLBACK");
    // One identity has at most one live lease, including concurrent refreshes.
    this.nodes.set(body.node_id, {
      height: body.height,
      until: this.now() + 45000,
    });
  }
  count() {
    this.prune();
    return this.nodes.size;
  }
}

export class NetworkService {
  constructor({
    choice,
    api,
    directory,
    caFile = ".tools/pwa/lan-tls/ca.pem",
  }) {
    this.choice = choice;
    this.api = api.replace(/\/$/, "");
    this.directory = resolve(directory);
    this.caFile = caFile;
    this.presence = new PresenceRegistry(choice.manifest.network_id);
    this.build = null;
    this.packages = new Map();
    mkdirSync(this.directory, { recursive: true });
  }
  async get(path) {
    const response = await fetch(this.api + path, {
      redirect: "error",
      signal: AbortSignal.timeout(15000),
    });
    if (!response.ok) throw Error("ERR_NETWORK_SOURCE");
    const raw = await response.text();
    if (Buffer.byteLength(raw) > 8 * 1024 * 1024) throw Error("ERR_CAPACITY");
    return raw;
  }
  async overview() {
    const status = parse(await this.get("/v1/network"));
    if (status.network_id !== this.choice.manifest.network_id)
      throw Error("ERR_NETWORK");
    let citizen = 0,
      test = 0,
      cursor = "",
      height = "";
    const seen = new Set();
    do {
      const page = parse(
        await this.get(
          "/v1/accounts?limit=100&status=active" +
            (cursor ? "&cursor=" + encodeURIComponent(cursor) : ""),
        ),
      );
      if (height && page.height !== height) throw Error("ERR_INDEX_CHANGED");
      height = page.height;
      for (const account of page.items) {
        if (seen.has(account.id)) throw Error("ERR_ACCOUNT_INDEX");
        seen.add(account.id);
        // The five seeded guardians are identified from the authenticated
        // genesis policy, never counted as human participants.
        if (this.guardians?.has(account.id)) test++;
        else citizen++;
      }
      cursor = page.next_cursor;
      if (seen.size > 100000) throw Error("ERR_CAPACITY");
    } while (cursor);
    const observers = this.presence.count();
    return {
      format: "civora-network-overview-1",
      network_id: status.network_id,
      source: "local_index_and_declared_presence",
      observer_nodes: String(observers),
      coordinator_nodes: "1",
      active_nodes: String(observers + 1),
      citizen_accounts: String(citizen),
      test_accounts: String(test),
      height: status.height,
      observed_at: String(Date.now()),
      presence_ttl_seconds: "45",
      public_qualified: false,
    };
  }
  async genesis() {
    if (!this.genesisRaw) {
      const raw = await this.get("/v1/genesis");
      const ref = this.choice.manifest.main.genesis;
      if (
        sha(raw) !== ref.sha256 ||
        String(Buffer.byteLength(raw)) !== ref.size
      )
        throw Error("ERR_GENESIS");
      const genesis = JSON.parse(raw);
      const config = genesis.app_state.data;
      if (
        genesis.chain_id !== this.choice.manifest.main.chain_id ||
        config.network_id !== this.choice.manifest.network_id ||
        config.voting_profile !== "inline_dev"
      )
        throw Error("ERR_PROFILE");
      this.guardians = new Set(
        config.governance.guardians.map((g) => g.account_id),
      );
      this.genesisRaw = raw;
    }
    return this.genesisRaw;
  }
  async bundle(target, origin) {
    if (!["windows-amd64", "linux-amd64"].includes(target))
      throw Error("ERR_JOIN_TARGET");
    origin = localOrigin(origin);
    const key = origin + "/" + target;
    if (this.packages.has(key)) return this.packages.get(key);
    // A host cannot create an unbounded on-disk mirror from arbitrary Host values.
    if (this.packages.size >= 12) throw Error("ERR_JOIN_CAPACITY");
    const raw = await this.genesis();
    this.build ??= buildParticipantBinaries();
    const native =
      this.build.targets[
        process.platform === "win32" ? "windows-amd64" : "linux-amd64"
      ];
    const configuration = {
      format: "civora-observer-1",
      network_id: this.choice.manifest.network_id,
      genesis: Buffer.from(raw).toString("base64url"),
      genesis_sha256: sha(raw),
      api_url: origin + this.choice.api_roots[0],
      presence_url: origin + "/network/presence",
      ca_pem:
        origin.startsWith("https:") && existsSync(this.caFile)
          ? readFileSync(this.caFile, "utf8").replaceAll("\r\n", "\n")
          : "",
      public_test_only: true,
    };
    if (!validateObserver(configuration)) throw Error("ERR_JOIN_CONFIG");
    const name = "civora-reseau-" + target + ".zip";
    const folder = join(
      this.directory,
      sha(canonical({ configuration, build: this.build.source_sha256 })),
    );
    mkdirSync(folder, { recursive: true });
    const configFile = join(folder, "reseau.json"),
      file = join(folder, name);
    writeFileSync(configFile, canonical(configuration));
    if (!existsSync(file)) {
      const temporary =
        file + "." + randomBytes(8).toString("hex") + ".partial";
      const result = spawnSync(
        native.file,
        [
          "bundle",
          "--binary",
          this.build.targets[target].file,
          "--config",
          configFile,
          "--output",
          temporary,
          "--target",
          target,
        ],
        { encoding: "utf8", windowsHide: true },
      );
      if (result.error || result.status !== 0)
        throw Error(result.error?.message ?? result.stderr);
      renameSync(temporary, file);
    }
    const bytes = readFileSync(file),
      entry = { file, name, sha256: sha(bytes), size: String(bytes.length) };
    this.packages.set(key, entry);
    return entry;
  }
  async handle(req, res, url) {
    if (!url.pathname.startsWith("/network/")) return false;
    res.setHeader("Content-Type", "application/json");
    const send = (value) => res.end(canonical(value));
    try {
      if (url.pathname === "/network/overview" && req.method === "GET") {
        await this.genesis();
        send(await this.overview());
      } else if (
        url.pathname === "/network/challenge" &&
        req.method === "GET"
      ) {
        send(this.presence.challenge(url.searchParams.get("node") ?? ""));
      } else if (
        url.pathname === "/network/presence" &&
        req.method === "POST"
      ) {
        let raw = "";
        for await (const chunk of req) {
          raw += chunk;
          if (Buffer.byteLength(raw) > 4096) throw Error("ERR_CAPACITY");
        }
        await this.presence.announce(parse(raw), async (body) => {
          const block = await this.get("/v1/blocks/" + body.height);
          if (sha(block) !== body.block_sha256)
            throw Error("ERR_PRESENCE_BLOCK");
        });
        send({ accepted: true, expires_after_seconds: "45" });
      } else {
        const match =
          /^\/network\/download\/(windows-amd64|linux-amd64)\.(zip|sha256)$/.exec(
            url.pathname,
          );
        if (!match || req.method !== "GET") {
          res.statusCode = 404;
          send({ code: "ERR_NOT_FOUND" });
          return true;
        }
        const origin =
          req.headers["x-civora-origin"] ?? "http://" + req.headers.host;
        const entry = await this.bundle(match[1], origin);
        if (match[2] === "sha256")
          send({
            name: entry.name,
            sha256: entry.sha256,
            size: entry.size,
            network_id: this.choice.manifest.network_id,
          });
        else {
          res.setHeader("Content-Type", "application/zip");
          res.setHeader(
            "Content-Disposition",
            'attachment; filename="' + entry.name + '"',
          );
          res.setHeader("Content-Length", entry.size);
          res.setHeader("X-Content-SHA256", entry.sha256);
          createReadStream(entry.file)
            .on("error", () => res.destroy())
            .pipe(res);
        }
      }
    } catch (error) {
      res.statusCode =
        error.message.startsWith("ERR_PRESENCE") ||
        error.message.startsWith("ERR_NODE") ||
        error.message.startsWith("ERR_JOIN")
          ? 400
          : 503;
      send({
        code: error.message.startsWith("ERR_")
          ? error.message
          : "ERR_NETWORK_UNAVAILABLE",
      });
    }
    return true;
  }
}
