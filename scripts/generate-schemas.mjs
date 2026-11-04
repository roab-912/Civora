import { mkdirSync, writeFileSync, readFileSync } from "node:fs";

function writeGenerated(path, content) {
  if (process.argv.includes("--check")) {
    if (readFileSync(path, "utf8") !== content)
      throw Error("stale schema: " + path);
  } else writeFileSync(path, content);
}
const text = { type: "string", maxLength: 4096 };
const id = { type: "string", pattern: "^[0-9a-f]{64}$" };
// Decimal uint64 range without coercion or floating point in schema engines.
const maximum = "18446744073709551615";
const alternatives = ["0", "[1-9][0-9]{0,18}", maximum];
for (let i = 0; i < maximum.length; i++) {
  const low = i === 0 ? 1 : 0,
    high = Number(maximum[i]) - 1;
  if (high < low) continue;
  const digit = low === high ? String(low) : `[${low}-${high}]`;
  alternatives.push(
    maximum.slice(0, i) + digit + `[0-9]{${maximum.length - i - 1}}`,
  );
}
const uint = { $ref: "https://civora.invalid/v0.1/protocol#/$defs/uint" };
const uintDefinition = {
  type: "string",
  pattern: "^(" + alternatives.join("|") + ")$",
};
const name = { type: "string", pattern: "^[a-z][a-z0-9._/-]{0,95}$" };
const constant = (value) => ({ const: value });
const list = (items, maxItems = 1024) => ({ type: "array", items, maxItems });
const object = (properties) => ({
  type: "object",
  properties,
  required: Object.keys(properties),
  additionalProperties: false,
});
const ref = (key) => ({
  $ref: `https://civora.invalid/v0.1/protocol#/$defs/${key}`,
});
const accountPayload = object({
  signing_pk: id,
  recovery_pk: id,
  commitment: { type: "string", pattern: "^[1-9][0-9]{0,76}$" },
});
const module = object({
  id: name,
  status: { enum: ["prepared", "active", "legacy_verify_only", "revoked"] },
  operations: list(name, 32),
  parameters: object({ format: name }),
  implementation_sha256: id,
  artifacts: list(id, 32),
  dependencies: list(name, 32),
  retirement_policy: name,
});
const registry = object({
  protocol_epoch: uint,
  state_schema: constant("1"),
  transaction_version: { enum: ["1", "2"] },
  modules: list(ref("module"), 64),
});
const artifact = object({ target: name, sha256: id, size: uint });
const plan = object({
  network_id: id,
  from_epoch: uint,
  to_epoch: uint,
  from_state_schema: constant("1"),
  to_state_schema: constant("1"),
  hash_suite: constant("sha256"),
  modules: list(ref("module"), 64),
  artifacts: list(artifact, 64),
  provenance_sha256: id,
  sbom_sha256: id,
  reproducible_build_sha256: id,
  conformance_sha256: id,
  chains: list(name, 256),
  activation: object({
    not_before_ms: uint,
    expires_at_ms: uint,
    notice_ms: uint,
    prerequisite: constant("no_open_polls"),
  }),
  migration: object({
    id: constant("identity_v0_1"),
    precondition_sha256: id,
    postcondition_sha256: id,
    replay_sha256: id,
    stop_point: constant("before_activation_block"),
    failure_procedure: text,
  }),
  on_call: text,
  public_notice: text,
  transaction_version: { enum: ["1", "2"] },
});
const instance = object({
  plan_id: id,
  heights: list(object({ chain_id: name, height: uint }), 256),
  arm_window_start_ms: uint,
  arm_window_end_ms: uint,
  expires_at_ms: uint,
});
const armed = object({
  plan_id: id,
  plan: ref("plan"),
  instance_id: id,
  instance: ref("instance"),
  authorization: constant("development_fixture"),
});
const config = object({
  network_id: id,
  chain_id: name,
  voting_profile: constant("inline_dev"),
  mode: constant("development"),
  registry: ref("registry"),
  activations: list(ref("armed"), 16),
});
const auth = object({
  scheme: constant("ed25519"),
  signature: { type: "string", pattern: "^[0-9a-f]{128}$" },
});
const variants = [];
for (const version of ["1", "2"])
  for (const type of ["account.register", "account.rotate", "account.retire"]) {
    const body = {
      v: constant(version),
      network_id: id,
      type: constant(type),
      actor_id: id,
      nonce: uint,
      payload: type === "account.retire" ? object({}) : ref("payload"),
    };
    if (version === "2") body.protocol_epoch = uint;
    variants.push(object({ body: object(body), auth }));
  }
const transaction = { oneOf: variants };
const account = object({
  signing_pk: id,
  recovery_pk: id,
  commitment: accountPayload.properties.commitment,
  next_nonce: uint,
  status: { enum: ["active", "retired"] },
  registered_height: uint,
  used_keys: list(id, 262144),
  used_commitments: list(accountPayload.properties.commitment, 262144),
});
const entry = {
  oneOf: [
    object({ table: constant("accounts"), key: id, value: ref("account") }),
    object({
      table: constant("network"),
      key: constant("config"),
      value: object({ config: ref("config"), height: uint, time_ms: uint }),
    }),
    object({
      table: constant("upgrades"),
      key: id,
      value: object({
        plan: ref("plan"),
        instance: ref("instance"),
        status: { enum: ["armed", "activated"] },
      }),
    }),
  ],
};
const event = object({
  id,
  type: {
    enum: [
      "account.register",
      "account.rotate",
      "account.retire",
      "protocol.activate",
    ],
  },
  subject: id,
});
const block = object({
  height: uint,
  time_ms: uint,
  transactions: list(ref("transaction"), 8192),
});
const state = object({
  height: uint,
  time_ms: uint,
  entries: list(ref("entry"), 131089),
});
const commit = object({
  block: ref("block"),
  state: ref("state"),
  events: list(ref("event"), 8208),
  app_hash: id,
});
const fixture = object({
  name: text,
  genesis: ref("config"),
  blocks: list(ref("block"), 1024),
  expected_roots: list(id, 1024),
  expected_events: list(list(ref("event"), 8208), 1024),
});
const negative = object({
  name: text,
  genesis: ref("config"),
  prefix_blocks: list(ref("block"), 1024),
  rejected_block: ref("block"),
  expected_error: text,
});
const revoked = object({
  registry: ref("registry"),
  plan: ref("plan"),
  expected_error: text,
});
const definitions = {
  uint: uintDefinition,
  module,
  payload: accountPayload,
  armed,
  entry,
  event,
  transaction,
  config,
  registry,
  plan,
  instance,
  account,
  block,
  state,
  commit,
  fixture,
  negative,
  revoked,
};
mkdirSync("spec/schemas", { recursive: true });
writeGenerated(
  "spec/schemas/protocol.schema.json",
  JSON.stringify(
    {
      $schema: "https://json-schema.org/draft/2020-12/schema",
      $id: "https://civora.invalid/v0.1/protocol",
      $defs: definitions,
    },
    null,
    2,
  ) + "\n",
);
for (const key of [
  "transaction",
  "config",
  "registry",
  "plan",
  "instance",
  "account",
  "block",
  "state",
  "commit",
  "fixture",
  "negative",
  "revoked",
])
  writeGenerated(
    `spec/schemas/${key}.schema.json`,
    JSON.stringify(
      {
        $schema: "https://json-schema.org/draft/2020-12/schema",
        $id: `https://civora.invalid/v0.1/${key}`,
        ...ref(key),
      },
      null,
      2,
    ) + "\n",
  );
