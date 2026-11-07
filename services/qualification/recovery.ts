// Independent OpenSSL verifier for the Go/CIRCL recovery fixtures. This is a
// Node operator tool, never a browser fallback or an authorization to write.
import { createHash, createPublicKey, verify } from "node:crypto";
import { canonical, decode, uint, validate } from "../../web/src/protocol.js";
type Member = { member_id: string; college?: string; public_key: string };
export interface RecoveryRoot {
  v: string;
  network_id: string;
  recovery_epoch: string;
  mode: string;
  not_before_ms: string;
  expires_at_ms: string;
  members: Member[];
  mirrors: Member[];
}
export interface RecoveryOrder {
  v: string;
  network_id: string;
  recovery_epoch: string;
  counter: string;
  type: string;
  scope_kind: string;
  scope_id: string;
  reference_sha3_384: string;
  reference_point: string;
  public_reason: string;
  starts_at_ms: string;
  expires_at_ms: string;
}
interface Statement {
  network_id: string;
  recovery_epoch: string;
  counter: string;
  root_sha3_384: string;
  order_sha3_384: string;
  observed_at_ms: string;
  expires_at_ms: string;
}
interface Signature {
  member_id: string;
  signature: string;
}
export interface RecoveryBundle {
  order: RecoveryOrder;
  signatures: Signature[];
  mirrors: { member_id: string; statement: Statement; signature: string }[];
}
export const sha3 = (raw: Uint8Array) =>
  createHash("sha3-384").update(raw).digest("hex");
export const recoveryDigest = (domain: string, value: unknown) =>
  createHash("sha3-384").update(domain).update(canonical(value)).digest();
function bytes(value: string, size: number) {
  const b = Buffer.from(value, "base64url");
  if (b.length !== size || b.toString("base64url") !== value)
    throw Error("ERR_RECOVERY_ENCODING");
  return b;
}
function signature(member: Member, signature: string, digest: Uint8Array) {
  const key = createPublicKey({
    key: Buffer.concat([
      Buffer.from("308207b2300b0609608648016503040312038207a100", "hex"),
      bytes(member.public_key, 1952),
    ]),
    format: "der",
    type: "spki",
  });
  if (
    key.asymmetricKeyType !== "ml-dsa-65" ||
    !verify(null, digest, key, bytes(signature, 3309))
  )
    throw Error("ERR_RECOVERY_SIGNATURE");
}
function verifyRecoveryMode(
  rootBytes: Uint8Array,
  rootPin: string,
  bundleBytes: Uint8Array,
  now: bigint,
  last: bigint,
  allowTest = false,
  onChain = false,
): RecoveryOrder {
  if (
    rootBytes.length > 256 * 1024 ||
    bundleBytes.length > 2 * 1024 * 1024 ||
    sha3(rootBytes) !== rootPin
  )
    throw Error("ERR_RECOVERY_ROOT_PIN");
  const root = decode<RecoveryRoot>("recovery_root", rootBytes),
    bundle = decode<RecoveryBundle>("recovery_bundle", bundleBytes),
    order = bundle.order;
  if (
    uint(root.recovery_epoch) === 0n ||
    now < uint(root.not_before_ms) ||
    now >= uint(root.expires_at_ms) ||
    (!allowTest && root.mode !== "operator")
  )
    throw Error("ERR_RECOVERY_ROOT_VALIDITY");
  const ids = new Set<string>(),
    keys = new Set<string>(),
    counts = { technical: 0, civic: 0 };
  for (const member of [...root.members, ...root.mirrors]) {
    if (ids.has(member.member_id) || keys.has(member.public_key))
      throw Error("ERR_RECOVERY_KEY_SEPARATION");
    ids.add(member.member_id);
    keys.add(member.public_key);
    bytes(member.public_key, 1952);
  }
  for (const member of root.members)
    counts[member.college as keyof typeof counts]++;
  if (counts.technical !== 7 || counts.civic !== 7)
    throw Error("ERR_RECOVERY_COLLEGES");
  if (
    order.network_id !== root.network_id ||
    order.recovery_epoch !== root.recovery_epoch
  )
    throw Error("ERR_RECOVERY_CONTEXT");
  if (uint(order.counter) <= last) throw Error("ERR_RECOVERY_REPLAY");
  const start = uint(order.starts_at_ms),
    end = uint(order.expires_at_ms);
  if (
    start > now ||
    now >= end ||
    end <= start ||
    end - start > 259200000n ||
    end > uint(root.expires_at_ms)
  )
    throw Error("ERR_RECOVERY_WINDOW");
  if (
    (order.type === "security.hold" && order.scope_kind === "network") ||
    (order.type === "security.arm_reserve" && order.scope_kind !== "module") ||
    (order.type === "recovery.publish" &&
      (order.scope_kind !== "network" ||
        !/^[0-9a-f]{64}$/.test(order.scope_id) ||
        order.scope_id === order.network_id))
  )
    throw Error("ERR_RECOVERY_SCOPE");
  const digest = recoveryDigest("CIVIC/RECOVERY/1/" + order.type + "\n", order),
    seen = new Set<string>();
  counts.technical = counts.civic = 0;
  for (const sig of bundle.signatures) {
    if (seen.has(sig.member_id)) throw Error("ERR_RECOVERY_DUPLICATE");
    seen.add(sig.member_id);
    const member = root.members.find((m) => m.member_id === sig.member_id);
    if (!member) throw Error("ERR_RECOVERY_SIGNER");
    signature(member, sig.signature, digest);
    counts[member.college as keyof typeof counts]++;
  }
  if (counts.technical < 5 || counts.civic < 5)
    throw Error("ERR_RECOVERY_QUORUM");
  if (onChain) {
    if (bundle.mirrors.length) throw Error("ERR_RECOVERY_ONCHAIN_MIRRORS");
    return order;
  }
  seen.clear();
  for (const mirror of bundle.mirrors) {
    if (seen.has(mirror.member_id)) throw Error("ERR_RECOVERY_DUPLICATE");
    seen.add(mirror.member_id);
    const s = mirror.statement,
      observed = uint(s.observed_at_ms),
      expires = uint(s.expires_at_ms);
    if (
      s.network_id !== root.network_id ||
      s.recovery_epoch !== root.recovery_epoch ||
      s.counter !== order.counter ||
      s.root_sha3_384 !== rootPin ||
      s.order_sha3_384 !== digest.toString("hex") ||
      observed > now ||
      now >= expires ||
      expires <= observed ||
      expires - observed > 600000n ||
      now - observed > 600000n
    )
      throw Error("ERR_RECOVERY_FRESHNESS");
    const member = root.mirrors.find((m) => m.member_id === mirror.member_id);
    if (!member) throw Error("ERR_RECOVERY_MIRROR");
    signature(
      member,
      mirror.signature,
      recoveryDigest("CIVIC/RECOVERY/MIRROR/1\n", s),
    );
  }
  if (seen.size < 3) throw Error("ERR_RECOVERY_MIRRORS");
  validate("recovery_order", order);
  return order;
}
export function verifyRecovery(
  root: Uint8Array,
  pin: string,
  bundle: Uint8Array,
  now: bigint,
  last: bigint,
  allowTest = false,
) {
  return verifyRecoveryMode(root, pin, bundle, now, last, allowTest, false);
}
export function verifyOnChainRecovery(
  root: Uint8Array,
  pin: string,
  bundle: Uint8Array,
  now: bigint,
  last: bigint,
) {
  return verifyRecoveryMode(root, pin, bundle, now, last, true, true);
}
