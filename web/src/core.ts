// Independent TypeScript transition oracle for Go fixture/replay comparison.
import {
  canonical,
  chainID,
  decode,
  domain,
  txID,
  uint,
  verifyDigest,
  type Account,
  type Block,
  type Config,
  type Event,
  type Payload,
  type State,
  type Transaction,
} from "./protocol.js";
import { checkActivation, nextRegistry, validateRegistry } from "./registry.js";
import { stateRoot } from "./tree.js";
import {
  sha3,
  verifyOnChainRecovery,
  type RecoveryBundle,
} from "../../services/qualification/recovery.js";
export class Engine {
  config: Config;
  accounts = new Map<string, Account>();
  height = 0n;
  time = 0n;
  security?: { last_counter: string; orders: RecoveryBundle[] };
  upgrades = new Map<
    string,
    {
      plan: Config["activations"][number]["plan"];
      instance: Config["activations"][number]["instance"];
      status: "armed" | "activated";
    }
  >();
  constructor(
    raw: string,
    readonly supportedEpoch = 1n,
  ) {
    this.config = decode<Config>("config", raw);
    // This oracle covers v0.1. Governance fixtures are replayed by the Go
    // machine and their schema, thresholds and Merkle roots checked in TS.
    if ("governance" in this.config)
      throw Error("ERR_GOVERNANCE_ORACLE_UNAVAILABLE");
    if ("eligibility" in this.config)
      throw Error("ERR_ELIGIBILITY_ORACLE_UNAVAILABLE");
    if (chainID(this.config.network_id) !== this.config.chain_id)
      throw Error("invalid chain_id");
    validateRegistry(this.config.registry);
    if (this.config.recovery) {
      const r = this.config.recovery;
      if (
        r.root.network_id !== this.config.network_id ||
        r.root.mode !== "development_TEST_ONLY" ||
        r.protected_profile !== this.config.voting_profile ||
        sha3(Buffer.from(canonical(r.root))) !== r.root_sha3_384
      )
        throw Error("ERR_RECOVERY_GENESIS");
      const root = r.root;
      if (
        uint(root.recovery_epoch) === 0n ||
        uint(root.not_before_ms) >= uint(root.expires_at_ms)
      )
        throw Error("ERR_RECOVERY_ROOT_VALIDITY");
      const members = [...root.members, ...root.mirrors];
      if (
        new Set(members.map((m) => m.member_id)).size !== 19 ||
        new Set(members.map((m) => m.public_key)).size !== 19 ||
        root.members.filter((m) => m.college === "technical").length !== 7 ||
        root.members.filter((m) => m.college === "civic").length !== 7
      )
        throw Error("ERR_RECOVERY_KEY_SEPARATION");
      for (const m of members) {
        const b = Buffer.from(m.public_key, "base64url");
        if (b.length !== 1952 || b.toString("base64url") !== m.public_key)
          throw Error("ERR_RECOVERY_ENCODING");
      }
      this.security = { last_counter: "0", orders: [] };
    }
    if (uint(this.config.registry.protocol_epoch) > supportedEpoch)
      throw Error("ERR_UNSUPPORTED_EPOCH");
    let r = this.config.registry,
      last = 0n;
    for (const a of this.config.activations) {
      checkActivation(a, this.config.network_id, this.config.chain_id);
      const h = uint(a.instance.heights[0]!.height);
      if (h <= last) throw Error("activation order");
      last = h;
      r = nextRegistry(r, a.plan);
      if (this.upgrades.has(a.plan_id)) throw Error("duplicate plan");
      this.upgrades.set(a.plan_id, {
        plan: a.plan,
        instance: a.instance,
        status: "armed",
      });
    }
  }
  snapshot(): State {
    const entries: State["entries"] = [
      {
        table: "network",
        key: "config",
        value: {
          config: this.config,
          height: String(this.height),
          time_ms: String(this.time),
        },
      },
    ];
    for (const [key, value] of this.accounts)
      entries.push({ table: "accounts", key, value });
    if (this.security) {
      entries.push({
        table: "security",
        key: "recovery",
        value: { last_counter: this.security.last_counter },
      });
      for (const b of this.security.orders)
        entries.push({
          table: "security_orders",
          key: b.order.counter,
          value: b,
        });
    }
    for (const [key, value] of this.upgrades)
      entries.push({ table: "upgrades", key, value });
    entries.sort((a, b) =>
      a.table < b.table
        ? -1
        : a.table > b.table
          ? 1
          : a.key < b.key
            ? -1
            : a.key > b.key
              ? 1
              : 0,
    );
    return { height: String(this.height), time_ms: String(this.time), entries };
  }
  root(): string {
    return stateRoot(this.snapshot().entries);
  }
  clone(): Engine {
    // Construct a clone without re-running genesis preconditions on an already
    // activated registry. All mutable data are copied before a candidate block.
    const n = Object.create(Engine.prototype) as Engine;
    Object.assign(n, {
      config: structuredClone(this.config),
      accounts: structuredClone(this.accounts),
      height: this.height,
      time: this.time,
      upgrades: structuredClone(this.upgrades),
      supportedEpoch: this.supportedEpoch,
      security: structuredClone(this.security),
    });
    return n;
  }
  apply(raw: string): { engine: Engine; events: Event[] } {
    if (Buffer.byteLength(raw) > 2 * 1024 * 1024) throw Error("ERR_SIZE");
    const b = decode<Block>("block", raw);
    const h = uint(b.height),
      tm = uint(b.time_ms);
    if (h !== this.height + 1n) throw Error("ERR_HEIGHT");
    const n = this.clone();
    n.height = h;
    if (tm > n.time) n.time = tm;
    const events: Event[] = [];
    const emit = (type: string, subject: string): void => {
      events.push({
        id: domain("event", [
          n.config.network_id,
          b.height,
          String(events.length),
        ]),
        type,
        subject,
      });
    };
    for (const a of n.config.activations)
      if (uint(a.instance.heights[0]!.height) === h) {
        if (uint(a.plan.to_epoch) > n.supportedEpoch)
          throw Error("ERR_UNSUPPORTED_EPOCH");
        if (
          n.time < uint(a.plan.activation.not_before_ms) ||
          n.time >= uint(a.instance.expires_at_ms) ||
          n.time <
            uint(a.instance.arm_window_end_ms) +
              uint(a.plan.activation.notice_ms)
        )
          throw Error("activation outside fixed window");
        n.config.registry = nextRegistry(n.config.registry, a.plan);
        n.upgrades.get(a.plan_id)!.status = "activated";
        emit("protocol.activate", a.plan_id);
      }
    if ((b.security_orders?.length ?? 0) > 4)
      throw Error("ERR_SECURITY_BUDGET");
    for (const bundle of b.security_orders ?? []) {
      const cfg = n.config.recovery;
      if (!cfg || !n.security) throw Error("ERR_RECOVERY_NOT_CONFIGURED");
      if (n.security.orders.length >= 10000)
        throw Error("ERR_RECOVERY_CAPACITY");
      const order = verifyOnChainRecovery(
        Buffer.from(canonical(cfg.root)),
        cfg.root_sha3_384,
        Buffer.from(canonical(bundle)),
        n.time,
        uint(n.security.last_counter),
      );
      if (order.type !== "security.hold")
        throw Error("ERR_RECOVERY_ACTION_UNAVAILABLE");
      if (order.scope_kind === "module") {
        const module = n.config.registry.modules.find(
          (m) => m.id === order.scope_id,
        );
        if (!module) throw Error("ERR_SECURITY_UNKNOWN_MODULE");
        if (
          module.operations.some((op) =>
            ["state.hash", "consensus.sign"].includes(op),
          )
        )
          throw Error("ERR_SECURITY_OFFCHAIN_REQUIRED");
      }
      n.security.last_counter = order.counter;
      n.security.orders.push(bundle);
      emit("security.hold", domain("recovery/order", order));
    }
    for (const tx of b.transactions) {
      n.applyTransaction(tx);
      emit(tx.body.type, tx.body.actor_id);
    }
    return { engine: n, events };
  }
  checkTx(raw: string): void {
    if (Buffer.byteLength(raw) > 256 * 1024) throw Error("ERR_SIZE");
    this.clone().applyTransaction(decode<Transaction>("transaction", raw));
  }
  private applyTransaction(tx: Transaction): void {
    if (Buffer.byteLength(canonical(tx)) > 256 * 1024) throw Error("ERR_SIZE");
    const b = tx.body,
      r = this.config.registry;
    if (b.network_id !== this.config.network_id) throw Error("ERR_NETWORK");
    if (
      b.v !== r.transaction_version ||
      (b.v === "1" && r.protocol_epoch !== "1") ||
      (b.v === "2" && b.protocol_epoch !== r.protocol_epoch)
    )
      throw Error("ERR_EPOCH");
    const suite = r.modules.find(
      (m) => m.status === "active" && m.operations.includes("account.sign"),
    );
    if (!suite || suite.parameters.format !== "ed25519")
      throw Error("ERR_SUITE");
    if (
      this.security?.orders.some(
        (b) => b.order.scope_kind === "module" && b.order.scope_id === suite.id,
      )
    )
      throw Error("ERR_SECURITY_MODULE_REVIEW");
    const nonce = uint(b.nonce);
    if (nonce === 0xffffffffffffffffn) throw Error("ERR_NONCE_OVERFLOW");
    const p = b.payload as Payload;
    if (b.type !== "account.retire") {
      if (
        BigInt(p.commitment) <= 0n ||
        BigInt(p.commitment) >=
          21888242871839275222246405745257275088548364400416034343698204186575808495617n
      )
        throw Error("ERR_COMMITMENT");
      if (p.signing_pk === p.recovery_pk) throw Error("ERR_KEY_SEPARATION");
    }
    let a = this.accounts.get(b.actor_id);
    let pk = a?.recovery_pk;
    if (b.type === "account.register") {
      if (
        a ||
        nonce !== 0n ||
        b.actor_id !== domain("account", [b.network_id, p.signing_pk])
      )
        throw Error("ERR_REGISTER");
      if (
        this.accounts.size >= 131072 ||
        [...this.accounts.values()].filter((a) => a.status === "active")
          .length >= 65536
      )
        throw Error("ERR_CAPACITY");
      pk = p.signing_pk;
    } else {
      if (!a || a.status !== "active") throw Error("ERR_ACCOUNT");
      if (b.nonce !== a.next_nonce) throw Error("ERR_NONCE");
    }
    if (b.type !== "account.retire")
      for (const [id, other] of this.accounts) {
        if (
          id !== b.actor_id &&
          other.used_keys.some((k) => k === p.signing_pk || k === p.recovery_pk)
        )
          throw Error("ERR_KEY_IN_USE");
        if (
          other.used_commitments.some(
            (c) =>
              c === p.commitment && (id !== b.actor_id || c !== a?.commitment),
          )
        )
          throw Error("ERR_COMMITMENT_USED");
      }
    if (!pk || !verifyDigest(txID(b), tx.auth.signature, pk))
      throw Error("ERR_SIGNATURE");
    if (b.type === "account.retire") a!.status = "retired";
    else {
      a ??= {
        signing_pk: p.signing_pk,
        recovery_pk: p.recovery_pk,
        commitment: p.commitment,
        next_nonce: "1",
        status: "active",
        registered_height: String(this.height),
        used_keys: [],
        used_commitments: [],
      };
      a.signing_pk = p.signing_pk;
      a.recovery_pk = p.recovery_pk;
      a.commitment = p.commitment;
      for (const k of [p.signing_pk, p.recovery_pk])
        if (!a.used_keys.includes(k)) a.used_keys.push(k);
      if (!a.used_commitments.includes(p.commitment))
        a.used_commitments.push(p.commitment);
    }
    a!.next_nonce = String(nonce + 1n);
    if (a!.used_keys.length > 262144 || a!.used_commitments.length > 262144)
      throw Error("ERR_CAPACITY");
    this.accounts.set(b.actor_id, a!);
  }
}
