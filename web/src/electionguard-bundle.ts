// Civic's envelope boundary only. No ElectionGuard ciphertext/proof is verified
// here and this module must never be used as a VotingCrypto implementation.
import { parse } from "./codec.js";
import { domain, sha256 } from "./eligibility.js";

export const electionguardSuite = "eg-2.0-civic-1";
export const maxBundleBytes = 128 * 1024;
export const maxFileBytes = 64 * 1024;
export const maxTotalFileBytes = 90 * 1024;
export type BundleKind = "ceremony" | "ballot" | "tally";
export interface BundleFile {
  path: string;
  bytes: string;
}
export interface ElectionGuardBundle {
  suite: typeof electionguardSuite;
  files: BundleFile[];
}
export interface DecodedBundle {
  bundle: ElectionGuardBundle;
  raw: Uint8Array;
  files: Map<string, Uint8Array>;
}
const paths: Record<BundleKind, string[]> = {
  ceremony: [
    "context.json",
    "guardians/01.json",
    "guardians/02.json",
    "guardians/03.json",
    "guardians/04.json",
    "guardians/05.json",
    "link.json",
    "manifest.json",
  ],
  ballot: ["ballot.json"],
  tally: ["tally.json"],
};
function exact(
  value: unknown,
  keys: string[],
): asserts value is Record<string, unknown> {
  if (
    typeof value !== "object" ||
    value === null ||
    Array.isArray(value) ||
    Object.keys(value).sort().join(",") !== keys.toSorted().join(",")
  )
    throw Error("ERR_EG_BUNDLE");
}
function base64url(value: unknown, limit: number): Uint8Array {
  if (typeof value !== "string" || value.length > Math.ceil((limit * 8) / 6))
    throw Error("ERR_EG_SIZE");
  if (!/^[A-Za-z0-9_-]+$/.test(value)) throw Error("ERR_EG_BASE64URL");
  const binary = atob(value.replaceAll("-", "+").replaceAll("_", "/"));
  if (
    btoa(binary)
      .replaceAll("+", "-")
      .replaceAll("/", "_")
      .replace(/=+$/, "") !== value
  )
    throw Error("ERR_EG_BASE64URL");
  const bytes = Uint8Array.from(binary, (c) => c.charCodeAt(0));
  if (bytes.length === 0 || bytes.length > limit) throw Error("ERR_EG_SIZE");
  return bytes;
}
// This decoder preserves opaque ElectionGuard bytes, including JSON numbers and
// its own canonical property order. Only the enclosing Civic object is JCS.
export function decodeBundleBytes(
  raw: Uint8Array,
  kind: BundleKind,
): DecodedBundle {
  if (!Object.hasOwn(paths, kind)) throw Error("ERR_EG_KIND");
  if (raw.length > maxBundleBytes) throw Error("ERR_EG_SIZE");
  const value = parse(raw);
  exact(value, ["suite", "files"]);
  const expected = paths[kind];
  if (
    value.suite !== electionguardSuite ||
    !Array.isArray(value.files) ||
    value.files.length !== expected.length
  )
    throw Error("ERR_EG_BUNDLE");
  const files = new Map<string, Uint8Array>();
  let total = 0;
  for (const [i, file] of value.files.entries()) {
    exact(file, ["path", "bytes"]);
    if (file.path !== expected[i]) throw Error("ERR_EG_FILES");
    const data = base64url(file.bytes, maxFileBytes);
    total += data.length;
    if (total > maxTotalFileBytes) throw Error("ERR_EG_SIZE");
    files.set(expected[i]!, data);
  }
  return {
    bundle: value as unknown as ElectionGuardBundle,
    raw: new Uint8Array(raw),
    files,
  };
}
export function decodeBundle(encoded: string, kind: BundleKind): DecodedBundle {
  return decodeBundleBytes(base64url(encoded, maxBundleBytes), kind);
}
export async function checkCeremonyLink(
  d: DecodedBundle,
  network: string,
  poll: string,
  proposal: string,
): Promise<void> {
  const bytes = d.files.get("link.json");
  if (!bytes) throw Error("ERR_EG_LINK");
  const value = parse(bytes);
  exact(value, ["network_id", "poll_id", "proposal_id", "question_digest"]);
  if (
    Object.values(value).some(
      (v) => typeof v !== "string" || !/^[0-9a-f]{64}$/.test(v),
    )
  )
    throw Error("ERR_EG_LINK");
  if (
    value.network_id !== network ||
    value.poll_id !== poll ||
    value.proposal_id !== proposal ||
    value.question_digest !==
      (await domain("poll/question", [network, poll, proposal]))
  )
    throw Error("ERR_EG_LINK");
}
export async function inspectBundle(d: DecodedBundle, kind: BundleKind) {
  return {
    suite: d.bundle.suite,
    kind,
    envelope_sha256: await sha256(d.raw),
    files: await Promise.all(
      d.bundle.files.map(async (f) => ({
        path: f.path,
        size: String(d.files.get(f.path)!.length),
        sha256: await sha256(d.files.get(f.path)!),
      })),
    ),
    cryptography_verified: false as const,
    suite_qualified: false as const,
  };
}
