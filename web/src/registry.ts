import {
  canonical,
  domain,
  uint,
  type Armed,
  type Plan,
  type Registry,
} from "./protocol.js";
export function validateRegistry(r: Registry): void {
  if (
    uint(r.protocol_epoch) === 0n ||
    r.state_schema !== "1" ||
    !["1", "2"].includes(r.transaction_version) ||
    (r.transaction_version === "1" && r.protocol_epoch !== "1")
  )
    throw Error("invalid epoch/version");
  const ids = new Set<string>(),
    active = new Set<string>();
  for (const m of r.modules) {
    if (ids.has(m.id)) throw Error("duplicate module");
    ids.add(m.id);
    if (m.status === "active")
      for (const op of m.operations) {
        if (active.has(op)) throw Error("ambiguous active module");
        active.add(op);
      }
  }
  for (const m of r.modules) {
    const deps = new Set<string>();
    for (const d of m.dependencies) {
      if (!ids.has(d) || d === m.id || deps.has(d))
        throw Error("invalid dependency");
      deps.add(d);
    }
  }
  const hash = r.modules.find(
    (m) => m.status === "active" && m.operations.includes("state.hash"),
  );
  if (!hash) throw Error("ERR_SUITE: no active state hash");
  if (hash.parameters.format !== "sha256")
    throw Error("ERR_UNSUPPORTED_HASH_SUITE");
}
export function authorize(
  r: Registry,
  epoch: string,
  op: string,
  id: string,
  historical = false,
): void {
  if (epoch !== r.protocol_epoch) throw Error("ERR_EPOCH");
  if (
    !r.modules.some(
      (m) =>
        m.id === id &&
        m.operations.includes(op) &&
        (m.status === "active" ||
          (historical && m.status === "legacy_verify_only")),
    )
  )
    throw Error("ERR_SUITE");
}
export function nextRegistry(r: Registry, p: Plan): Registry {
  if (
    p.from_epoch !== r.protocol_epoch ||
    uint(p.to_epoch) !== uint(p.from_epoch) + 1n ||
    p.from_state_schema !== r.state_schema
  )
    throw Error("ERR_EPOCH");
  const next: Registry = {
    protocol_epoch: p.to_epoch,
    state_schema: p.to_state_schema,
    transaction_version: p.transaction_version,
    modules: p.modules,
  };
  validateRegistry(next);
  for (const old of r.modules) {
    const m = next.modules.find((m) => m.id === old.id);
    if (!m) throw Error("module history removed");
    if (canonical({ ...old, status: "" }) !== canonical({ ...m, status: "" }))
      throw Error("immutable module changed");
    if (
      (old.status === "revoked" && m.status !== "revoked") ||
      (old.status === "legacy_verify_only" &&
        ["active", "prepared"].includes(m.status)) ||
      (old.status === "active" && m.status === "prepared")
    )
      throw Error("ERR_REVOKED_ROLLBACK");
  }
  return next;
}
export function checkActivation(
  a: Armed,
  network: string,
  chain: string,
): void {
  if (
    a.authorization !== "development_fixture" ||
    a.plan.network_id !== network ||
    a.plan_id !== domain("upgrade/plan", a.plan) ||
    a.instance.plan_id !== a.plan_id ||
    a.instance_id !== domain("upgrade/instance", a.instance)
  )
    throw Error("invalid fixture activation identity");
  if (
    a.plan.chains.length !== 1 ||
    a.plan.chains[0] !== chain ||
    a.instance.heights.length !== 1 ||
    a.instance.heights[0]!.chain_id !== chain ||
    uint(a.instance.heights[0]!.height) === 0n
  )
    throw Error("invalid activation chain/height");
  const [lo, hi, notice, start, end, expiry] = [
    a.plan.activation.not_before_ms,
    a.plan.activation.expires_at_ms,
    a.plan.activation.notice_ms,
    a.instance.arm_window_start_ms,
    a.instance.arm_window_end_ms,
    a.instance.expires_at_ms,
  ].map(uint) as [bigint, bigint, bigint, bigint, bigint, bigint];
  if (
    lo >= hi ||
    start > end ||
    end >= expiry ||
    expiry > hi ||
    start < lo ||
    notice > end - start
  )
    throw Error("invalid activation window");
  for (const art of a.plan.artifacts) uint(art.size);
}
