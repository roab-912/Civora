// Independent Node reference verifier for the pinned CometBFT 0.38 encoding.
// Trust is supplied by authenticated genesis/frozen context, never by a proof.
import { ed25519 } from "@noble/curves-comet/ed25519.js";
import {
  blob,
  canonical,
  domain,
  sha,
  uint,
  validate,
  type Entry,
} from "./protocol.js";
export interface Trust {
  chain_id: string;
  keys: string[];
}
export interface Membership {
  entry: Entry;
  siblings: string[];
}
export interface StateProof {
  height: string;
  signed_header: string;
  path: Membership;
}
interface Part {
  total: number;
  hash: string;
}
interface BlockID {
  hash: string;
  parts: Part;
}
export interface Header {
  version: { block: string; app: string };
  chain_id: string;
  height: string;
  time: string;
  last_block_id: BlockID;
  last_commit_hash: string;
  data_hash: string;
  validators_hash: string;
  next_validators_hash: string;
  consensus_hash: string;
  app_hash: string;
  last_results_hash: string;
  evidence_hash: string;
  proposer_address: string;
}
interface Signature {
  block_id_flag: number;
  validator_address: string;
  timestamp: string;
  signature: string | null;
}
interface SignedHeader {
  header: Header;
  commit: {
    height: string;
    round: number;
    block_id: BlockID;
    signatures: Signature[];
  };
}
const join = (...b: Uint8Array[]) => Buffer.concat(b),
  empty = () => Buffer.alloc(0);
function hex(s: string, size?: number): Buffer {
  if (typeof s !== "string" || !/^([0-9a-fA-F]{2})*$/.test(s))
    throw Error("hex encoding");
  const b = Buffer.from(s, "hex");
  if (size !== undefined && b.length !== size) throw Error("hash length");
  return b;
}
function varint(n: bigint): Buffer {
  if (n < 0n) n = BigInt.asUintN(64, n);
  const b = [];
  do {
    let x = Number(n & 127n);
    n >>= 7n;
    if (n) x |= 128;
    b.push(x);
  } while (n);
  return Buffer.from(b);
}
const integer = (field: number, n: bigint) =>
  n === 0n ? empty() : join(varint(BigInt(field * 8)), varint(n));
const message = (field: number, b: Uint8Array) =>
  join(varint(BigInt(field * 8 + 2)), varint(BigInt(b.length)), b);
const bytes = (field: number, b: Uint8Array) =>
  b.length === 0 ? empty() : message(field, b);
const fixed = (field: number, n: bigint) => {
  if (n === 0n) return empty();
  const b = Buffer.alloc(8);
  b.writeBigInt64LE(n);
  return join(varint(BigInt(field * 8 + 1)), b);
};
function timestamp(s: string): { wire: Buffer; ms: bigint } {
  if (typeof s !== "string") throw Error("timestamp");
  const m = /^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?Z$/.exec(s);
  if (!m) throw Error("timestamp encoding");
  const ms = Date.parse(m[1] + "Z");
  if (
    !Number.isSafeInteger(ms) ||
    new Date(ms).toISOString().slice(0, 19) !== m[1]
  )
    throw Error("timestamp date");
  const seconds = BigInt(ms) / 1000n,
    nanos = BigInt((m[2] ?? "").padEnd(9, "0"));
  return {
    wire: join(integer(1, seconds), integer(2, nanos)),
    ms: seconds * 1000n + nanos / 1000000n,
  };
}
function blockID(b: BlockID): Buffer {
  const h = hex(b.hash),
    p = hex(b.parts.hash);
  if (
    h.length === 0
      ? p.length !== 0 || b.parts.total !== 0
      : h.length !== 32 || p.length !== 32 || b.parts.total === 0
  )
    throw Error("block ID shape");
  if (
    !Number.isInteger(b.parts.total) ||
    b.parts.total < 0 ||
    b.parts.total > 0xffffffff
  )
    throw Error("part count");
  return join(
    bytes(1, hex(b.hash)),
    message(
      2,
      join(integer(1, BigInt(b.parts.total)), bytes(2, hex(b.parts.hash))),
    ),
  );
}
export function logRoot(items: Uint8Array[]): string {
  const walk = (xs: Uint8Array[]): Buffer => {
    if (!xs.length) return sha(empty());
    if (xs.length === 1) return sha(Buffer.from([0]), xs[0]!);
    let k = 1;
    while (k * 2 < xs.length) k *= 2;
    return sha(Buffer.from([1]), walk(xs.slice(0, k)), walk(xs.slice(k)));
  };
  return walk(items).toString("hex");
}
export function verifyInclusion(
  root: string,
  item: Uint8Array,
  index: bigint,
  size: bigint,
  path: string[],
): void {
  if (size <= 0n || index < 0n || index >= size || path.length > 64)
    throw Error("inclusion bounds");
  let fn = index,
    sn = size - 1n,
    r = sha(Buffer.from([0]), item);
  for (const s of path) {
    if (sn === 0n) throw Error("excess path");
    const p = hex(s, 32);
    if (fn % 2n === 1n || fn === sn) {
      r = sha(Buffer.from([1]), p, r);
      while (fn !== 0n && fn % 2n === 0n) {
        fn >>= 1n;
        sn >>= 1n;
      }
    } else r = sha(Buffer.from([1]), r, p);
    fn >>= 1n;
    sn >>= 1n;
  }
  if (sn !== 0n || r.toString("hex") !== root) throw Error("inclusion root");
}
export function verifyConsistency(
  oldRoot: string,
  newRoot: string,
  m: bigint,
  n: bigint,
  path: string[],
): void {
  if (m < 0n || m > n || path.length > 64) throw Error("consistency bounds");
  if (m === 0n) {
    if (
      oldRoot !== logRoot([]) ||
      path.length ||
      (n === 0n && newRoot !== logRoot([]))
    )
      throw Error("empty log proof");
    return;
  }
  if (m === n) {
    if (oldRoot !== newRoot || path.length) throw Error("same-size log fork");
    return;
  }
  const ps = path.map((s) => hex(s, 32));
  let fn = m - 1n,
    sn = n - 1n;
  while (fn % 2n === 1n) {
    fn >>= 1n;
    sn >>= 1n;
  }
  let fr: Buffer, sr: Buffer;
  if (fn === 0n) {
    fr = hex(oldRoot, 32);
    sr = fr;
  } else {
    if (!ps.length) throw Error("short consistency");
    fr = ps.shift()!;
    sr = fr;
  }
  for (const p of ps) {
    if (sn === 0n) throw Error("excess consistency");
    if (fn % 2n === 1n || fn === sn) {
      fr = sha(Buffer.from([1]), p, fr);
      sr = sha(Buffer.from([1]), p, sr);
      while (fn !== 0n && fn % 2n === 0n) {
        fn >>= 1n;
        sn >>= 1n;
      }
    } else sr = sha(Buffer.from([1]), sr, p);
    fn >>= 1n;
    sn >>= 1n;
  }
  if (
    sn !== 0n ||
    fr.toString("hex") !== oldRoot ||
    sr.toString("hex") !== newRoot
  )
    throw Error("consistency root");
}
export function verifyMembership(root: string, p: Membership): void {
  validate("state_membership", p);
  const key = hex(domain("state/key", [p.entry.table, p.entry.key]), 32);
  let h = sha(
    Buffer.from([0]),
    key,
    sha(Buffer.from(canonical(p.entry.value))),
  );
  for (let d = 255; d >= 0; d--) {
    const s = hex(p.siblings[d]!, 32);
    h =
      key[d >> 3]! & (1 << (7 - (d % 8)))
        ? sha(Buffer.from([1]), s, h)
        : sha(Buffer.from([1]), h, s);
  }
  if (h.toString("hex") !== root.toLowerCase())
    throw Error("state membership root");
}
export function verifyHeader(trust: Trust, encoded: string): Header {
  validate("consensus_trust", trust);
  const raw = blob(encoded);
  if (raw.length > 48 * 1024) throw Error("signed header bound");
  const sh = JSON.parse(raw.toString("utf8")) as SignedHeader,
    h = sh.header,
    c = sh.commit;
  const height = uint(h.height);
  if (
    h.chain_id !== trust.chain_id ||
    height === 0n ||
    height > 0x7fffffffffffffffn ||
    c.height !== h.height ||
    !Number.isInteger(c.round) ||
    c.round < 0 ||
    c.round > 0x7fffffff ||
    c.signatures.length !== 7
  )
    throw Error("commit context");
  const validators = trust.keys
    .map((key) => ({
      key,
      address: sha(hex(key, 32)).subarray(0, 20).toString("hex"),
    }))
    .sort((a, b) => a.address.localeCompare(b.address));
  const valHash = logRoot(
    validators.map((v) =>
      join(message(1, message(1, hex(v.key, 32))), integer(2, 1n)),
    ),
  );
  if (valHash !== h.validators_hash.toLowerCase())
    throw Error("frozen validator set");
  if (uint(h.version.block) !== 11n) throw Error("unsupported block protocol");
  const fields = [
    join(integer(1, uint(h.version.block)), integer(2, uint(h.version.app))),
    bytes(1, Buffer.from(h.chain_id)),
    integer(1, height),
    timestamp(h.time).wire,
    blockID(h.last_block_id),
    ...[
      "last_commit_hash",
      "data_hash",
      "validators_hash",
      "next_validators_hash",
      "consensus_hash",
      "app_hash",
      "last_results_hash",
      "evidence_hash",
      "proposer_address",
    ].map((k) => bytes(1, hex(h[k as keyof Header] as string))),
  ];
  for (const key of [
    "validators_hash",
    "next_validators_hash",
    "consensus_hash",
    "app_hash",
  ] as const)
    hex(h[key], 32);
  for (const key of [
    "last_commit_hash",
    "data_hash",
    "last_results_hash",
    "evidence_hash",
  ] as const) {
    const b = hex(h[key]);
    if (b.length !== 0 && b.length !== 32) throw Error("header hash size");
  }
  hex(h.proposer_address, 20);
  const headerHash = logRoot(fields);
  if (
    headerHash !== c.block_id.hash.toLowerCase() ||
    hex(c.block_id.hash, 32).length !== 32 ||
    hex(c.block_id.parts.hash, 32).length !== 32 ||
    c.block_id.parts.total === 0
  )
    throw Error("header/commit hash");
  let count = 0;
  for (let i = 0; i < 7; i++) {
    const s = c.signatures[i]!,
      v = validators[i]!;
    if (s.block_id_flag === 1) {
      if (s.validator_address !== "" || s.signature !== null)
        throw Error("absent signature");
      continue;
    }
    if (s.block_id_flag !== 2 && s.block_id_flag !== 3)
      throw Error("commit flag");
    if (s.validator_address.toLowerCase() !== v.address || s.signature === null)
      throw Error("validator order/address");
    const body = join(
        integer(1, 2n),
        fixed(2, height),
        fixed(3, BigInt(c.round)),
        s.block_id_flag === 2 ? message(4, blockID(c.block_id)) : empty(),
        message(5, timestamp(s.timestamp).wire),
        bytes(6, Buffer.from(trust.chain_id)),
      ),
      signed = join(varint(BigInt(body.length)), body),
      sig = Buffer.from(s.signature, "base64");
    if (sig.length !== 64 || sig.toString("base64") !== s.signature)
      throw Error("signature length/encoding");
    if (!ed25519.verify(sig, signed, hex(v.key, 32), { zip215: true }))
      throw Error("commit signature");
    if (s.block_id_flag === 2) count++;
  }
  if (count < 5) throw Error("commit quorum 5/7");
  return h;
}
export function verifyState(trust: Trust, p: StateProof): Header {
  validate("consensus_state_proof", p);
  const h = verifyHeader(trust, p.signed_header);
  if (uint(p.height) === 0n || uint(h.height) !== uint(p.height) + 1n)
    throw Error("state AppHash requires H+1");
  verifyMembership(h.app_hash, p.path);
  return h;
}
