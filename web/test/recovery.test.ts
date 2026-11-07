import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { canonical, decode } from "../src/protocol.js";
import { Client } from "../src/pwa/network.js";
import type { Config, Block, Transaction, Event } from "../src/protocol.js";
import { Engine } from "../src/core.js";
import { verifyPartitionCheckpoint } from "../src/citizen-audit.js";
import {
  verifyRecovery,
  sha3,
  type RecoveryBundle,
} from "../../services/qualification/recovery.js";
const root = readFileSync("spec/fixtures/v0.9/root.TEST_ONLY.json"),
  bundle = readFileSync("spec/fixtures/v0.9/hold.TEST_ONLY.json"),
  pin = sha3(root),
  now = 1793970000000n;
test("CIRCL ML-DSA-65 recovery order and mirrors verify independently with OpenSSL", () => {
  const order = verifyRecovery(root, pin, bundle, now, 0n, true);
  assert.equal(order.type, "security.hold");
  assert.throws(() => verifyRecovery(root, pin, bundle, now, 0n));
  assert.throws(() =>
    verifyRecovery(root, "0".repeat(96), bundle, now, 0n, true),
  );
  assert.throws(
    () => verifyRecovery(root, pin, bundle, now, 1n, true),
    /REPLAY/,
  );
  assert.throws(
    () => verifyRecovery(root, pin, bundle, now + 600000n, 0n, true),
    /FRESHNESS/,
  );
});
test("Go and OpenSSL transition oracle agree on on-chain holds and post-expiry safety", () => {
  const f = JSON.parse(
    readFileSync("spec/fixtures/v0.9/onchain-hold.TEST_ONLY.json", "utf8"),
  ) as {
    genesis: Config;
    blocks: Block[];
    expected_roots: string[];
    expected_events: Event[][];
    rejected_transaction: Transaction;
  };
  let engine = new Engine(canonical(f.genesis), 2n);
  for (const [i, b] of f.blocks.entries()) {
    const next = engine.apply(canonical(b));
    assert.equal(next.engine.root(), f.expected_roots[i]);
    assert.deepEqual(next.events, f.expected_events[i]);
    engine = next.engine;
  }
  assert.throws(
    () => engine.checkTx(canonical(f.rejected_transaction)),
    /SECURITY_MODULE_REVIEW/,
  );
  assert.throws(
    () =>
      engine.apply(canonical({ ...f.blocks[0], height: "3", time_ms: "4000" })),
    /RECOVERY_REPLAY/,
  );
});
test("independent checkpoint verifier refuses an authentically signed interrupted partition", () => {
  const f = JSON.parse(
    readFileSync(
      "spec/fixtures/v0.9/interrupted-checkpoint.TEST_ONLY.json",
      "utf8",
    ),
  );
  assert.throws(
    () => verifyPartitionCheckpoint(f.frozen, f.checkpoint),
    /SECURITY_INTERRUPTED/,
  );
});
test("PWA refuses writes under a recovery profile it cannot independently operate", async () => {
  const client = Object.create(Client.prototype) as Client;
  client.configuration = {
    path: {
      entry: {
        value: { config: { recovery: { root_sha3_384: "00".repeat(48) } } },
      },
    },
  } as any;
  assert.equal(client.canWrite(), false);
  await assert.rejects(
    () => client.post("/v1/transactions", "{}"),
    /SECURITY_READ_ONLY/,
  );
});
test("independent recovery verifier refuses forged, duplicate and insufficient authorities", () => {
  const mutations: ((b: RecoveryBundle) => void)[] = [
    (b) => {
      b.order.scope_id = "other";
    },
    (b) => {
      b.signatures.pop();
    },
    (b) => {
      b.signatures[9] = b.signatures[0]!;
    },
    (b) => {
      b.mirrors.pop();
    },
    (b) => {
      b.mirrors[2] = b.mirrors[0]!;
    },
    (b) => {
      b.order.expires_at_ms = String(now);
    },
    (b) => {
      b.order.network_id = "00".repeat(32);
    },
  ];
  for (const mutate of mutations) {
    const b = decode<RecoveryBundle>("recovery_bundle", bundle);
    mutate(b);
    assert.throws(() =>
      verifyRecovery(root, pin, Buffer.from(canonical(b)), now, 0n, true),
    );
  }
});
