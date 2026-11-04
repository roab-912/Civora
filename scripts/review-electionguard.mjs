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
  review.status !== "blocked_interoperability" ||
  review.review_scope !== "source_review_only_no_sdk_execution" ||
  !/^[0-9a-f]{40}$/.test(review.candidate_commit) ||
  review.artifacts.length !== 5 ||
  review.blockers.length !== 3
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
console.log(
  JSON.stringify({
    suite: review.suite,
    qualified: false,
    status: review.status,
    source_integrity: checkSources ? "verified" : "not_checked",
    crypto_vectors: "not_qualified",
    decision: "spec/decisions/0009-electionguard-interop.md",
  }),
);
if (args.includes("--require-qualified")) process.exitCode = 2;
