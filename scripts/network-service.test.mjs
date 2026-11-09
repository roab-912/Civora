import test from "node:test";
import assert from "node:assert/strict";
import { PresenceRegistry, localOrigin } from "./network-service.mjs";
import { domain, signDigest, testKey } from "../dist/web/src/protocol.js";
const publicKey = (seed) => testKey(seed).publicKey;

// Public development keys, solely for testing the presence protocol.
const seed = "42".repeat(32),
  secondSeed = "43".repeat(32);
const network = "11".repeat(32),
  block = "22".repeat(32);
function report(
  registry,
  { key = seed, height = "3", action = "online", ...override } = {},
) {
  const node = publicKey(key);
  const body = {
    format: "civora-presence-1",
    network_id: network,
    node_id: node,
    height,
    block_sha256: block,
    challenge: registry.challenge(node).challenge,
    action,
    ...override,
  };
  return {
    body,
    signature: signDigest(
      domain("development/participant-presence", body),
      key,
    ),
  };
}

test("presence is signed, deduplicated per identity and expires without counting browsers", async () => {
  let now = 1000;
  const registry = new PresenceRegistry(network, { now: () => now });
  const verify = async (body) => assert.equal(body.block_sha256, block);
  assert.equal(registry.count(), 0);
  await registry.announce(report(registry), verify);
  await registry.announce(report(registry), verify);
  assert.equal(registry.count(), 1);
  await registry.announce(report(registry, { key: secondSeed }), verify);
  assert.equal(registry.count(), 2);
  await registry.announce(report(registry, { action: "offline" }), verify);
  assert.equal(registry.count(), 1);
  now += 45000;
  assert.equal(registry.count(), 0);
});

test("presence refuses replayed, expired and foreign challenges, signature tampering and unknown fields", async () => {
  let now = 1000;
  const registry = new PresenceRegistry(network, { now: () => now });
  const valid = report(registry);
  await registry.announce(valid, async () => {});
  await assert.rejects(
    registry.announce(valid, async () => {}),
    /CHALLENGE/,
  );
  const expired = report(registry);
  now += 30000;
  await assert.rejects(
    registry.announce(expired, async () => {}),
    /CHALLENGE/,
  );
  await assert.rejects(
    registry.announce(
      report(registry, { network_id: "ff".repeat(32) }),
      async () => {},
    ),
    /CHALLENGE/,
  );
  const tampered = report(registry);
  tampered.body.height = "4";
  await assert.rejects(
    registry.announce(tampered, async () => {}),
    /SIGNATURE/,
  );
  await assert.rejects(
    registry.announce({ ...report(registry), extra: true }, async () => {}),
    /FORMAT/,
  );
  await assert.rejects(
    registry.announce(
      report(registry, { height: "18446744073709551616" }),
      async () => {},
    ),
    /HEIGHT/,
  );
});

test("presence requires a matching block and never lowers a live node's verified height", async () => {
  const registry = new PresenceRegistry(network);
  await assert.rejects(
    registry.announce(report(registry), async () => {
      throw Error("ERR_PRESENCE_BLOCK");
    }),
    /BLOCK/,
  );
  assert.equal(registry.count(), 0);
  await registry.announce(report(registry, { height: "8" }), async () => {});
  await assert.rejects(
    registry.announce(report(registry, { height: "7" }), async () => {}),
    /ROLLBACK/,
  );
  assert.equal(registry.count(), 1);
});

test("presence capacity bounds both pending challenges and identities", async () => {
  let now = 0;
  const registry = new PresenceRegistry(network, {
    capacity: 1,
    now: () => now,
  });
  const first = report(registry);
  assert.throws(() => registry.challenge(publicKey(secondSeed)), /CAPACITY/);
  await registry.announce(first, async () => {});
  await assert.rejects(
    registry.announce(report(registry, { key: secondSeed }), async () => {}),
    /CAPACITY/,
  );
  now = 45001;
  await registry.announce(
    report(registry, { key: secondSeed }),
    async () => {},
  );
  assert.equal(registry.count(), 1);
});

test("download bootstrap origins are restricted to the local experimental network", () => {
  assert.equal(localOrigin("http://127.0.0.1:8790"), "http://127.0.0.1:8790");
  assert.equal(
    localOrigin("https://192.168.1.40:8791"),
    "https://192.168.1.40:8791",
  );
  for (const origin of [
    "https://example.org",
    "http://user:pass@127.0.0.1",
    "http://127.0.0.1/evil",
    "http://127.0.0.1/?q=1",
    "ftp://127.0.0.1",
    "https://192.168.1.40/#x",
  ])
    assert.throws(() => localOrigin(origin));
});
