// Node reference codec for the v0.1 interoperability harness. Browser key
// storage, Semaphore and the PWA belong to later milestones.
import {
  createHash,
  createPrivateKey,
  createPublicKey,
  sign,
  verify,
} from "node:crypto";
import { readFileSync, readdirSync } from "node:fs";
import { Ajv2020 } from "ajv/dist/2020.js";

import { canonical, parse } from "./codec.js";
export { canonical, parse } from "./codec.js";
export type { JSONValue } from "./codec.js";
const ajv = new Ajv2020({ strict: true, allErrors: true });
for (const file of readdirSync("spec/schemas").filter((f) =>
  f.endsWith(".schema.json"),
))
  ajv.addSchema(JSON.parse(readFileSync("spec/schemas/" + file, "utf8")));
export function validate<T>(name: string, value: unknown): T {
  const schema = ajv.getSchema("https://civora.invalid/v0.1/" + name);
  if (!schema || !schema(value))
    throw Error("ERR_SCHEMA " + JSON.stringify(schema?.errors));
  return value as T;
}
export function decode<T>(name: string, raw: Uint8Array | string): T {
  return validate<T>(name, parse(raw));
}
export function sha(...parts: Uint8Array[]): Buffer {
  const h = createHash("sha256");
  for (const p of parts) h.update(p);
  return h.digest();
}
export function domain(tag: string, value: unknown): string {
  return sha(
    Buffer.from("CIVIC/1/" + tag + "\n"),
    Buffer.from(canonical(value)),
  ).toString("hex");
}
export function uint(value: string): bigint {
  if (!/^(0|[1-9][0-9]*)$/.test(value)) throw Error("invalid uint64");
  const n = BigInt(value);
  if (n > 0xffffffffffffffffn) throw Error("uint64 overflow");
  return n;
}
export function blob(value: string): Buffer {
  const b = Buffer.from(value, "base64url");
  if (b.toString("base64url") !== value) throw Error("invalid base64url");
  return b;
}
export function chainID(network: string): string {
  if (!/^[0-9a-f]{64}$/.test(network)) throw Error("invalid network");
  let bits = 0,
    value = 0,
    out = "civ1-";
  const alphabet = "abcdefghijklmnopqrstuvwxyz234567";
  for (const b of Buffer.from(network, "hex").subarray(0, 20)) {
    value = (value << 8) | b;
    bits += 8;
    while (bits >= 5) {
      out += alphabet[(value >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  return out;
}
export function testKey(seed: string): { seed: string; publicKey: string } {
  if (!/^[0-9a-f]{64}$/.test(seed)) throw Error("invalid TEST seed");
  const privateKey = createPrivateKey({
    key: Buffer.from("302e020100300506032b657004220420" + seed, "hex"),
    type: "pkcs8",
    format: "der",
  });
  return {
    seed,
    publicKey: (
      createPublicKey(privateKey).export({
        type: "spki",
        format: "der",
      }) as Buffer
    )
      .subarray(-32)
      .toString("hex"),
  };
}
export function signDigest(digest: string, seed: string): string {
  const key = createPrivateKey({
    key: Buffer.from("302e020100300506032b657004220420" + seed, "hex"),
    type: "pkcs8",
    format: "der",
  });
  return sign(null, Buffer.from(digest, "hex"), key).toString("hex");
}
export function verifyDigest(
  digest: string,
  signature: string,
  pk: string,
): boolean {
  try {
    return verify(
      null,
      Buffer.from(digest, "hex"),
      createPublicKey({
        key: Buffer.from("302a300506032b6570032100" + pk, "hex"),
        type: "spki",
        format: "der",
      }),
      Buffer.from(signature, "hex"),
    );
  } catch {
    return false;
  }
}
export interface Payload {
  signing_pk: string;
  recovery_pk: string;
  commitment: string;
}
export interface Body {
  v: "1" | "2";
  network_id: string;
  type: "account.register" | "account.rotate" | "account.retire";
  actor_id: string;
  nonce: string;
  payload: Payload | Record<string, never>;
  protocol_epoch?: string;
}
export interface Transaction {
  body: Body;
  auth: { scheme: "ed25519"; signature: string };
}
export const txID = (body: Body): string =>
  domain(body.v === "1" ? "tx" : "tx/2", body);
export function signTransaction(body: Body, TEST_seed: string): Transaction {
  return {
    body,
    auth: { scheme: "ed25519", signature: signDigest(txID(body), TEST_seed) },
  };
}
export interface Module {
  id: string;
  status: "prepared" | "active" | "legacy_verify_only" | "revoked";
  operations: string[];
  parameters: { format: string };
  implementation_sha256: string;
  artifacts: string[];
  dependencies: string[];
  retirement_policy: string;
}
export interface Registry {
  protocol_epoch: string;
  state_schema: "1";
  transaction_version: "1" | "2";
  modules: Module[];
}
export interface Plan {
  network_id: string;
  from_epoch: string;
  to_epoch: string;
  from_state_schema: "1";
  to_state_schema: "1";
  hash_suite: "sha256";
  modules: Module[];
  artifacts: { target: string; sha256: string; size: string }[];
  provenance_sha256: string;
  sbom_sha256: string;
  reproducible_build_sha256: string;
  conformance_sha256: string;
  chains: string[];
  activation: {
    not_before_ms: string;
    expires_at_ms: string;
    notice_ms: string;
    prerequisite: "no_open_polls";
  };
  migration: {
    id: "identity_v0_1";
    precondition_sha256: string;
    postcondition_sha256: string;
    replay_sha256: string;
    stop_point: "before_activation_block";
    failure_procedure: string;
  };
  on_call: string;
  public_notice: string;
  transaction_version: "1" | "2";
}
export interface Instance {
  plan_id: string;
  heights: { chain_id: string; height: string }[];
  arm_window_start_ms: string;
  arm_window_end_ms: string;
  expires_at_ms: string;
}
export interface Armed {
  plan_id: string;
  plan: Plan;
  instance_id: string;
  instance: Instance;
  authorization: "development_fixture";
}
export interface Config {
  network_id: string;
  chain_id: string;
  voting_profile: "inline_dev";
  mode: "development";
  registry: Registry;
  activations: Armed[];
}
export interface Block {
  height: string;
  time_ms: string;
  transactions: Transaction[];
}
export interface Account {
  signing_pk: string;
  recovery_pk: string;
  commitment: string;
  next_nonce: string;
  status: "active" | "retired";
  registered_height: string;
  used_keys: string[];
  used_commitments: string[];
  eligibility_index?: string;
}
export interface Event {
  id: string;
  type: string;
  subject: string;
}
export interface Entry {
  table: string;
  key: string | string[];
  value: unknown;
}
export interface State {
  height: string;
  time_ms: string;
  entries: Entry[];
}
export interface Fixture {
  name: string;
  genesis: Config;
  blocks: Block[];
  expected_roots: string[];
  expected_events: Event[][];
}
