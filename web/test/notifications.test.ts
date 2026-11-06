import assert from "node:assert/strict";
import test from "node:test";
import {
  createDecipheriv,
  createCipheriv,
  createECDH,
  createPublicKey,
  verify,
  webcrypto,
} from "node:crypto";
import { readFileSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { canonical, domain, validate } from "../src/protocol.js";
import {
  encryptionKeys,
  encryptPush,
  vapidPublic,
  vapidAuthorization,
  encodeP256Private,
} from "../../services/notifications/crypto.js";
import {
  endpointURL,
  publicAddress,
  resolvePublic,
  pushTopic,
} from "../../services/notifications/transport.js";
import { NotificationStore } from "../../services/notifications/store.js";
import { NotificationService } from "../../services/notifications/service.js";
import { createServer } from "node:http";
import { verifySourceProof } from "../../services/notifications/source.js";
import type {
  News,
  Subscription,
  Sender,
} from "../../services/notifications/types.js";
const fixture = JSON.parse(
    readFileSync("spec/fixtures/v0.8/webpush-rfc8291.TEST_ONLY.json", "utf8"),
  ),
  b = (s: string) => Buffer.from(s, "base64url"),
  network = "11".repeat(32),
  poll = "22".repeat(32),
  sub: Subscription = {
    v: "1",
    subscription_id: "33".repeat(32),
    unsubscribe_secret: "44".repeat(32),
    endpoint: "https://push.example.test/public-test-endpoint",
    keys: { p256dh: fixture.receiver_public, auth: fixture.auth },
    networks: [network],
    enabled: true,
  },
  news: News = {
    v: "1",
    type: "poll.opened",
    network_id: network,
    poll_id: poll,
    event_id: domain("push/poll.opened", [network, poll]),
    proposal_id: "55".repeat(32),
    opened_at_ms: "1000",
    closes_at_ms: "100000",
    source_height: "4",
  };
const store = () => new NotificationStore(":memory:", [network]);
test("Web Push reproduces the RFC8291 published key, nonce and ciphertext", () => {
  const k = encryptionKeys(
    b(fixture.receiver_public),
    b(fixture.auth),
    b(fixture.sender_private_TEST_ONLY),
    b(fixture.salt),
  );
  assert.equal(k.key.toString("base64url"), fixture.key);
  assert.equal(k.nonce.toString("base64url"), fixture.nonce);
  assert.equal(k.publicKey.toString("base64url"), fixture.sender_public);
  const cipher = createCipheriv("aes-128-gcm", k.key, k.nonce),
    out = Buffer.concat([
      cipher.update(Buffer.concat([b(fixture.plaintext), Buffer.from([2])])),
      cipher.final(),
      cipher.getAuthTag(),
    ]);
  assert.equal(out.toString("base64url"), fixture.ciphertext);
});

test("notification source refuses tampered, foreign and unfinalised state proofs", () => {
  const record = JSON.parse(
      readFileSync("spec/fixtures/v0.6/record/record.json", "utf8"),
    ),
    g = JSON.parse(
      readFileSync(
        "spec/fixtures/v0.6/record/objects/" +
          record.genesis.sha256.slice(0, 2) +
          "/" +
          record.genesis.sha256,
        "utf8",
      ),
    ),
    p = {
      ...record.opening,
      path: {
        entry: record.opening.path.entry,
        exists: true,
        siblings: [...record.opening.path.siblings].reverse(),
      },
    };
  verifySourceProof(g, p, "transport_polls", p.path.entry.key, p.height);
  assert.throws(() =>
    verifySourceProof(g, p, "proposals", p.path.entry.key, p.height),
  );
  assert.throws(() =>
    verifySourceProof(g, p, "transport_polls", "00".repeat(32), p.height),
  );
  assert.throws(() =>
    verifySourceProof(
      g,
      p,
      "transport_polls",
      p.path.entry.key,
      String(BigInt(p.height) + 1n),
    ),
  );
  const bad = structuredClone(p);
  bad.path.entry.value.closes_at_ms = "1";
  assert.throws(() =>
    verifySourceProof(g, bad, "transport_polls", p.path.entry.key, p.height),
  );
  const foreign = structuredClone(g);
  foreign.chain_id = "foreign";
  assert.throws(() =>
    verifySourceProof(
      foreign,
      p,
      "transport_polls",
      p.path.entry.key,
      p.height,
    ),
  );
});
test("fresh Web Push aes128gcm frames decrypt independently with WebCrypto", async () => {
  const raw = Buffer.from(canonical({ generic: "question" })),
    first = encryptPush(raw, sub.keys),
    second = encryptPush(raw, sub.keys);
  assert.notDeepEqual(first, second);
  assert.equal(first.readUInt32BE(16), 4096);
  assert.equal(first[20], 65);
  const server = b(fixture.receiver_private_TEST_ONLY),
    receiver = createECDH("prime256v1");
  receiver.setPrivateKey(server);
  const crypto = webcrypto.subtle,
    auth = await crypto.importKey(
      "raw",
      b(fixture.auth),
      { name: "HKDF" },
      false,
      ["deriveBits"],
    ),
    // HKDF's initial key material is the ECDH shared secret; the auth secret is its salt.
    shared = receiver.computeSecret(first.subarray(21, 86)),
    base = await crypto.importKey("raw", shared, "HKDF", false, ["deriveBits"]),
    ikm = await crypto.deriveBits(
      {
        name: "HKDF",
        hash: "SHA-256",
        salt: b(fixture.auth),
        info: Buffer.concat([
          Buffer.from("WebPush: info\0"),
          receiver.getPublicKey(),
          first.subarray(21, 86),
        ]),
      },
      base,
      256,
    ),
    content = await crypto.importKey("raw", ikm, "HKDF", false, ["deriveBits"]),
    key = await crypto.deriveBits(
      {
        name: "HKDF",
        hash: "SHA-256",
        salt: first.subarray(0, 16),
        info: Buffer.from("Content-Encoding: aes128gcm\0"),
      },
      content,
      128,
    ),
    nonce = await crypto.deriveBits(
      {
        name: "HKDF",
        hash: "SHA-256",
        salt: first.subarray(0, 16),
        info: Buffer.from("Content-Encoding: nonce\0"),
      },
      content,
      96,
    ),
    aes = await crypto.importKey("raw", key, "AES-GCM", false, ["decrypt"]),
    plain = new Uint8Array(
      await crypto.decrypt(
        { name: "AES-GCM", iv: nonce },
        aes,
        first.subarray(86),
      ),
    );
  assert.deepEqual(Buffer.from(plain.subarray(0, -1)), raw);
  assert.equal(plain.at(-1), 2);
  assert(auth);
  const corrupted = Buffer.from(first);
  corrupted[corrupted.length - 1] = corrupted[corrupted.length - 1]! ^ 1;
  await assert.rejects(() =>
    crypto.decrypt({ name: "AES-GCM", iv: nonce }, aes, corrupted.subarray(86)),
  );
});
test("VAPID ES256 binds audience, contact, expiry and a key distinct from message ECDH", () => {
  const secret = "AQ".repeat(0) + Buffer.alloc(32, 1).toString("base64url"),
    authorization = vapidAuthorization(
      secret,
      "https://push.example.test/path",
      "mailto:operator@example.test",
      5000000,
    ),
    match = /^vapid t=([^,]+), k=(.+)$/.exec(authorization)!;
  const [header, claims, sig] = match[1]!.split("."),
    p = b(match[2]!);
  assert.equal(match[2], vapidPublic(secret));
  assert.deepEqual(JSON.parse(b(header!).toString()), {
    typ: "JWT",
    alg: "ES256",
  });
  const c = JSON.parse(b(claims!).toString());
  assert.equal(c.aud, "https://push.example.test");
  assert(c.exp > 5000 && c.exp <= 5000 + 86400);
  const key = createPublicKey({
    key: {
      kty: "EC",
      crv: "P-256",
      x: p.subarray(1, 33).toString("base64url"),
      y: p.subarray(33).toString("base64url"),
    },
    format: "jwk",
  });
  assert(
    verify(
      "sha256",
      Buffer.from(header + "." + claims),
      { key, dsaEncoding: "ieee-p1363" },
      b(sig!),
    ),
  );
});

test("VAPID private key encoding preserves leading zeroes in native short scalars", () => {
  const key = createECDH("prime256v1");
  key.setPrivateKey(Buffer.from([1]));
  const encoded = encodeP256Private(key.getPrivateKey());
  assert.equal(b(encoded).length, 32);
  assert.equal(b(encoded).at(-1), 1);
  assert.equal(vapidPublic(encoded), key.getPublicKey().toString("base64url"));
});
test("Push destinations reject internal, mapped, link-local and reserved addresses", async () => {
  for (const address of [
    "127.0.0.1",
    "10.0.0.1",
    "172.16.1.1",
    "192.168.1.40",
    "169.254.169.254",
    "100.64.0.1",
    "0.0.0.0",
    "203.0.113.1",
    "::1",
    "::ffff:127.0.0.1",
    "fc00::1",
    "fe80::1",
    "64:ff9b::7f00:1",
    "2002:7f00:1::",
  ])
    assert.equal(publicAddress(address), false, address);
  for (const url of [
    "http://example.org/x",
    "https://user:password@example.org/x",
    "https://127.0.0.1/x",
    "https://localhost/x",
    "https://example.org:8443/x",
    "https://example.org/x#secret",
  ])
    assert.throws(() => endpointURL(url));
  assert(publicAddress("8.8.8.8"));
  assert(publicAddress("2606:4700:4700::1111"));
  await assert.rejects(
    () =>
      resolvePublic(
        "https://public.example.test",
        async () =>
          [
            { address: "8.8.8.8", family: 4 },
            { address: "127.0.0.1", family: 4 },
          ] as any,
      ),
    /DESTINATION/,
  );
});
test("subscription schema rejects account identifiers and ballot metadata", () => {
  validate("push_subscription", sub);
  for (const field of ["account_id", "ballot", "membership", "voted", "choice"])
    assert.throws(() =>
      validate("push_subscription", { ...sub, [field]: "private" }),
    );
});

test("provider replacement topic is stable for retries and distinct for each recipient", () => {
  const a = pushTopic(sub.subscription_id, news.event_id),
    b = pushTopic("ee".repeat(32), news.event_id);
  assert.equal(a.length, 32);
  assert.match(a, /^[A-Za-z0-9_-]+$/);
  assert.equal(a, pushTopic(sub.subscription_id, news.event_id));
  assert.notEqual(a, b);
  assert.notEqual(a, news.event_id.slice(0, 32));
});
test("news and one task per subscription/event commit atomically and replay idempotently", async () => {
  const s = store();
  try {
    s.subscribe(sub);
    assert.throws(() =>
      s.project(network, "4:0", [news], 1000, () => {
        throw Error("crash");
      }),
    );
    assert.equal(s.cursor(network), "");
    assert.equal(s.news(network, "").items.length, 0);
    s.project(network, "4:0", [news], 1000);
    s.project(network, "4:0", [news], 1000);
    assert.equal(s.news(network, "").items.length, 1);
    assert.equal(s.db.prepare("SELECT count(*) n FROM jobs").get()!.n, 1);
    const row = s.db.prepare("SELECT value FROM subscriptions").get() as {
      value: string;
    };
    assert(!row.value.includes(sub.unsubscribe_secret));
    assert(!row.value.includes("unsubscribe_secret"));
    let attempts = 0;
    const sender: Sender = {
      async send(_s, p) {
        attempts++;
        assert(!("account_id" in p));
        return { status: 201 };
      },
    };
    await s.deliver(sender, 1000);
    await s.deliver(sender, 2000);
    assert.equal(attempts, 1);
  } finally {
    s.close();
  }
});
test("denial or no subscription still produces public inbox news", () => {
  const s = store();
  try {
    s.project(network, "4:0", [news], 1000);
    assert.equal(s.news(network, "").items.length, 1);
    assert.equal(s.db.prepare("SELECT count(*) n FROM jobs").get()!.n, 0);
  } finally {
    s.close();
  }
});
test("temporary failures back off, respect Retry-After and expire at close", async () => {
  const s = store();
  try {
    s.subscribe(sub);
    s.project(network, "4:0", [news], 1000);
    let count = 0;
    const sender: Sender = {
      async send() {
        count++;
        return { status: 503, retryAfter: 10 };
      },
    };
    await s.deliver(sender, 1000);
    await s.deliver(sender, 9999);
    assert.equal(count, 1);
    await s.deliver(sender, 11000);
    assert.equal(count, 2);
    await s.deliver(sender, 100000);
    assert.equal(count, 2);
    assert.equal(
      s.db.prepare("SELECT state FROM jobs").get()!.state,
      "expired",
    );
  } finally {
    s.close();
  }
});
test("permanent invalid subscription is erased; redirects are never followed or retried", async () => {
  for (const status of [404, 410, 302]) {
    const s = store();
    try {
      s.subscribe(sub);
      s.project(network, "4:0", [news], 1000);
      await s.deliver(
        {
          async send() {
            return { status };
          },
        },
        1000,
      );
      assert.equal(
        s.db.prepare("SELECT count(*) n FROM subscriptions").get()!.n,
        status === 302 ? 1 : 0,
      );
      if (status === 302)
        assert.equal(
          s.db.prepare("SELECT state FROM jobs").get()!.state,
          "rejected",
        );
    } finally {
      s.close();
    }
  }
});
test("unsubscribe requires its capability, erases endpoint and cancels all jobs", () => {
  const s = store();
  try {
    s.subscribe(sub);
    s.project(network, "4:0", [news], 1000);
    assert.throws(
      () => s.unsubscribe(sub.subscription_id, "00".repeat(32)),
      /AUTH/,
    );
    s.unsubscribe(sub.subscription_id, sub.unsubscribe_secret);
    s.unsubscribe(sub.subscription_id, sub.unsubscribe_secret);
    assert.equal(
      s.db.prepare("SELECT count(*) n FROM subscriptions").get()!.n,
      0,
    );
    assert.equal(s.db.prepare("SELECT count(*) n FROM jobs").get()!.n, 0);
    assert.equal(s.news(network, "").items.length, 1);
    assert.throws(() => s.subscribe(sub), /RETIRED/);
  } finally {
    s.close();
  }
});
test("network preferences cancel pending alerts without suppressing inbox", async () => {
  const s = store();
  try {
    s.subscribe(sub);
    s.project(network, "4:0", [news], 1000);
    s.subscribe(sub);
    assert.equal(
      s.db.prepare("SELECT state FROM jobs").get()!.state,
      "pending",
    );
    s.subscribe({ ...sub, enabled: false });
    let sent = 0;
    await s.deliver(
      {
        async send() {
          sent++;
          return { status: 201 };
        },
      },
      2000,
    );
    assert.equal(sent, 0);
    assert.equal(s.news(network, "").items.length, 1);
  } finally {
    s.close();
  }
});
test("SQLite abrupt exit before/after commit preserves atomic projection and restart", () => {
  for (const before of [true, false]) {
    const directory = mkdtempSync(join(tmpdir(), "civora-push-")),
      path = join(directory, "notifications.db");
    const module = resolve("dist/services/notifications/store.js").replaceAll(
      "\\",
      "/",
    );
    const code = `import {NotificationStore}from ${JSON.stringify("file:///" + module)};const s=new NotificationStore(${JSON.stringify(path)},[${JSON.stringify(network)}]);s.subscribe(${JSON.stringify(sub)});s.project(${JSON.stringify(network)},'4:0',[${JSON.stringify(news)}],1000${before ? ",()=>process.kill(process.pid,'SIGKILL')" : ""});process.kill(process.pid,'SIGKILL');`;
    const child = spawnSync(
      process.execPath,
      ["--input-type=module", "-e", code],
      { windowsHide: true, encoding: "utf8" },
    );
    assert.notEqual(child.status, 0, child.stderr);
    const s = new NotificationStore(path, [network]);
    try {
      assert.equal(s.news(network, "").items.length, before ? 0 : 1);
      assert.equal(s.cursor(network), before ? "" : "4:0");
      s.project(network, "4:0", [news], 1000);
      assert.equal(s.db.prepare("SELECT count(*) n FROM jobs").get()!.n, 1);
    } finally {
      s.close();
    }
  }
});

test("authenticated consensus time prevents an alert after close and refuses rollback", async () => {
  const s = store();
  try {
    s.subscribe(sub);
    s.project(network, "4:0", [news], 1000);
    s.observeClock(network, "5", "100000");
    assert.throws(() => s.observeClock(network, "4", "200000"), /ROLLBACK/);
    assert.throws(() => s.observeClock(network, "6", "90000"), /ROLLBACK/);
    let sent = 0;
    await s.deliver(
      {
        async send() {
          sent++;
          return { status: 201 };
        },
      },
      2000,
    );
    assert.equal(sent, 0);
    assert.equal(
      s.db.prepare("SELECT state FROM jobs").get()!.state,
      "expired",
    );
  } finally {
    s.close();
  }
});
test("expiry discovered during dispatch expires the task without deleting the subscription", async () => {
  const s = store();
  try {
    s.subscribe(sub);
    s.project(network, "4:0", [news], 1000);
    await s.deliver(
      {
        async send() {
          return { status: 0, expired: true };
        },
      },
      1000,
    );
    assert.equal(
      s.db.prepare("SELECT state FROM jobs").get()!.state,
      "expired",
    );
    assert.equal(
      s.db.prepare("SELECT count(*) n FROM subscriptions").get()!.n,
      1,
    );
  } finally {
    s.close();
  }
});
test("inbox reads remain available while the Push provider is blocked", async () => {
  const manifest = JSON.parse(
      readFileSync("spec/fixtures/v0.6/network.json", "utf8"),
    ),
    id = manifest.network_id as string,
    event = {
      ...news,
      network_id: id,
      event_id: domain("push/poll.opened", [id, poll]),
    },
    directory = mkdtempSync(join(tmpdir(), "civora-news-"));
  let release: () => void = () => {},
    started = false;
  const blocked = new Promise<void>((yes) => {
    release = yes;
  });
  const service = new NotificationService({
    directory,
    watches: [{ name: "test", manifest, api_roots: [] }],
    clock: () => 1000,
    sender: {
      async send() {
        started = true;
        await blocked;
        return { status: 201 };
      },
    },
    sources: [
      {
        watch: { manifest: { network_id: id } },
        async sync(store, now) {
          store.project(id, "4:0", [event], now);
          return 1;
        },
      },
    ],
  });
  const server = createServer(async (req, res) => {
    await service.handle(
      req,
      res,
      new URL(req.url!, "http://localhost").pathname,
    );
  });
  try {
    service.store.subscribe({ ...sub, networks: [id] });
    await service.tick();
    await Promise.resolve();
    assert(started);
    await new Promise<void>((yes) => server.listen(0, "127.0.0.1", yes));
    const port = (server.address() as { port: number }).port;
    const response = await fetch(
      `http://127.0.0.1:${port}/notifications/v1/news?network_id=${id}`,
      { signal: AbortSignal.timeout(2000) },
    );
    assert.equal(response.status, 200);
    assert.equal(
      ((await response.json()) as { items: unknown[] }).items.length,
      1,
    );
  } finally {
    release();
    server.closeAllConnections();
    server.close();
    await service.close();
  }
});

test("notification journal refuses a different genesis and a cursor rollback", () => {
  const s = store();
  try {
    s.pin(network, "ab".repeat(32));
    s.pin(network, "ab".repeat(32));
    assert.throws(() => s.pin(network, "cd".repeat(32)), /GENESIS_REPLACED/);
    s.project(network, "4:0", [news], 1000);
    assert.throws(() => s.project(network, "3:9", [], 2000), /ROLLBACK/);
    assert.equal(s.cursor(network), "4:0");
    assert.equal(s.news(network, "").items.length, 1);
  } finally {
    s.close();
  }
});
