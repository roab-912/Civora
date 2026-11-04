// Browser-safe path checking and Semaphore message binding. No account id or
// private identity is included in the membership proof returned to a caller.
import { poseidon2 } from "poseidon-lite/poseidon2";
import { canonical } from "./codec.js";
export const scalarModulus =
  21888242871839275222246405745257275088548364400416034343698204186575808495617n;
export interface Snapshot {
  network_id: string;
  protocol_epoch: string;
  tree_id: string;
  suite_id: string;
  height: string;
  circuit_depth: "16" | "26";
  size: string;
  active_count: string;
  root: string | null;
  snapshot_id: string;
}
export interface EligibilityPath {
  snapshot: Snapshot;
  leaf_index: string;
  leaf: string;
  index: string;
  siblings: string[];
}
export interface MembershipContext {
  network_id: string;
  poll_id: string;
  protocol_epoch: string;
  suite_id: string;
  circuit_depth: "16" | "26";
  group_root: string;
}
export interface Membership {
  merkleTreeDepth: "16" | "26";
  merkleTreeRoot: string;
  nullifier: string;
  message: string;
  scope: string;
  points: string[];
}
export function decimal(s: unknown, max = scalarModulus - 1n): bigint {
  if (typeof s !== "string" || !/^(0|[1-9][0-9]*)$/.test(s) || s.length > 78)
    throw Error("ERR_INTEGER");
  const n = BigInt(s);
  if (n > max) throw Error("ERR_INTEGER");
  return n;
}
const utf8 = new TextEncoder();
export async function sha256(bytes: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new Uint8Array(bytes).buffer,
  );
  return Array.from(new Uint8Array(digest), (b) =>
    b.toString(16).padStart(2, "0"),
  ).join("");
}
export async function domain(tag: string, value: unknown): Promise<string> {
  return sha256(utf8.encode("CIVIC/1/" + tag + "\n" + canonical(value)));
}
export async function binding(
  network: string,
  poll: string,
  ballot: Uint8Array,
): Promise<{ scope: string; message: string }> {
  if (!/^[0-9a-f]{64}$/.test(network) || !/^[0-9a-f]{64}$/.test(poll))
    throw Error("ERR_DOMAIN");
  const scope = await domain("vote/scope", [network, poll]),
    message = await domain("vote/message", [
      network,
      poll,
      await sha256(ballot),
    ]);
  return {
    scope: BigInt("0x" + scope.slice(0, 62)).toString(),
    message: BigInt("0x" + message.slice(0, 62)).toString(),
  };
}
function exact(
  value: unknown,
  keys: string[],
): asserts value is Record<string, unknown> {
  if (
    typeof value !== "object" ||
    value === null ||
    Array.isArray(value) ||
    Object.keys(value).sort().join(",") !== keys.sort().join(",")
  )
    throw Error("ERR_SCHEMA");
}
export async function validateSnapshot(value: Snapshot): Promise<void> {
  exact(value, [
    "network_id",
    "protocol_epoch",
    "tree_id",
    "suite_id",
    "height",
    "circuit_depth",
    "size",
    "active_count",
    "root",
    "snapshot_id",
  ]);
  if (
    !/^[0-9a-f]{64}$/.test(value.network_id) ||
    !["16", "26"].includes(value.circuit_depth) ||
    value.suite_id !== "semaphore-v4.14.5-depth-" + value.circuit_depth ||
    typeof value.tree_id !== "string" ||
    value.tree_id.length < 1 ||
    value.tree_id.length > 96
  )
    throw Error("ERR_SNAPSHOT");
  decimal(value.height, 0xffffffffffffffffn);
  if (decimal(value.protocol_epoch, 0xffffffffffffffffn) === 0n)
    throw Error("ERR_EPOCH");
  const size = decimal(value.size, 1n << BigInt(value.circuit_depth)),
    active = decimal(value.active_count, size);
  if (active > size || (value.root === null) !== (size === 0n))
    throw Error("ERR_SNAPSHOT");
  if (value.root !== null) decimal(value.root);
  const id = await domain("eligibility/snapshot", [
    value.network_id,
    value.protocol_epoch,
    value.tree_id,
    value.suite_id,
    value.height,
    value.circuit_depth,
    value.size,
    value.active_count,
    value.root,
  ]);
  if (value.snapshot_id !== id) throw Error("ERR_SNAPSHOT_ID");
}
// expected must come from an authenticated ledger context. An API response
// that merely hashes itself does not authenticate its root.
export async function verifyPath(
  path: EligibilityPath,
  expected: Snapshot,
  commitment: string,
): Promise<void> {
  exact(path, ["snapshot", "leaf_index", "leaf", "index", "siblings"]);
  await validateSnapshot(expected);
  await validateSnapshot(path.snapshot);
  if (canonical(path.snapshot) !== canonical(expected))
    throw Error("ERR_SNAPSHOT_CONTEXT");
  if (path.leaf !== commitment || decimal(path.leaf) === 0n)
    throw Error("ERR_IDENTITY_PATH");
  const size = decimal(expected.size),
    physical = decimal(path.leaf_index, size),
    packed = decimal(path.index, 0xffffffffffffffffn);
  if (
    physical >= size ||
    !Array.isArray(path.siblings) ||
    path.siblings.length > Number(expected.circuit_depth)
  )
    throw Error("ERR_PATH");
  let n = BigInt(path.leaf),
    idx = physical,
    width = size,
    expectedIndex = 0n,
    used = 0;
  while (width > 1n) {
    const sibling = idx ^ 1n;
    if (sibling < width) {
      if (used >= path.siblings.length) throw Error("ERR_PATH");
      const s = decimal(path.siblings[used]);
      expectedIndex |= (idx & 1n) << BigInt(used);
      n = idx & 1n ? poseidon2([s, n]) : poseidon2([n, s]);
      used++;
    }
    idx >>= 1n;
    width = (width + 1n) >> 1n;
  }
  if (
    used !== path.siblings.length ||
    expectedIndex !== packed ||
    n.toString() !== expected.root
  )
    throw Error("ERR_PATH");
}
export function checkContext(c: MembershipContext, snapshot: Snapshot): void {
  exact(c, [
    "network_id",
    "poll_id",
    "protocol_epoch",
    "suite_id",
    "circuit_depth",
    "group_root",
  ]);
  if (
    c.network_id !== snapshot.network_id ||
    c.protocol_epoch !== snapshot.protocol_epoch ||
    c.suite_id !== snapshot.suite_id ||
    c.circuit_depth !== snapshot.circuit_depth ||
    c.group_root !== snapshot.root ||
    !/^[0-9a-f]{64}$/.test(c.poll_id)
  )
    throw Error("ERR_PROOF_CONTEXT");
}
