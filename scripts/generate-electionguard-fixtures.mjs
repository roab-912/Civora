// Envelope vectors only: these contain no valid EG ballot, secret or tally.
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { canonical, domain, sha } from "../dist/web/src/protocol.js";
const file = (path, bytes = '{"TEST_ONLY":"NOT_AN_ELECTIONGUARD_RECORD"}') => ({
  path,
  bytes: Buffer.from(bytes).toString("base64url"),
});
const context = {
  network_id: "01".repeat(32),
  poll_id: "02".repeat(32),
  proposal_id: "03".repeat(32),
};
const link = {
  ...context,
  question_digest: domain("poll/question", Object.values(context)),
};
const make = (files) => ({ suite: "eg-2.0-civic-1", files });
const ballot = make([file("ballot.json")]);
const tally = make([file("tally.json")]);
const ceremony = make([
  // The outer Civic codec must preserve the external bytes exactly. This is
  // deliberately not Civic JCS, and is not an EG public context either.
  file("context.json", '{"n":5, "k":3}\n'),
  ...Array.from({ length: 5 }, (_, i) => file(`guardians/0${i + 1}.json`)),
  file("link.json", canonical(link)),
  file("manifest.json"),
]);
const b64 = (s) => Buffer.from(s).toString("base64url");
const vectors = [];
function add(name, kind, bundle, accept, checkLink = false) {
  const raw = typeof bundle === "string" ? bundle : canonical(bundle);
  const record = {
    name,
    kind,
    encoded: b64(raw),
    accept,
    check_link: checkLink,
  };
  if (accept) {
    const b = JSON.parse(raw);
    record.inspection = {
      suite: b.suite,
      kind,
      envelope_sha256: sha(Buffer.from(raw)).toString("hex"),
      files: b.files.map((f) => {
        const data = Buffer.from(f.bytes, "base64url");
        return {
          path: f.path,
          size: String(data.length),
          sha256: sha(data).toString("hex"),
        };
      }),
      cryptography_verified: false,
      suite_qualified: false,
    };
  }
  vectors.push(record);
}
add("ceremony-envelope-only", "ceremony", ceremony, true, true);
add("ballot-envelope-only", "ballot", ballot, true);
add("tally-envelope-only", "tally", tally, true);
const change = (value, fn) => {
  const copy = structuredClone(value);
  fn(copy);
  return copy;
};
for (const path of [
  "../ballot.json",
  "./ballot.json",
  "/ballot.json",
  "C:/ballot.json",
  "guardians/../ballot.json",
  "ballot\\json",
  "Ballot.json",
  "ballot.json/",
  "bállot.json",
  "ballot.json\u0000",
  "ballot.json?x",
  "ballot.json#x",
  "https://example.invalid/ballot.json",
  "guardians//01.json",
  "ballot%2Ejson",
])
  add("forbidden-path-" + vectors.length, "ballot", make([file(path)]), false);
add(
  "duplicate-path",
  "ballot",
  make([file("ballot.json"), file("ballot.json")]),
  false,
);
add("missing-file", "ballot", make([]), false);
add("wrong-kind", "tally", ballot, false);
add("unknown-kind", "unknown", ballot, false);
add("suite-2.1", "ballot", { ...ballot, suite: "eg-2.1-civic-1" }, false);
add("extra-envelope-field", "ballot", { ...ballot, nonce: "TEST_ONLY" }, false);
add(
  "extra-file-field",
  "ballot",
  make([{ ...file("ballot.json"), choice: "0" }]),
  false,
);
add(
  "secret-file",
  "ceremony",
  make([...ceremony.files, file("guardian.SECRET_key.json")]),
  false,
);
add(
  "missing-guardian",
  "ceremony",
  make(ceremony.files.filter((f) => f.path !== "guardians/05.json")),
  false,
);
add("unsorted-ceremony", "ceremony", make(ceremony.files.toReversed()), false);
add(
  "guardian-index-zero",
  "ceremony",
  change(ceremony, (b) => {
    b.files[1].path = "guardians/00.json";
  }),
  false,
);
for (const bytes of [
  "",
  "Zg==",
  "Zh",
  "Z",
  "Z+",
  "Z/",
  "Z g",
  "Zg\n",
  "é",
  null,
  1,
  [1],
])
  add(
    "invalid-inner-base64-" + vectors.length,
    "ballot",
    typeof bytes === "number" || Array.isArray(bytes)
      ? canonical(
          make([{ path: "ballot.json", bytes: "NUMBER_PLACEHOLDER" }]),
        ).replace('"NUMBER_PLACEHOLDER"', JSON.stringify(bytes))
      : make([{ path: "ballot.json", bytes }]),
    false,
  );
add(
  "noncanonical-whitespace",
  "ballot",
  JSON.stringify(ballot, null, 2),
  false,
);
add("noncanonical-trailing-LF", "ballot", canonical(ballot) + "\n", false);
add("noncanonical-BOM", "ballot", "\uFEFF" + canonical(ballot), false);
add(
  "duplicate-JSON-key",
  "ballot",
  canonical(ballot).replace('"suite":', '"suite":"eg-2.1-civic-1","suite":'),
  false,
);
add(
  "noncanonical-JSON-order",
  "ballot",
  JSON.stringify({ suite: ballot.suite, files: ballot.files }),
  false,
);
add("non-object", "ballot", "[]", false);
for (const field of Object.keys(link)) {
  const wrong = { ...link, [field]: "ff".repeat(32) };
  add(
    "link-mismatch-" + field,
    "ceremony",
    change(ceremony, (b) => {
      b.files[6] = file("link.json", canonical(wrong));
    }),
    false,
    true,
  );
}
for (const [name, raw] of [
  ["extra-field", canonical({ ...link, extra: "1" })],
  ["pretty", JSON.stringify(link, null, 2)],
  ["missing-field", canonical(context)],
  ["uppercase-id", canonical({ ...link, network_id: "AB".repeat(32) })],
])
  add(
    "link-" + name,
    "ceremony",
    change(ceremony, (b) => {
      b.files[6] = file("link.json", raw);
    }),
    false,
    true,
  );
const alphabet =
  "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
const canonicalOuter = b64(canonical(ballot));
const nonzeroPadBits =
  canonicalOuter.slice(0, -1) +
  alphabet[alphabet.indexOf(canonicalOuter.at(-1)) | 1];
if (
  nonzeroPadBits === canonicalOuter ||
  !Buffer.from(nonzeroPadBits, "base64url").equals(
    Buffer.from(canonical(ballot)),
  )
)
  throw Error(
    "outer padding alias fixture no longer exercises identical decoded bytes",
  );
for (const [name, encoded] of [
  ["padded-outer", b64(canonical(ballot)) + "="],
  ["empty-outer", ""],
  ["trailing-outer-whitespace", b64(canonical(ballot)) + "\n"],
  ["bad-outer-pad-bits", nonzeroPadBits],
])
  vectors.push({
    name,
    kind: "ballot",
    encoded,
    accept: false,
    check_link: false,
  });
const output = canonical({
  label: "TEST_ONLY_ENVELOPES_NOT_CRYPTOGRAPHIC_FIXTURES",
  context,
  vectors,
});
const outputs = {
  "envelopes.json": output,
  "ceremony.envelope.json": canonical(ceremony),
  "ballot.envelope.json": canonical(ballot),
  "tally.envelope.json": canonical(tally),
};
for (const [name, value] of Object.entries(outputs)) {
  const path = "spec/fixtures/v0.4/" + name;
  if (process.argv.includes("--check")) {
    if (readFileSync(path, "utf8") !== value)
      throw Error("stale ElectionGuard envelope fixture: " + path);
  } else {
    mkdirSync("spec/fixtures/v0.4", { recursive: true });
    writeFileSync(path, value);
  }
}
console.log(`${vectors.length} envelope vectors; no qualified crypto fixture`);
