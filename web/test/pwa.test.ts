import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync, readdirSync } from "node:fs";
import { Ajv2020 } from "ajv/dist/2020.js";
import {
  installValidators,
  domain,
  signDigest,
  publicKey,
  validate,
  canonical,
} from "../src/pwa/protocol.js";
import {
  newAccount,
  exportBackup,
  importBackup,
  credentials,
  type PublicAccount,
} from "../src/pwa/vault.js";
import {
  verifyCeremony,
  verifyBallot,
  power,
  generator,
  integer,
} from "../src/electionguard.js";
import { encryptVote } from "../src/pwa/encrypt.js";
const ajv = new Ajv2020({ strict: true });
for (const file of readdirSync("spec/schemas").filter((p) =>
  p.endsWith(".schema.json"),
))
  ajv.addSchema(JSON.parse(readFileSync("spec/schemas/" + file, "utf8")));
installValidators(
  Object.fromEntries(
    readdirSync("spec/schemas")
      .filter((p) => p.endsWith(".schema.json") && p !== "protocol.schema.json")
      .map((file) => [
        "v_" + file.replace(".schema.json", ""),
        ajv.getSchema(
          "https://civora.invalid/v0.1/" + file.replace(".schema.json", ""),
        )!,
      ]),
  ),
);
test("browser signing port reproduces the exact §13 vector", () => {
  const v = JSON.parse(readFileSync("spec/fixtures/vector13.json", "utf8"));
  assert.equal(
    domain("account", [v.network_id, publicKey(v.seed)]),
    v.account_id,
  );
  assert.equal(domain("tx", v.body), v.tx_id);
  assert.equal(signDigest(v.tx_id, v.seed), v.signature);
});
test("backup is portable, randomized, authenticated and network bound", async () => {
  const s = newAccount("12".repeat(32)),
    c = credentials(s, s.recovery_seed),
    a: PublicAccount = { ...c, status: "active", next_nonce: "0" };
  assert.notEqual(s.signing_seed, s.recovery_seed);
  const first = await exportBackup(s),
    second = await exportBackup(s);
  assert.notEqual(first.code, second.code);
  assert.notEqual(first.backup.iv, second.backup.iv);
  assert.notEqual(first.backup.salt, second.backup.salt);
  assert(!JSON.stringify(first.backup).includes(s.signing_seed));
  assert.deepEqual(
    await importBackup(canonical(first.backup), first.code, s.network_id, a),
    s,
  );
  await assert.rejects(
    () => importBackup(canonical(first.backup), first.code, "34".repeat(32), a),
    /NETWORK/,
  );
  await assert.rejects(() =>
    importBackup(canonical(first.backup), "00".repeat(32), s.network_id, a),
  );
  await assert.rejects(() =>
    importBackup(canonical(first.backup), first.code, s.network_id, {
      ...a,
      signing_pk: "00".repeat(32),
    }),
  );
  const modified = structuredClone(first.backup);
  modified.header.account_id = "00".repeat(32);
  await assert.rejects(() =>
    importBackup(canonical(modified), first.code, s.network_id, a),
  );
  assert.throws(() =>
    validate("pwa_backup", { ...first.backup, secret: s.recovery_seed }),
  );
});
test("browser produces fresh real binary ElectionGuard ciphertexts for Yes and No", async () => {
  const f = JSON.parse(readFileSync("spec/fixtures/v0.4/crypto.json", "utf8"))
      .cases[0],
    e = await verifyCeremony(f.context, f.ceremony_bundle);
  for (const choice of [0, 1] as const) {
    const encrypted = await encryptVote(e, choice),
      b = await verifyBallot(e, encrypted.encoded);
    assert.equal(b.ciphertext.alpha, encrypted.alpha);
    const nonce = integer(encrypted.nonce);
    assert.equal(
      power(generator, nonce),
      BigInt(
        "0x" + Buffer.from(b.ciphertext.alpha, "base64url").toString("hex"),
      ),
    );
    encrypted.nonce.fill(0);
    assert(!new TextDecoder().decode(b.raw).includes("selection_nonce"));
    assert.equal((b.ballot.contests as unknown[]).length, 1);
  }
  const a = await encryptVote(e, 1),
    b = await encryptVote(e, 1);
  assert.notEqual(a.encoded, b.encoded);
  a.nonce.fill(0);
  b.nonce.fill(0);
});
