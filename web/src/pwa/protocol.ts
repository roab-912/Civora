// Browser implementation of the reference protocol port. The build supplies
// precompiled validators: no Node crypto, filesystem or runtime code generation.
import { Buffer } from "./buffer.js";
import { sha256 } from "@noble/hashes/sha256";
import { ed25519 } from "@noble/curves-comet/ed25519.js";
import { canonical, parse } from "../codec.js";
export { canonical, parse } from "../codec.js";
export type { Entry } from "../protocol.js";
type Validator = ((value: unknown) => boolean) & { errors?: unknown };
let validators: Record<string, Validator> = {};
export function installValidators(value: Record<string, Validator>) {
  validators = value;
}
export function validate<T>(name: string, value: unknown): T {
  if (!validators["v_" + name]?.(value)) throw Error("ERR_SCHEMA " + name);
  return value as T;
}
export function decode<T>(name: string, raw: string | Uint8Array): T {
  return validate<T>(name, parse(raw));
}
export function sha(...parts: Uint8Array[]): Buffer {
  const h = sha256.create();
  for (const p of parts) h.update(p);
  return Buffer.from(h.digest());
}
export function domain(tag: string, value: unknown): string {
  return sha(Buffer.from("CIVIC/1/" + tag + "\n" + canonical(value))).toString(
    "hex",
  );
}
export function uint(value: string): bigint {
  if (
    typeof value !== "string" ||
    !/^(0|[1-9][0-9]*)$/.test(value) ||
    value.length > 20 ||
    BigInt(value) > 0xffffffffffffffffn
  )
    throw Error("ERR_INTEGER");
  return BigInt(value);
}
export function blob(value: string): Buffer {
  if (typeof value !== "string") throw Error("ERR_ENCODING");
  const b = Buffer.from(value, "base64url");
  if (b.toString("base64url") !== value) throw Error("ERR_ENCODING");
  return b;
}
export function verifyDigest(
  digest: string,
  signature: string,
  key: string,
): boolean {
  try {
    return ed25519.verify(
      Buffer.from(signature, "hex"),
      Buffer.from(digest, "hex"),
      Buffer.from(key, "hex"),
      { zip215: false },
    );
  } catch {
    return false;
  }
}
export function randomHex(n: number): string {
  return Buffer.from(crypto.getRandomValues(new Uint8Array(n))).toString("hex");
}
export function publicKey(seed: string): string {
  if (!/^[0-9a-f]{64}$/.test(seed)) throw Error("ERR_KEY");
  return Buffer.from(ed25519.getPublicKey(Buffer.from(seed, "hex"))).toString(
    "hex",
  );
}
export function signDigest(digest: string, seed: string): string {
  return Buffer.from(
    ed25519.sign(Buffer.from(digest, "hex"), Buffer.from(seed, "hex")),
  ).toString("hex");
}
export function chainID(network: string): string {
  if (!/^[0-9a-f]{64}$/.test(network)) throw Error("ERR_NETWORK");
  let bits = 0,
    value = 0,
    out = "civ1-";
  for (const byte of Buffer.from(network, "hex").subarray(0, 20)) {
    value = (value << 8) | byte;
    bits += 8;
    while (bits >= 5) {
      out += "abcdefghijklmnopqrstuvwxyz234567"[(value >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  return out;
}
