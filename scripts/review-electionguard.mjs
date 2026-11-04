// Source integrity is distinct from cryptographic qualification. This script
// cannot activate a suite by downloading its documentation successfully.
import { createHash } from "node:crypto";
import {
  mkdirSync,
  existsSync,
  readFileSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { resolve } from "node:path";
import "./electionguard-lock.mjs";
const review = JSON.parse(
  readFileSync("spec/crypto/electionguard-v2.review.json", "utf8"),
);
const args = process.argv.slice(2);
if (
  args.some(
    (a) => !["--fetch", "--check-local", "--require-qualified"].includes(a),
  )
)
  throw Error("unknown review option");
if (
  review.suite !== "eg-2.0-civic-1" ||
  review.design_version !== "2.0.0" ||
  review.serialization_version !== "0.0.1" ||
  review.qualified !== false ||
  review.status !== "development_interop_verified" ||
  review.development_interop_verified !== true ||
  review.review_scope !==
    "sdk_execution_and_independent_go_typescript_development_vectors" ||
  !/^[0-9a-f]{40}$/.test(review.candidate_commit) ||
  review.artifacts.length !== 5 ||
  review.blockers.length !== 0
)
  throw Error(
    "unexpected ElectionGuard review; qualification requires an explicit reviewed implementation change",
  );
const directory = resolve(".tools/electionguard/review");
const fetchSources = args.includes("--fetch");
const checkSources = fetchSources || args.includes("--check-local");
if (fetchSources) mkdirSync(directory, { recursive: true });
const names = new Set();
for (const a of review.artifacts) {
  const url = new URL(a.url);
  if (
    !/^[a-zA-Z0-9_.-]+$/.test(a.file) ||
    [".", ".."].includes(a.file) ||
    names.has(a.file) ||
    !["github.com", "raw.githubusercontent.com"].includes(url.hostname) ||
    url.protocol !== "https:" ||
    !/^[1-9][0-9]{0,6}$/.test(a.size) ||
    Number(a.size) > 1000000 ||
    !/^[0-9a-f]{64}$/.test(a.sha256)
  )
    throw Error("invalid source pin");
  names.add(a.file);
  const path = resolve(directory, a.file);
  const verify = (bytes) => {
    if (
      bytes.length !== Number(a.size) ||
      createHash("sha256").update(bytes).digest("hex") !== a.sha256
    )
      throw Error("ElectionGuard source integrity failure: " + a.file);
  };
  if (fetchSources && !existsSync(path)) {
    const response = await fetch(url, { signal: AbortSignal.timeout(60000) });
    if (!response.ok || !response.body)
      throw Error(`HTTP ${response.status}: ${a.file}`);
    const chunks = [];
    let size = 0;
    for await (const chunk of response.body) {
      size += chunk.length;
      if (size > Number(a.size))
        throw Error("oversized ElectionGuard source: " + a.file);
      chunks.push(chunk);
    }
    const bytes = Buffer.concat(chunks);
    verify(bytes);
    writeFileSync(path, bytes, { flag: "wx" });
  }
  if (checkSources) {
    if (statSync(path).size !== Number(a.size))
      throw Error("wrong source size: " + a.file);
    verify(readFileSync(path));
  }
}
if (args.includes("--require-qualified")) {
  const {
    verifyCeremony,
    verifyBallot,
    verifyTally,
    zeroAggregate,
    addAggregate,
  } = await import("../dist/web/src/electionguard.js");
  const { verifyScale } = await import(
    "../dist/web/src/electionguard-scale.js"
  );
  try {
    const vectors = JSON.parse(
      readFileSync("spec/fixtures/v0.4/crypto.json", "utf8"),
    );
    for (const c of vectors.cases) {
      const election = await verifyCeremony(c.context, c.ceremony_bundle);
      let aggregate = zeroAggregate();
      for (const encoded of c.ballots) {
        const b = await verifyBallot(election, encoded);
        aggregate = addAggregate(aggregate, b.ciphertext);
      }
      if (aggregate !== c.aggregate) throw Error("EG aggregate gate");
      const result = await verifyTally(
        election,
        aggregate,
        c.count,
        c.tally_bundle,
      );
      if (result.yes !== c.yes || result.no !== c.no)
        throw Error("EG tally gate");
    }
    const s = JSON.parse(readFileSync("spec/fixtures/v0.4/scale.json", "utf8")),
      f = vectors.cases[0],
      e = await verifyCeremony(f.context, f.ceremony_bundle);
    await verifyScale(e, s.context, s.valid);
    let refused = false;
    try {
      await verifyScale(e, s.context, s.copy_attack);
    } catch (error) {
      if (String(error).includes("INDEPENDENCE")) refused = true;
    }
    if (!refused) throw Error("copied ciphertext gate");
  } finally {
    if (globalThis.curve_bn128) await globalThis.curve_bn128.terminate();
  }
}
console.log(
  JSON.stringify({
    suite: review.suite,
    qualified: false,
    status: review.status,
    source_integrity: checkSources ? "verified" : "not_checked",
    development_interop_verified: true,
    crypto_vectors: args.includes("--require-qualified")
      ? "verified"
      : "pinned_not_executed",
    decision: "spec/decisions/0010-electionguard-v0.4.md",
  }),
);
