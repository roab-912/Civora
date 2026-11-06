import { Buffer } from "./buffer.js";
import {
  canonical,
  parse,
  domain,
  sha,
  blob,
  uint,
  validate,
  decode,
  chainID,
  signDigest,
} from "./protocol.js";
import {
  verifyPublicState,
  type PublicProof,
  type Frozen,
} from "../citizen-audit.js";
import { verifyHeader, verifyInclusion, type Trust } from "../consensus.js";
import { validateRegistry, authorize } from "../registry.js";
import type { Registry } from "../protocol.js";
import type { LocalAccount, PublicAccount } from "./vault.js";
import { read, write } from "./storage.js";
export type Row = Record<string, any>;
export interface NetworkChoice {
  name: string;
  manifest: Row;
  api_roots: string[];
  development_clock?: boolean;
}
export interface VerifiedContext {
  network_id: string;
  poll_id: string;
  genesis: string;
  genesis_sha256: string;
  poll?: PublicProof;
  proposal: PublicProof;
  configuration: PublicProof;
  frozen?: PublicProof;
}
export function trustAt(genesis: Row, height: string): Trust {
  let keys = genesis.validators.map((v: Row) => {
    if (v.power !== "1" || v.pub_key.type !== "tendermint/PubKeyEd25519")
      throw Error("ERR_VALIDATORS");
    return Buffer.from(v.pub_key.value, "base64").toString("hex");
  });
  if (keys.length !== 7 || new Set(keys).size !== 7)
    throw Error("ERR_VALIDATORS");
  for (const rotation of genesis.app_state.consensus_rotations ?? [])
    if (uint(height) >= uint(rotation.height) + 2n) keys = rotation.keys;
  return { chain_id: genesis.chain_id, keys };
}
export function checkProof(
  genesis: Row,
  p: PublicProof,
  table: string,
  key: unknown,
): unknown {
  verifyPublicState(trustAt(genesis, String(uint(p.height) + 1n)), p);
  if (
    p.path.entry.table !== table ||
    canonical(p.path.entry.key) !== canonical(key)
  )
    throw Error("ERR_PROOF_CONTEXT");
  return p.path.exists ? p.path.entry.value : null;
}
export function inspectContext(value: VerifiedContext): {
  poll: Row;
  proposal: Row;
  config: Row;
  frozen?: Frozen;
} {
  if (
    [value.poll, value.proposal, value.frozen].some(
      (p) => p && p.height !== value.configuration.height,
    )
  )
    throw Error("ERR_PROOF_HEIGHT");
  const g = JSON.parse(value.genesis);
  if (
    sha(Buffer.from(value.genesis)).toString("hex") !== value.genesis_sha256 ||
    g.chain_id !== chainID(value.network_id)
  )
    throw Error("ERR_NETWORK");
  const configuration = checkProof(
    g,
    value.configuration,
    "network",
    "config",
  ) as Row;
  let frozen: Frozen | undefined, poll: Row;
  if (value.frozen) {
    frozen = checkProof(
      g,
      value.frozen,
      "transport_polls",
      value.poll_id,
    ) as Frozen;
    if (
      !frozen ||
      frozen.context.network_id !== value.network_id ||
      frozen.context.poll_id !== value.poll_id
    )
      throw Error("ERR_POLL_CONTEXT");
    const now = uint(configuration.time_ms);
    poll = {
      proposal_id: frozen.proposal_id,
      protocol_epoch: frozen.context.protocol_epoch,
      eligibility_suite:
        "semaphore-v4.14.5-depth-" + frozen.context.circuit_depth,
      ballot_suite: "eg-2.0-civic-1",
      tally_suite: "eg-2.0-civic-1",
      ceremony_bundle: frozen.ceremony_bundle,
      group_root: frozen.context.group_root,
      eligible_count: frozen.eligible_count,
      opens_at_ms: frozen.opens_at_ms,
      closes_at_ms: frozen.closes_at_ms,
      state:
        now >= uint(frozen.opens_at_ms) && now < uint(frozen.closes_at_ms)
          ? "open"
          : "closed",
    };
  } else {
    if (!value.poll) throw Error("ERR_POLL_CONTEXT");
    poll = checkProof(g, value.poll, "polls", value.poll_id) as Row;
  }
  if (!poll || configuration.config.network_id !== value.network_id)
    throw Error("ERR_NETWORK");
  const config = configuration.config,
    registry = config.registry as Registry;
  validateRegistry(registry);
  if (!["1", "2"].includes(registry.protocol_epoch)) throw Error("ERR_VERSION");
  if (poll.protocol_epoch !== registry.protocol_epoch) throw Error("ERR_EPOCH");
  const proposal = checkProof(
    g,
    value.proposal,
    "proposals",
    poll.proposal_id,
  ) as Row;
  if (!proposal || proposal.poll_id !== value.poll_id || !poll.ceremony_bundle)
    throw Error("ERR_POLL_CONTEXT");
  for (const [op, id] of [
    ["eligibility.prove", poll.eligibility_suite],
    ["ballot.prove", poll.ballot_suite],
    ["tally.prove", poll.tally_suite],
  ])
    authorize(registry, poll.protocol_epoch, op!, id!);
  if (
    poll.ballot_suite !== "eg-2.0-civic-1" ||
    !["16", "26"].includes(config.eligibility.circuit_depth) ||
    poll.eligibility_suite !==
      "semaphore-v4.14.5-depth-" + config.eligibility.circuit_depth
  )
    throw Error("ERR_SUITE");
  if (frozen) {
    const f = frozen;
    if (
      !f ||
      f.context.network_id !== value.network_id ||
      f.context.poll_id !== value.poll_id ||
      f.context.protocol_epoch !== poll.protocol_epoch ||
      f.context.group_root !== poll.group_root ||
      f.ceremony_bundle !== poll.ceremony_bundle ||
      f.proposal_id !== poll.proposal_id
    )
      throw Error("ERR_POLL_CONTEXT");
    return { poll, proposal, config, frozen: f };
  }
  if (config.voting_profile !== "inline_dev") throw Error("ERR_PROFILE");
  return { poll, proposal, config };
}
export function verifyTransaction(
  genesis: Row,
  proof: Row,
  expectedID: string,
): Uint8Array {
  const h = verifyHeader(trustAt(genesis, proof.height), proof.signed_header);
  if (h.height !== proof.height) throw Error("ERR_PROOF_HEIGHT");
  const p = JSON.parse(blob(proof.transaction_proof).toString()),
    raw = Buffer.from(p.data, "base64");
  if (
    p.root_hash.toLowerCase() !== h.data_hash.toLowerCase() ||
    Buffer.from(p.proof.leaf_hash, "base64").toString("hex") !==
      sha(Buffer.from([0]), sha(raw)).toString("hex")
  )
    throw Error("ERR_PROOF");
  verifyInclusion(
    h.data_hash.toLowerCase(),
    sha(raw),
    uint(p.proof.index),
    uint(p.proof.total),
    p.proof.aunts.map((v: string) => Buffer.from(v, "base64").toString("hex")),
  );
  const tx = parse(raw) as Row,
    id = domain(tx.body.v === "1" ? "tx" : "tx/2", tx.body);
  if (id !== expectedID || proof.tx_id !== id) throw Error("ERR_PROOF_CONTEXT");
  return raw;
}
export class Client {
  choice: NetworkChoice;
  genesis: Row | null = null;
  genesisRaw = "";
  configuration: PublicProof | null = null;
  height = "0";
  time = "0";
  offline = false;
  stale = false;
  lastRead = 0;
  versionOK = false;
  lastError = "";
  constructor(choice: NetworkChoice) {
    validate("citizen_manifest", choice.manifest);
    if (choice.manifest.main.chain_id !== chainID(choice.manifest.network_id))
      throw Error("ERR_NETWORK");
    this.choice = choice;
  }
  get network() {
    return this.choice.manifest.network_id as string;
  }
  async get(
    path: string,
    cache = true,
    accept?: (value: any) => void,
  ): Promise<any> {
    const key = this.network + "/" + path;
    let failure: unknown;
    for (const root of this.choice.api_roots) {
      try {
        if (!navigator.onLine) throw Error("ERR_OFFLINE");
        const res = await fetch(root + path, {
          credentials: "omit",
          cache: "no-store",
          redirect: "error",
          signal: AbortSignal.timeout(12000),
        });
        if (!res.ok) {
          if (res.status === 410) throw Error("ERR_CURSOR_EXPIRED");
          throw Error("ERR_HTTP_" + res.status);
        }
        const raw = await res.text();
        if (raw.length > 8 * 1024 * 1024) throw Error("ERR_CAPACITY");
        const data = path.endsWith("/genesis") ? JSON.parse(raw) : parse(raw);
        accept?.(data);
        if (cache) await write("public", key, { raw, time: Date.now() });
        return data;
      } catch (e) {
        failure = e;
      }
    }
    if (cache) {
      const item = await read<{ raw: string; time: number }>("public", key);
      if (item) {
        this.offline = true;
        const value = path.endsWith("/genesis")
          ? JSON.parse(item.raw)
          : parse(item.raw);
        accept?.(value);
        return value;
      }
    }
    throw failure ?? Error("ERR_UNAVAILABLE");
  }
  async initialize(): Promise<void> {
    const key = this.network + "/genesis-bytes";
    let raw = "";
    for (const root of this.choice.api_roots) {
      try {
        if (!navigator.onLine) throw Error("ERR_OFFLINE");
        const res = await fetch(root + "/v1/genesis", {
          credentials: "omit",
          cache: "no-store",
          redirect: "error",
        });
        if (!res.ok) throw Error("ERR_UNAVAILABLE");
        const candidate = await res.text();
        if (
          sha(Buffer.from(candidate)).toString("hex") !==
            this.choice.manifest.main.genesis.sha256 ||
          String(Buffer.byteLength(candidate)) !==
            this.choice.manifest.main.genesis.size
        )
          throw Error("ERR_GENESIS");
        raw = candidate;
        await write("public", key, { raw });
        break;
      } catch {}
    }
    if (!raw) {
      const cached = await read<{ raw: string }>("public", key);
      if (!cached) throw Error("ERR_GENESIS");
      raw = cached.raw;
      this.offline = true;
    }
    if (
      sha(Buffer.from(raw)).toString("hex") !==
      this.choice.manifest.main.genesis.sha256
    )
      throw Error("ERR_GENESIS");
    this.genesisRaw = raw;
    this.genesis = JSON.parse(raw);
    const app = this.genesis!.app_state,
      cfg = app.kind === "coordinator" ? app.data.ledger : app.data;
    if (
      cfg.network_id !== this.network ||
      cfg.voting_profile !== this.choice.manifest.voting_profile ||
      this.genesis!.chain_id !== this.choice.manifest.main.chain_id ||
      !["inline_dev", "transport_dev_v0_6"].includes(cfg.voting_profile)
    )
      throw Error("ERR_PROFILE");
    await this.refresh();
  }
  async refresh() {
    this.versionOK = false;
    this.offline = false;
    const status = await this.get("/v1/network");
    const usedCache = this.offline;
    this.stale = !this.choice.development_clock && status.stale === true;
    if (
      status.network_id !== this.network ||
      status.profile !== this.choice.manifest.voting_profile
    )
      throw Error("ERR_NETWORK");
    const high = await read<string>("settings", "height/" + this.network);
    if (high && uint(status.height) < uint(high)) throw Error("ERR_ROLLBACK");
    if (uint(status.height) < 2n) throw Error("ERR_PROOF_PENDING");
    this.height = String(uint(status.height) - 1n);
    this.configuration = await this.proof("network", "config", this.height);
    this.offline = this.offline || usedCache;
    const network = this.configuration.path.entry.value as Row;
    if (!network || network.config.network_id !== this.network)
      throw Error("ERR_NETWORK");
    validateRegistry(network.config.registry);
    const signing = network.config.registry.modules.find(
      (m: Row) =>
        m.status === "active" && m.operations.includes("account.sign"),
    );
    if (!signing || signing.parameters.format !== "ed25519")
      throw Error("ERR_SUITE");
    if (
      !["1", "2"].includes(network.config.registry.protocol_epoch) ||
      network.config.registry.state_schema !== "1"
    )
      throw Error("ERR_VERSION");
    this.time = network.time_ms;
    if (
      !this.choice.development_clock &&
      (Date.now() - Number(uint(this.time)) > 60000 ||
        Number(uint(this.time)) - Date.now() > 30000)
    )
      this.stale = true;
    this.versionOK = true;
    this.lastRead = this.offline
      ? ((await read<number>("settings", "last-verified/" + this.network)) ?? 0)
      : Date.now();
    if (!this.offline)
      await write("settings", "last-verified/" + this.network, this.lastRead);
    if (!this.offline)
      await write("settings", "height/" + this.network, status.height);
  }
  async proof(
    table: string,
    key: unknown,
    height = this.height,
  ): Promise<PublicProof> {
    if (!this.genesis) throw Error("ERR_GENESIS");
    const p = await this.get(
      "/v1/proofs/state?table=" +
        encodeURIComponent(table) +
        "&key=" +
        Buffer.from(canonical(key)).toString("base64url") +
        "&height=" +
        height,
      true,
      (value) => {
        checkProof(this.genesis!, value, table, key);
        if (value.height !== height) throw Error("ERR_PROOF_HEIGHT");
      },
    );
    checkProof(this.genesis, p, table, key);
    if (p.height !== height) throw Error("ERR_PROOF_HEIGHT");
    return p as PublicProof;
  }
  async value(table: string, key: unknown): Promise<any> {
    const p = await this.proof(table, key);
    return p.path.exists ? p.path.entry.value : null;
  }
  get config(): Row {
    if (!this.configuration) throw Error("ERR_VERSION");
    return (this.configuration.path.entry.value as Row).config;
  }
  canWrite() {
    return (
      navigator.onLine &&
      !this.offline &&
      !this.stale &&
      this.versionOK &&
      Date.now() - this.lastRead < 60000
    );
  }
  async post(path: string, raw: string): Promise<Row> {
    if (!this.canWrite()) throw Error("ERR_OFFLINE_OR_STALE");
    for (const root of this.choice.api_roots) {
      try {
        const res = await fetch(root + path, {
          method: "POST",
          body: raw,
          headers: { "Content-Type": "application/json" },
          credentials: "omit",
          cache: "no-store",
          redirect: "error",
          signal: AbortSignal.timeout(15000),
        });
        const value = parse(await res.text()) as Row;
        if (!res.ok) throw Error(value.code ?? "ERR_SUBMIT");
        return value;
      } catch (e) {
        this.lastError = e instanceof Error ? e.message : "ERR_SUBMIT";
      }
    }
    throw Error(this.lastError);
  }
  async publicAction(
    account: LocalAccount,
    type: string,
    payload: unknown,
    recoverySeed?: string,
  ): Promise<string> {
    await this.refresh();
    if (account.network_id !== this.network) throw Error("ERR_NETWORK");
    const current = (await this.value(
      "accounts",
      account.account_id,
    )) as PublicAccount | null;
    if (
      type !== "account.register" &&
      (!current || current.status !== "active")
    )
      throw Error("ERR_ACCOUNT");
    const registry = this.config.registry as Registry,
      body: Row = {
        v: registry.transaction_version,
        network_id: this.network,
        type,
        actor_id: account.account_id,
        nonce: current?.next_nonce ?? "0",
        payload,
      };
    if (body.v === "2") body.protocol_epoch = registry.protocol_epoch;
    const id = domain(body.v === "1" ? "tx" : "tx/2", body),
      tx = {
        body,
        auth: {
          scheme: "ed25519",
          signature: signDigest(id, recoverySeed ?? account.signing_seed),
        },
      };
    validate("transaction", tx);
    await this.post("/v1/transactions", canonical(tx));
    return id;
  }
  async confirmation(id: string): Promise<Row> {
    const status = await this.get("/v1/transactions/" + id, false);
    if (status.status === "finalized") {
      const proof = await this.get("/v1/proofs/tx/" + id, false);
      verifyTransaction(this.genesis!, proof, id);
      return { ...status, proof, verified: true };
    }
    return status;
  }
  async context(pollID: string): Promise<VerifiedContext> {
    await this.refresh();
    if (this.config.voting_profile === "transport_dev_v0_6") {
      const frozen = await this.proof("transport_polls", pollID),
        f = frozen.path.entry.value as Frozen;
      if (!f) throw Error("ERR_POLL");
      const result: VerifiedContext = {
        network_id: this.network,
        poll_id: pollID,
        genesis: this.genesisRaw,
        genesis_sha256: this.choice.manifest.main.genesis.sha256,
        proposal: await this.proof("proposals", f.proposal_id),
        configuration: this.configuration!,
        frozen,
      };
      inspectContext(result);
      return result;
    }
    const poll = await this.proof("polls", pollID),
      p = poll.path.entry.value as Row;
    if (!p) throw Error("ERR_POLL");
    const result: VerifiedContext = {
      network_id: this.network,
      poll_id: pollID,
      genesis: this.genesisRaw,
      genesis_sha256: this.choice.manifest.main.genesis.sha256,
      poll,
      proposal: await this.proof("proposals", p.proposal_id),
      configuration: this.configuration!,
    };
    inspectContext(result);
    return result;
  }
}
