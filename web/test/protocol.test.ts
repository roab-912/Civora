import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { spawnSync } from "node:child_process";
import test from "node:test";
import {
  canonical,
  decode,
  parse,
  domain,
  chainID,
  signDigest,
  testKey,
  verifyDigest,
  txID,
  blob,
  uint,
  signTransaction,
  type Body,
  type Block,
  type Config,
  type Fixture,
  type Registry,
  type Plan,
  type Transaction,
} from "../src/protocol.js";
import { Engine } from "../src/core.js";
import {
  authorize,
  nextRegistry,
  checkActivation,
  validateRegistry,
} from "../src/registry.js";
import { stateRoot } from "../src/tree.js";
const read = (name: string): string =>
  readFileSync("spec/fixtures/" + name + ".json", "utf8");
const fixture = (name: string): Fixture =>
  decode<Fixture>("fixture", read(name));

test("§13: exact domain, chain, account, signature and empty tree", () => {
  const v = JSON.parse(read("vector13")) as {
    network_id: string;
    chain_id: string;
    seed: string;
    account_id: string;
    body: Body;
    tx_id: string;
    signature: string;
    empty_root: string;
  };
  const key = testKey(v.seed);
  assert.equal(chainID(v.network_id), v.chain_id);
  assert.equal(domain("account", [v.network_id, key.publicKey]), v.account_id);
  assert.equal(txID(v.body), v.tx_id);
  assert.equal(signDigest(v.tx_id, v.seed), v.signature);
  assert.ok(verifyDigest(v.tx_id, v.signature, key.publicKey));
  assert.equal(stateRoot([]), v.empty_root);
});
for (const name of ["accounts", "activation"])
  test(name + ": deterministic replay, shared roots and events", () => {
    const f = fixture(name);
    function replay(): Engine {
      let engine = new Engine(canonical(f.genesis), 2n);
      for (const [i, b] of f.blocks.entries()) {
        const r = engine.apply(canonical(b));
        engine = r.engine;
        assert.equal(engine.root(), f.expected_roots[i]);
        assert.deepEqual(r.events, f.expected_events[i]);
        decode("state", canonical(engine.snapshot()));
      }
      return engine;
    }
    const a = replay(),
      b = replay();
    assert.deepEqual(a.snapshot(), b.snapshot());
    assert.equal(a.accounts.size, 1);
    const account = [...a.accounts.values()][0]!;
    assert.equal(account.status, "retired");
    assert.equal(account.next_nonce, "3");
    assert.deepEqual(account.used_commitments, ["5", "6"]);
  });
for (const file of readdirSync("spec/fixtures").filter((f) =>
  f.startsWith("reject-"),
))
  test(file + ": failure is atomic", () => {
    const f = decode<{
      genesis: Config;
      prefix_blocks: Block[];
      rejected_block: Block;
      expected_error: string;
    }>("negative", readFileSync("spec/fixtures/" + file));
    let engine = new Engine(canonical(f.genesis), 2n);
    for (const b of f.prefix_blocks) engine = engine.apply(canonical(b)).engine;
    const before = canonical(engine.snapshot());
    assert.throws(
      () => engine.apply(canonical(f.rejected_block)),
      new RegExp(f.expected_error),
    );
    assert.equal(canonical(engine.snapshot()), before);
  });
const encoding = JSON.parse(read("encoding-negative")) as {
  name: string;
  raw_base64url: string;
  schema: string;
}[];
for (const c of encoding)
  test("reject encoding: " + c.name, () => {
    const raw = blob(c.raw_base64url);
    assert.throws(() => (c.schema ? decode(c.schema, raw) : parse(raw)));
  });
test("JCS uses UTF-16 key order, exact escapes and no HTML escaping", () => {
  const input = {
    "\ue000": "<>&\u2028",
    "😀": "é\n\t\x01",
    "1": "one",
    "10": "ten",
    "2": "two",
  };
  const expected =
    '{"1":"one","10":"ten","2":"two","😀":"é\\n\\t\\u0001","\ue000":"<>&\u2028"}';
  assert.equal(canonical(input), expected);
  assert.deepEqual(parse(expected), input);
});
test("strict base64url and uint64 bounds", () => {
  assert.equal(blob("Zg").toString(), "f");
  for (const s of ["Zg=", "Zh", "Zg==", "+w", "/w", "Zg\n"])
    assert.throws(() => blob(s));
  for (const s of ["00", "-1", "18446744073709551616", "1.0", "+1"])
    assert.throws(() => uint(s));
  assert.equal(uint("18446744073709551615"), 0xffffffffffffffffn);
});
test("old node stops before applying activation block", () => {
  const f = fixture("activation");
  let engine = new Engine(canonical(f.genesis));
  for (const b of f.blocks.slice(0, 2))
    engine = engine.apply(canonical(b)).engine;
  const before = canonical(engine.snapshot());
  assert.throws(
    () => engine.apply(canonical(f.blocks[2])),
    /ERR_UNSUPPORTED_EPOCH/,
  );
  assert.equal(canonical(engine.snapshot()), before);
});
test("revoked suite remains forbidden for new and historical authorization", () => {
  const f = decode<{ registry: Registry; plan: Plan; expected_error: string }>(
    "revoked",
    read("revoked-suite"),
  );
  assert.throws(
    () => nextRegistry(f.registry, f.plan),
    new RegExp(f.expected_error),
  );
  for (const historic of [false, true])
    assert.throws(
      () =>
        authorize(
          f.registry,
          "2",
          "account.sign",
          "dev.account.ed25519.v1",
          historic,
        ),
      /ERR_SUITE/,
    );
});
test("registry binds epoch, operation, module and immutable identity", () => {
  const r = fixture("accounts").genesis.registry,
    id = "dev.account.ed25519.v1";
  authorize(r, "1", "account.sign", id);
  assert.throws(() => authorize(r, "2", "account.sign", id), /ERR_EPOCH/);
  assert.throws(() => authorize(r, "1", "ballot.prove", id), /ERR_SUITE/);
  assert.throws(
    () => authorize(r, "1", "account.sign", "unknown"),
    /ERR_SUITE/,
  );
  assert.throws(
    () =>
      authorize(r, "1", "eligibility.prove", "dev.unimplemented.eligibility"),
    /ERR_SUITE/,
  );
  r.modules.find((m) => m.id === id)!.status = "legacy_verify_only";
  assert.throws(() => authorize(r, "1", "account.sign", id), /ERR_SUITE/);
  authorize(r, "1", "account.sign", id, true);
  const a = fixture("activation").genesis.activations[0]!;
  a.plan.modules[0]!.implementation_sha256 = "00".repeat(32);
  assert.throws(
    () => nextRegistry(fixture("accounts").genesis.registry, a.plan),
    /immutable module/,
  );
});
test("plan and instance identifiers commit exact content", () => {
  const f = fixture("activation"),
    a = f.genesis.activations[0]!;
  checkActivation(a, f.genesis.network_id, f.genesis.chain_id);
  const p = structuredClone(a);
  p.plan.public_notice += "tampered";
  assert.throws(() =>
    checkActivation(p, f.genesis.network_id, f.genesis.chain_id),
  );
  a.instance.heights[0]!.height = "4";
  assert.throws(() =>
    checkActivation(a, f.genesis.network_id, f.genesis.chain_id),
  );
});
test("mempool checking does not consume nonce; committed retry fails", () => {
  const f = fixture("accounts");
  let e = new Engine(canonical(f.genesis));
  e = e.apply(canonical(f.blocks[0])).engine;
  const tx = canonical(f.blocks[1]!.transactions[0]);
  const before = e.root();
  e.checkTx(tx);
  e.checkTx(tx);
  assert.equal(e.root(), before);
  e = e.apply(canonical(f.blocks[1])).engine;
  assert.throws(() => e.checkTx(tx), /ERR_NONCE/);
});
test("block time is monotone and committed in state root", () => {
  const f = fixture("accounts");
  let e = new Engine(canonical(f.genesis));
  for (const b of f.blocks.slice(0, 3)) e = e.apply(canonical(b)).engine;
  assert.equal(e.time, 2000n);
  const a = e.clone();
  a.time++;
  assert.notEqual(a.root(), e.root());
});
test("retired commitments and keys cannot be reassigned", () => {
  const f = fixture("accounts");
  let engine = new Engine(canonical(f.genesis));
  for (const b of f.blocks) engine = engine.apply(canonical(b)).engine;
  const key = testKey("07".repeat(32));
  const original = f.blocks[0]!.transactions[0]!.body.payload as {
    signing_pk: string;
    recovery_pk: string;
    commitment: string;
  };
  const body: Body = {
    ...f.blocks[0]!.transactions[0]!.body,
    actor_id: domain("account", [f.genesis.network_id, key.publicKey]),
    payload: {
      signing_pk: key.publicKey,
      recovery_pk: "08".repeat(32),
      commitment: "5",
    },
  };
  const block = (tx: Transaction): Block => ({
    height: "5",
    time_ms: "5000",
    transactions: [tx],
  });
  assert.throws(
    () => engine.apply(canonical(block(signTransaction(body, key.seed)))),
    /ERR_COMMITMENT_USED/,
  );
  body.payload = {
    ...body.payload,
    commitment: "7",
    recovery_pk: original.signing_pk,
  } as typeof original;
  assert.throws(
    () => engine.apply(canonical(block(signTransaction(body, key.seed)))),
    /ERR_KEY_IN_USE/,
  );
});
test("tree root is independent of insertion order and binds business key", () => {
  const a = { table: "accounts", key: "a", value: { x: "a" } },
    b = { table: "accounts", key: "b", value: { x: "b" } };
  assert.equal(stateRoot([a, b]), stateRoot([b, a]));
  assert.notEqual(stateRoot([a]), stateRoot([b]));
  assert.throws(() => stateRoot([a, a]), /duplicate/);
});
test("checked-in development fixtures are reproducible", () => {
  const result = spawnSync(
    process.execPath,
    ["dist/scripts/generate-fixtures.js", "--check"],
    { encoding: "utf8" },
  );
  assert.equal(result.status, 0, result.stderr);
});

test("state hashing cannot fall back to an unauthorized or unknown suite", () => {
  const r = fixture("accounts").genesis.registry;
  r.modules[0]!.status = "revoked";
  assert.throws(() => validateRegistry(r), /ERR_SUITE/);
  r.modules[0]!.status = "active";
  r.modules[0]!.parameters.format = "unknown";
  assert.throws(() => validateRegistry(r), /ERR_UNSUPPORTED_HASH_SUITE/);
});
