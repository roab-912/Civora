// Reproducible public TEST KEYS ONLY. Never use this generator for a network.
import { mkdirSync, writeFileSync, readFileSync } from "node:fs";
import {
  canonical,
  chainID,
  domain,
  sha,
  signTransaction,
  testKey,
  type Body,
  type Config,
  type Fixture,
  type Module,
  type Plan,
  type Armed,
  type Block,
  type Transaction,
} from "../web/src/protocol.js";
import { Engine } from "../web/src/core.js";
const write = (name: string, value: unknown): void => {
  const path = "spec/fixtures/" + name + ".json",
    output = canonical(value);
  if (process.argv.includes("--check")) {
    if (readFileSync(path, "utf8") !== output)
      throw Error("stale fixture: " + path);
  } else writeFileSync(path, output);
};
mkdirSync("spec/fixtures", { recursive: true });
const network =
  "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f";
const seed = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60";
const recoverySeed =
  "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb";
const signing = testKey(seed),
  recovery = testKey(recoverySeed),
  rotated = testKey(
    sha(Buffer.from("CIVORA TEST ONLY signing 2")).toString("hex"),
  ),
  recovery2 = testKey(
    sha(Buffer.from("CIVORA TEST ONLY recovery 2")).toString("hex"),
  );
write("keys.TEST_ONLY", {
  warning: "PUBLIC DEVELOPMENT TEST KEYS — NEVER USE FOR REAL ACCOUNTS",
  signing,
  recovery,
  rotated,
  recovery2,
});
const actor = domain("account", [network, signing.publicKey]);
const body: Body = {
  v: "1",
  network_id: network,
  type: "account.register",
  actor_id: actor,
  nonce: "0",
  payload: {
    signing_pk: signing.publicKey,
    recovery_pk: recovery.publicKey,
    commitment: "5",
  },
};
const register = signTransaction(body, seed);
const rotate = signTransaction(
  {
    ...body,
    type: "account.rotate",
    nonce: "1",
    payload: {
      signing_pk: rotated.publicKey,
      recovery_pk: recovery2.publicKey,
      commitment: "6",
    },
  },
  recoverySeed,
);
const retire = signTransaction(
  { ...body, type: "account.retire", nonce: "2", payload: {} },
  recovery2.seed,
);
write("vector13", {
  network_id: network,
  chain_id: "civ1-aaaqeayeaudaocajbifqydiob4ibceqt",
  seed,
  account_id:
    "22b71280af1634ff95f0dea136f75236cbf89ae8f35e8ee25cd22ec9efa9ac3e",
  body,
  tx_id: "1ba464b90e577b7afd7ed5cfdca10a80d9660df246e9835e80be13bdda51c7ad",
  signature:
    "574448c1f4cce01c632a9ecaacfe718334f232e21fa3e85b1fb58d51acd990011b495a1bc41febb000ff291aeeb16567729becb0dcb30a25c9e1b8dd02f5370c",
  empty_root:
    "9b97517f25140762b9e187bae23a31ab78a4230354dece720bbbefcfce8de5a0",
});
const artifact = (label: string): string =>
  sha(Buffer.from("CIVORA TEST FIXTURE " + label)).toString("hex");
const module = (
  id: string,
  op: string,
  format: string,
  status: Module["status"] = "active",
): Module => ({
  id,
  status,
  operations: [op],
  parameters: { format },
  implementation_sha256: artifact(id),
  artifacts: [],
  dependencies: [],
  retirement_policy: "never_reactivate_revoked",
});
// Inventory entries for future adapters are prepared, never operational mocks.
const modules = [
  module("dev.hash.sha256.v1", "state.hash", "sha256"),
  module("dev.account.ed25519.v1", "account.sign", "ed25519"),
  ...[
    ["consensus", "consensus.sign", "ed25519"],
    ["eligibility", "eligibility.prove", "semaphore-v4"],
    ["ballot", "ballot.prove", "electionguard-2"],
    ["tally", "tally.prove", "electionguard-2"],
    ["release", "release.sign", "ed25519"],
    ["recovery", "recovery.sign", "ml-dsa-65"],
    ["recovery-hash", "recovery.hash", "sha3-384"],
  ].map(([id, op, format]) =>
    module("dev.unimplemented." + id!, op!, format!, "prepared"),
  ),
];
const genesis: Config = {
  network_id: network,
  chain_id: chainID(network),
  voting_profile: "inline_dev",
  mode: "development",
  registry: {
    protocol_epoch: "1",
    state_schema: "1",
    transaction_version: "1",
    modules,
  },
  activations: [],
};
function fixture(name: string, config: Config, blocks: Block[]): Fixture {
  let engine = new Engine(canonical(config), 2n);
  const roots: string[] = [],
    events: Fixture["expected_events"] = [];
  for (const block of blocks) {
    const result = engine.apply(canonical(block));
    engine = result.engine;
    roots.push(engine.root());
    events.push(result.events);
  }
  const f: Fixture = {
    name,
    genesis: config,
    blocks,
    expected_roots: roots,
    expected_events: events,
  };
  write(name, f);
  return f;
}
const block = (
  height: string,
  time_ms: string,
  transactions: Transaction[],
): Block => ({ height, time_ms, transactions });
const accounts = fixture("accounts", genesis, [
  block("1", "1000", [register]),
  block("2", "2000", [rotate]),
  block("3", "1500", []),
  block("4", "4000", [retire]),
]);
const nextModules = structuredClone(modules);
nextModules.find((m) => m.id === "dev.account.ed25519.v1")!.status = "revoked";
nextModules.push(module("dev.account.ed25519.v2", "account.sign", "ed25519"));
const plan: Plan = {
  network_id: network,
  from_epoch: "1",
  to_epoch: "2",
  from_state_schema: "1",
  to_state_schema: "1",
  hash_suite: "sha256",
  modules: nextModules,
  artifacts: [
    {
      target: "development-fixture",
      sha256: artifact("not a release binary"),
      size: "0",
    },
  ],
  provenance_sha256: artifact("provenance"),
  sbom_sha256: artifact("sbom"),
  reproducible_build_sha256: artifact("build"),
  conformance_sha256: artifact("conformance"),
  chains: [genesis.chain_id],
  activation: {
    not_before_ms: "0",
    expires_at_ms: "10000",
    notice_ms: "0",
    prerequisite: "no_open_polls",
  },
  migration: {
    id: "identity_v0_1",
    precondition_sha256: artifact("preconditions"),
    postcondition_sha256: artifact("postconditions"),
    replay_sha256: artifact("replay"),
    stop_point: "before_activation_block",
    failure_procedure: "STOP. Development fixture only; no emergency tooling.",
  },
  on_call: "TEST ONLY: no operator",
  public_notice: "TEST ONLY: activation at development height 3",
  transaction_version: "2",
};
const plan_id = domain("upgrade/plan", plan);
const instance = {
  plan_id,
  heights: [{ chain_id: genesis.chain_id, height: "3" }],
  arm_window_start_ms: "0",
  arm_window_end_ms: "100",
  expires_at_ms: "10000",
};
const armed: Armed = {
  plan_id,
  plan,
  instance,
  instance_id: domain("upgrade/instance", instance),
  authorization: "development_fixture",
};
const activationGenesis: Config = {
  ...structuredClone(genesis),
  activations: [armed],
};
const rotate2 = signTransaction(
  { ...rotate.body, v: "2", protocol_epoch: "2" },
  recoverySeed,
);
const retire2 = signTransaction(
  { ...retire.body, v: "2", protocol_epoch: "2" },
  recovery2.seed,
);
fixture("activation", activationGenesis, [
  block("1", "1000", [register]),
  block("2", "2000", []),
  block("3", "3000", [rotate2]),
  block("4", "4000", [retire2]),
]);
const rollback: Plan = {
  ...structuredClone(plan),
  from_epoch: "2",
  to_epoch: "3",
};
rollback.modules.find((m) => m.id === "dev.account.ed25519.v1")!.status =
  "active";
rollback.modules.find((m) => m.id === "dev.account.ed25519.v2")!.status =
  "revoked";
write("revoked-suite", {
  registry: {
    protocol_epoch: "2",
    state_schema: "1",
    transaction_version: "2",
    modules: nextModules,
  },
  plan: rollback,
  expected_error: "ERR_REVOKED_ROLLBACK",
});
const negative = (
  name: string,
  config: Config,
  prefix: Block[],
  rejected: Block,
  error: string,
): void =>
  write(name, {
    name,
    genesis: config,
    prefix_blocks: prefix,
    rejected_block: rejected,
    expected_error: error,
  });
negative(
  "reject-inter-network",
  genesis,
  [],
  block("1", "1000", [
    signTransaction({ ...body, network_id: "ff".repeat(32) }, seed),
  ]),
  "ERR_NETWORK",
);
negative(
  "reject-inter-epoch",
  activationGenesis,
  accounts.blocks.slice(0, 1).concat(block("2", "2000", [])),
  block("3", "3000", [rotate]),
  "ERR_EPOCH",
);
negative(
  "reject-concurrent-nonce",
  genesis,
  [accounts.blocks[0]!],
  block("2", "2000", [rotate, rotate]),
  "ERR_NONCE",
);
negative(
  "reject-old-recovery",
  genesis,
  accounts.blocks.slice(0, 2),
  block("3", "3000", [signTransaction(retire.body, recoverySeed)]),
  "ERR_SIGNATURE",
);
negative(
  "reject-retired-account",
  genesis,
  accounts.blocks,
  block("5", "5000", [
    signTransaction({ ...retire.body, nonce: "3" }, recovery2.seed),
  ]),
  "ERR_ACCOUNT",
);
negative(
  "reject-signing-key-retirement",
  genesis,
  [accounts.blocks[0]!],
  block("2", "2000", [signTransaction({ ...retire.body, nonce: "1" }, seed)]),
  "ERR_SIGNATURE",
);
negative(
  "reject-foreign-v2-epoch",
  activationGenesis,
  [block("1", "1000", [register]), block("2", "2000", [])],
  block("3", "3000", [
    signTransaction({ ...rotate2.body, protocol_epoch: "3" }, recoverySeed),
  ]),
  "ERR_EPOCH",
);
const encodingCases: { name: string; raw_base64url: string; schema: string }[] =
  [];
const invalid = (
  name: string,
  raw: string | Buffer,
  schema = "transaction",
): void => {
  encodingCases.push({
    name,
    raw_base64url: Buffer.from(raw).toString("base64url"),
    schema,
  });
};
const txText = canonical(register);
invalid("trailing-lf", txText + "\n");
invalid("whitespace", " " + txText);
invalid(
  "duplicate-key",
  txText.replace('"nonce":"0"', '"nonce":"0","nonce":"0"'),
);
invalid("unknown-field", txText.replace('"v":"1"', '"v":"1","extra":"x"'));
invalid("numeric-nonce", txText.replace('"nonce":"0"', '"nonce":0'));
invalid("leading-zero", txText.replace('"nonce":"0"', '"nonce":"00"'));
invalid(
  "uint-overflow",
  txText.replace('"nonce":"0"', '"nonce":"18446744073709551616"'),
);
invalid("null-payload", txText.replace(canonical(body.payload), "null"));
invalid(
  "uppercase-key",
  txText.replace(signing.publicKey, signing.publicKey.toUpperCase()),
);
invalid("missing-field", txText.replace('"nonce":"0",', ""));
invalid(
  "nonminimal-escape",
  txText.replace("account.register", "account.\\u0072egister"),
);
invalid("lone-surrogate", '"\\ud800"', "");
invalid("invalid-utf8", Buffer.from([0x22, 0xc0, 0xaf, 0x22]), "");
invalid(
  "utf8-bom",
  Buffer.concat([Buffer.from([0xef, 0xbb, 0xbf]), Buffer.from("{}")]),
  "",
);
invalid("non-nfc", '"e\u0301"', "");
invalid("cr-line-ending", '"a\\rb"', "");
invalid("unknown-version", txText.replace('"v":"1"', '"v":"3"'));
invalid("short-signature", txText.replace(register.auth.signature, "00"));
invalid(
  "proof-auth",
  txText.replace(canonical(register.auth), canonical({ scheme: "proof" })),
);
invalid("trailing-document", txText + "{}");
write("encoding-negative", encodingCases);
