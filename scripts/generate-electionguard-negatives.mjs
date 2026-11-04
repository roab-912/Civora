import { readFileSync, writeFileSync } from "node:fs";
import { canonical } from "../dist/web/src/protocol.js";
const vectors = JSON.parse(
    readFileSync("spec/fixtures/v0.4/crypto.json", "utf8"),
  ),
  base = vectors.cases[0],
  cases = [];
const flip = (s) => {
  const b = Buffer.from(s, "base64url");
  b[b.length - 1] ^= 1;
  return b.toString("base64url");
};
function mutate(encoded, path, fn) {
  const b = JSON.parse(Buffer.from(encoded, "base64url").toString());
  const f = b.files.find((f) => f.path === path);
  const value = JSON.parse(Buffer.from(f.bytes, "base64url").toString());
  fn(value);
  f.bytes = Buffer.from(canonical(value)).toString("base64url");
  return Buffer.from(canonical(b)).toString("base64url");
}
function add(name, stage, path, fn) {
  const encoded =
    stage === "ceremony"
      ? base.ceremony_bundle
      : stage === "ballot"
        ? base.ballots[0]
        : base.tally_bundle;
  cases.push({ name, stage, encoded: mutate(encoded, path, fn) });
}
for (const [name, fn] of [
  [
    "response",
    (g) =>
      (g.coefficient_proofs[0].response = flip(
        g.coefficient_proofs[0].response,
      )),
  ],
  ["index", (g) => (g.sequence_order = "2")],
  ["account", (g) => (g.account_id = "ff".repeat(32))],
  ["missing-coefficient", (g) => g.coefficient_proofs.pop()],
  ["secret-field", (g) => (g.secret_key = "TEST_ONLY_REJECT")],
])
  add("guardian-" + name, "ceremony", "guardians/01.json", fn);
for (const field of [
  "joint_public_key",
  "extended_base_hash",
  "parameter_base_hash",
  "manifest_hash",
])
  add(
    "context-" + field,
    "ceremony",
    "context.json",
    (c) => (c[field] = flip(c[field])),
  );
add("threshold", "ceremony", "context.json", (c) => (c.quorum = "2"));
const selection = (b) => b.contests[0].selections[0];
for (const [name, fn] of [
  [
    "selection-proof",
    (b) =>
      (selection(b).proof[0].response = flip(selection(b).proof[0].response)),
  ],
  [
    "contest-proof",
    (b) =>
      (b.contests[0].proof[0].response = flip(b.contests[0].proof[0].response)),
  ],
  [
    "ciphertext",
    (b) =>
      (selection(b).encrypted_vote.beta = flip(
        selection(b).encrypted_vote.beta,
      )),
  ],
  ["missing-selection", (b) => (b.contests[0].selections = [])],
  [
    "extra-selection",
    (b) => b.contests[0].selections.push(structuredClone(selection(b))),
  ],
  ["challenge", (b) => (b.state = "CHALLENGED")],
  ["plaintext", (b) => (b.choice = "0")],
  ["opening-nonce", (b) => (b.primary_nonce = "TEST_ONLY_REJECT")],
  ["device-identifier", (b) => (b.encrypting_device = "civic:account-device")],
  ["timestamp", (b) => (b.timestamp = "12345")],
  ["confirmation", (b) => (b.confirmation_code = flip(b.confirmation_code))],
  ["election-id", (b) => (b.election_id = flip(b.election_id))],
  [
    "zero-alpha",
    (b) =>
      (selection(b).encrypted_vote.alpha =
        Buffer.alloc(512).toString("base64url")),
  ],
  [
    "identity-alpha",
    (b) => {
      const p = Buffer.alloc(512);
      p[511] = 1;
      selection(b).encrypted_vote.alpha = p.toString("base64url");
    },
  ],
  ["padding", (b) => (selection(b).proof[0].challenge += "=")],
  [
    "contest-data-length",
    (b) => (b.contests[0].encrypted_contest_data.c1 = "Zg"),
  ],
])
  add(name, "ballot", "ballot.json", fn);
for (const [name, fn] of [
  ["false-total", (t) => (t.tally = "4")],
  ["aggregate-altered", (t) => (t.ciphertext.beta = flip(t.ciphertext.beta))],
  ["decryption-proof", (t) => (t.proof.response = flip(t.proof.response))],
  ["only-two-guardians", (t) => t.shares.pop()],
  [
    "duplicate-guardian",
    (t) => (t.shares[1].guardian_index = t.shares[0].guardian_index),
  ],
  ["unsorted-guardians", (t) => t.shares.reverse()],
  [
    "share-response",
    (t) => (t.shares[0].response = flip(t.shares[0].response)),
  ],
  ["share-altered", (t) => (t.shares[0].share = flip(t.shares[0].share))],
  ["foreign-poll", (t) => (t.id = "ff".repeat(32))],
  ["individual-decryption", (t) => (t.ballot_decryptions = [])],
])
  add(name, "tally", "tally.json", fn);
cases.push({
  name: "foreign-aggregate",
  stage: "tally",
  encoded: base.tally_bundle,
  aggregate: vectors.cases[1].aggregate,
});
cases.push({
  name: "foreign-ballot",
  stage: "ballot",
  encoded: vectors.cases[1].ballots[0],
});
const data = canonical({ base_label: base.label, cases }),
  path = "spec/fixtures/v0.4/reject-crypto.json";
if (process.argv.includes("--check")) {
  if (readFileSync(path, "utf8") !== data)
    throw Error("stale EG negative vectors");
} else writeFileSync(path, data);
