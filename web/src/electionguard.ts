// Public verification only; BigInt operands here are public ciphertexts/proofs.
import { parse, canonical } from "./codec.js";
import { decimal } from "./eligibility.js";
import {
  checkCeremonyLink,
  decodeBundle,
  type DecodedBundle,
} from "./electionguard-bundle.js";
import { electionguardParameters as pinned } from "./electionguard-parameters.js";
const utf8 = new TextEncoder();
type Obj = Record<string, unknown>;
export interface ElectionContext {
  network_id: string;
  poll_id: string;
  proposal_id: string;
  protocol_epoch: string;
  guardian_ids: string[];
}
export interface PublicElection {
  context: ElectionContext;
  key: bigint;
  he: Uint8Array;
  coefficients: bigint[][];
  source: Obj;
}
export function exact(value: unknown, keys: string[]): Obj {
  if (
    !value ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    Object.keys(value).sort().join(",") !== keys.toSorted().join(",")
  )
    throw Error("ERR_EG_SCHEMA");
  return value as Obj;
}
export function decodeFixed(value: unknown, size: number): Uint8Array {
  if (
    typeof value !== "string" ||
    value.length !== Math.ceil((size * 8) / 6) ||
    !/^[A-Za-z0-9_-]+$/.test(value)
  )
    throw Error("ERR_EG_ENCODING");
  const bin = atob(value.replaceAll("-", "+").replaceAll("_", "/"));
  if (
    bin.length !== size ||
    encodeBytes(Uint8Array.from(bin, (c) => c.charCodeAt(0))) !== value
  )
    throw Error("ERR_EG_ENCODING");
  return Uint8Array.from(bin, (c) => c.charCodeAt(0));
}
export function encodeBytes(b: Uint8Array): string {
  return btoa(Array.from(b, (c) => String.fromCharCode(c)).join(""))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replace(/=+$/, "");
}
export function integer(b: Uint8Array): bigint {
  return BigInt(
    "0x" + Array.from(b, (x) => x.toString(16).padStart(2, "0")).join(""),
  );
}
export const prime = integer(decodeFixed(pinned.p, 512)),
  order = integer(decodeFixed(pinned.q, 32)),
  generator = integer(decodeFixed(pinned.g, 512));
export const mod = (x: bigint, m = order) => ((x % m) + m) % m;
export function power(a: bigint, b: bigint): bigint {
  let out = 1n;
  for (; b > 0n; b >>= 1n, a = (a * a) % prime)
    if (b & 1n) out = (out * a) % prime;
  return out;
}
export function bytes(n: bigint, size = 512): Uint8Array {
  const hex = n.toString(16).padStart(size * 2, "0");
  if (n < 0n || hex.length > size * 2) throw Error("ERR_EG_INTEGER");
  return Uint8Array.from(hex.match(/../g)!, (b) => parseInt(b, 16));
}
export function scalar(value: unknown): bigint {
  const n = integer(decodeFixed(value, 32));
  if (n >= order) throw Error("ERR_EG_SCALAR");
  return n;
}
export function group(value: unknown, identity = true): bigint {
  const n = integer(decodeFixed(value, 512));
  if (
    n <= 0n ||
    n >= prime ||
    (!identity && n === 1n) ||
    power(n, order) !== 1n
  )
    throw Error("ERR_EG_GROUP");
  return n;
}
function array(v: unknown, n: number): unknown[] {
  if (!Array.isArray(v) || v.length !== n) throw Error("ERR_EG_SCHEMA");
  return v;
}
function eq(a: unknown, b: unknown) {
  if (a !== b) throw Error("ERR_EG_CONTEXT");
}
export function index(n: number): Uint8Array {
  const b = new Uint8Array(4);
  new DataView(b.buffer).setUint32(0, n);
  return b;
}
export function concat(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((a, b) => a + b.length, 0));
  let offset = 0;
  for (const p of parts) {
    out.set(p, offset);
    offset += p.length;
  }
  return out;
}
export async function hash(
  key: Uint8Array,
  tag: number,
  ...parts: Uint8Array[]
): Promise<Uint8Array> {
  const k = await crypto.subtle.importKey(
    "raw",
    new Uint8Array(key).buffer,
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  return new Uint8Array(
    await crypto.subtle.sign(
      "HMAC",
      k,
      new Uint8Array(concat(Uint8Array.of(tag), ...parts)).buffer,
    ),
  );
}
const hp = decodeFixed(pinned.parameter_base_hash, 32);
export async function verifyCeremony(
  context: ElectionContext,
  encoded: string,
): Promise<PublicElection> {
  if (decimal(context.protocol_epoch, 0xffffffffffffffffn) === 0n)
    throw Error("ERR_EG_EPOCH");
  if (
    !/^[1-9][0-9]*$/.test(context.protocol_epoch) ||
    context.guardian_ids.length !== 5 ||
    new Set(context.guardian_ids).size !== 5 ||
    context.guardian_ids.join(",") !== context.guardian_ids.toSorted().join(",")
  )
    throw Error("ERR_EG_CONTEXT");
  const d = decodeBundle(encoded, "ceremony");
  await checkCeremonyLink(
    d,
    context.network_id,
    context.poll_id,
    context.proposal_id,
  );
  const manifest = pinned.manifest_template.replaceAll(
    "POLL_ID",
    context.poll_id,
  );
  eq(
    new TextDecoder("utf-8", { fatal: true }).decode(
      d.files.get("manifest.json"),
    ),
    manifest,
  );
  const c = exact(parse(d.files.get("context.json")!), [
    "encoding",
    "number_of_guardians",
    "quorum",
    "parameter_base_hash",
    "manifest_hash",
    "election_base_hash",
    "extended_base_hash",
    "joint_public_key",
  ]);
  eq(c.encoding, "eg-civic-compact-1");
  eq(c.number_of_guardians, "5");
  eq(c.quorum, "3");
  eq(c.parameter_base_hash, pinned.parameter_base_hash);
  const hm = await hash(
      hp,
      1,
      index(utf8.encode(manifest).length),
      utf8.encode(manifest),
    ),
    hb = await hash(hp, 2, hm, index(5), index(3));
  eq(c.manifest_hash, encodeBytes(hm));
  eq(c.election_base_hash, encodeBytes(hb));
  let key = 1n;
  const coefficients: bigint[][] = [];
  for (let i = 0; i < 5; i++) {
    const guardian = exact(parse(d.files.get(`guardians/0${i + 1}.json`)!), [
      "account_id",
      "sequence_order",
      "coefficient_proofs",
    ]);
    eq(guardian.account_id, context.guardian_ids[i]);
    eq(guardian.sequence_order, String(i + 1));
    const coeffs: bigint[] = [];
    for (const [j, proof] of array(guardian.coefficient_proofs, 3).entries()) {
      const p = exact(proof, ["public_key", "challenge", "response"]),
        pk = group(p.public_key, false),
        c = scalar(p.challenge),
        r = scalar(p.response),
        h = (power(generator, r) * power(pk, c)) % prime;
      eq(
        c,
        integer(
          await hash(hp, 0x10, index(i + 1), index(j), bytes(pk), bytes(h)),
        ) % order,
      );
      coeffs.push(pk);
      if (j === 0) key = (key * pk) % prime;
    }
    coefficients.push(coeffs);
  }
  if (key === 1n) throw Error("ERR_EG_KEY");
  eq(c.joint_public_key, encodeBytes(bytes(key)));
  const he = await hash(hb, 0x12, bytes(key));
  eq(c.extended_base_hash, encodeBytes(he));
  return { context, key, he, coefficients, source: c };
}
async function rangeProof(
  e: PublicElection,
  a: bigint,
  b: bigint,
  value: unknown,
): Promise<void> {
  const parts = [bytes(e.key), bytes(a), bytes(b)];
  let sum = 0n;
  for (const [j, p] of array(value, 2).entries()) {
    const proof = exact(p, ["challenge", "response"]),
      c = scalar(proof.challenge),
      r = scalar(proof.response);
    parts.push(
      bytes((power(generator, r) * power(a, c)) % prime),
      bytes((power(e.key, mod(r - BigInt(j) * c)) * power(b, c)) % prime),
    );
    sum += c;
  }
  eq(mod(sum), integer(await hash(e.he, 0x21, ...parts)) % order);
}
export function pair(value: unknown, identity = true): [bigint, bigint] {
  const o = exact(value, ["alpha", "beta"]);
  return [group(o.alpha, identity), group(o.beta)];
}
export function encodedPair(a: bigint, b: bigint) {
  return { alpha: encodeBytes(bytes(a)), beta: encodeBytes(bytes(b)) };
}
export async function verifyBallot(
  e: PublicElection,
  encoded: string,
): Promise<{
  ballot: Obj;
  ciphertext: { alpha: string; beta: string };
  confirmation_code: string;
  ballot_id: string;
  raw: Uint8Array;
}> {
  const d = decodeBundle(encoded, "ballot"),
    raw = d.files.get("ballot.json")!;
  const b = exact(parse(raw), [
    "encoding",
    "ballot_id",
    "ballot_style_id",
    "encrypting_device",
    "timestamp",
    "code_baux",
    "confirmation_code",
    "election_id",
    "state",
    "contests",
  ]);
  eq(b.encoding, "eg-civic-compact-1");
  if (typeof b.ballot_id !== "string" || !/^[0-9a-f]{64}$/.test(b.ballot_id))
    throw Error("ERR_EG_ID");
  eq(b.ballot_style_id, "civic");
  eq(b.encrypting_device, "civic:" + e.context.poll_id);
  eq(b.timestamp, "0");
  eq(b.code_baux, "");
  eq(b.election_id, encodeBytes(e.he));
  eq(b.state, "CAST");
  const c = exact(array(b.contests, 1)[0], [
    "contest_id",
    "sequence_order",
    "votes_allowed",
    "contest_hash",
    "selections",
    "proof",
    "encrypted_contest_data",
  ]);
  eq(c.contest_id, "civic_question");
  eq(c.sequence_order, "0");
  eq(c.votes_allowed, "1");
  const s = exact(array(c.selections, 1)[0], [
    "selection_id",
    "sequence_order",
    "encrypted_vote",
    "proof",
  ]);
  eq(s.selection_id, "yes_bit");
  eq(s.sequence_order, "0");
  const [a, beta] = pair(s.encrypted_vote, false);
  await rangeProof(e, a, beta, s.proof);
  await rangeProof(e, a, beta, c.proof);
  const ch = await hash(
    e.he,
    0x23,
    index(0),
    bytes(e.key),
    bytes(a),
    bytes(beta),
  );
  eq(c.contest_hash, encodeBytes(ch));
  eq(b.confirmation_code, encodeBytes(await hash(e.he, 0x24, ch)));
  const data = exact(c.encrypted_contest_data, ["c0", "c1", "c2", "num_bytes"]);
  group(data.c0, false);
  decodeFixed(data.c1, 64);
  decodeFixed(data.c2, 32);
  eq(data.num_bytes, "62");
  return {
    ballot: b,
    ciphertext: encodedPair(a, beta),
    confirmation_code: b.confirmation_code as string,
    ballot_id: b.ballot_id,
    raw,
  };
}
export function zeroAggregate(): string {
  return canonical(encodedPair(1n, 1n));
}
export function addAggregate(
  aggregate: string,
  ciphertext: { alpha: string; beta: string },
): string {
  const [a, b] = pair(parse(aggregate)),
    [x, y] = pair(ciphertext, false);
  return canonical(encodedPair((a * x) % prime, (b * y) % prime));
}
function uint(v: unknown): bigint {
  if (typeof v !== "string" || !/^(0|[1-9][0-9]{0,19})$/.test(v))
    throw Error("ERR_EG_COUNT");
  const n = BigInt(v);
  if (n > 0xffffffffffffffffn) throw Error("ERR_EG_COUNT");
  return n;
}
export async function verifyTally(
  e: PublicElection,
  aggregate: string,
  count: string,
  encoded: string,
): Promise<{ yes: string; no: string }> {
  const d = decodeBundle(encoded, "tally"),
    t = exact(parse(d.files.get("tally.json")!), [
      "encoding",
      "id",
      "election_id",
      "ciphertext",
      "tally",
      "b_over_m",
      "proof",
      "shares",
    ]);
  eq(t.encoding, "eg-civic-compact-1");
  eq(t.id, e.context.poll_id);
  eq(t.election_id, encodeBytes(e.he));
  eq(canonical(t.ciphertext), aggregate);
  const yes = uint(t.tally),
    n = uint(count);
  if (yes > n) throw Error("ERR_EG_COUNT");
  const [a, b] = pair(t.ciphertext),
    bom = group(t.b_over_m);
  eq(power(e.key, yes), bom);
  const m = (b * power(bom, prime - 2n)) % prime;
  const p = exact(t.proof, ["challenge", "response"]),
    challenge = scalar(p.challenge),
    r = scalar(p.response),
    ga = (power(generator, r) * power(e.key, challenge)) % prime,
    gb = (power(a, r) * power(m, challenge)) % prime;
  eq(
    challenge,
    integer(
      await hash(
        e.he,
        0x30,
        bytes(e.key),
        bytes(a),
        bytes(b),
        bytes(ga),
        bytes(gb),
        bytes(m),
      ),
    ) % order,
  );
  if (!Array.isArray(t.shares) || t.shares.length < 3 || t.shares.length > 5)
    throw Error("ERR_EG_QUORUM");
  const shares = t.shares.map((s) =>
      exact(s, ["guardian_index", "share", "a", "b", "response"]),
    ),
    indices = shares.map((s) => uint(s.guardian_index));
  if (
    indices.some((i, j) => i < 1n || i > 5n || (j > 0 && i <= indices[j - 1]!))
  )
    throw Error("ERR_EG_QUORUM");
  let prodM = 1n,
    prodA = 1n,
    prodB = 1n,
    sum = 0n;
  for (const [k, s] of shares.entries()) {
    const i = indices[k]!;
    let w = 1n;
    for (const j of indices) if (j !== i) w = mod(w * j * inverse(mod(j - i)));
    let vk = 1n;
    for (const coeff of e.coefficients) {
      let exponent = 1n;
      for (const c of coeff) {
        vk = (vk * power(c, exponent)) % prime;
        exponent *= i;
      }
    }
    const mi = group(s.share),
      ai = group(s.a),
      bi = group(s.b),
      ri = scalar(s.response),
      ci = mod(challenge * w);
    eq((power(generator, ri) * power(vk, ci)) % prime, ai);
    eq((power(a, ri) * power(mi, ci)) % prime, bi);
    prodM = (prodM * power(mi, w)) % prime;
    prodA = (prodA * ai) % prime;
    prodB = (prodB * bi) % prime;
    sum += ri;
  }
  eq(prodM, m);
  eq(prodA, ga);
  eq(prodB, gb);
  eq(mod(sum), r);
  return { yes: yes.toString(), no: (n - yes).toString() };
}
function inverse(a: bigint): bigint {
  let x = 1n,
    y = 0n,
    b = order;
  for (; b !== 0n; ) {
    const q = a / b;
    [a, b] = [b, a - q * b];
    [x, y] = [y, x - q * y];
  }
  if (a !== 1n) throw Error("ERR_EG_INVERSE");
  return mod(x);
}
