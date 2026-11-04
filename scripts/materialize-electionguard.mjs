import { readFileSync, writeFileSync } from "node:fs";
import { format } from "prettier";
import { canonical, domain } from "../dist/web/src/protocol.js";
const oracle = JSON.parse(
  readFileSync("spec/fixtures/v0.4/oracle.TEST_ONLY.json", "utf8"),
);
const request = JSON.parse(
  readFileSync(".tools/electionguard/request.json", "utf8"),
);
const external = (v) =>
  Array.isArray(v)
    ? "[" + v.map(external).join(",") + "]"
    : v && typeof v === "object"
      ? "{" +
        Object.keys(v)
          .sort()
          .map((k) => JSON.stringify(k) + ":" + external(v[k]))
          .join(",") +
        "}"
      : JSON.stringify(v);
const write = (path, value) => {
  if (process.argv.includes("--check")) {
    if (readFileSync(path, "utf8") !== value)
      throw Error("stale EG fixture: " + path);
  } else writeFileSync(path, value);
};
const bundle = (files) =>
  Buffer.from(
    canonical({
      suite: "eg-2.0-civic-1",
      files: Object.entries(files)
        .sort(([a], [b]) => (a < b ? -1 : 1))
        .map(([path, bytes]) => ({
          path,
          bytes: Buffer.from(bytes).toString("base64url"),
        })),
    }),
  ).toString("base64url");
let template;
const cases = oracle.contexts.map((o) => {
  const r = request.contexts.find((c) => c.poll_id === o.poll_id);
  const current = external(o.manifest).replaceAll(r.poll_id, "POLL_ID");
  if (template && template !== current)
    throw Error("manifest template differs between polls");
  template = current;
  const files = {
    "context.json": canonical(o.context),
    "manifest.json": external(o.manifest),
    "link.json": canonical({
      network_id: r.network_id,
      poll_id: r.poll_id,
      proposal_id: r.proposal_id,
      question_digest: domain("poll/question", [
        r.network_id,
        r.poll_id,
        r.proposal_id,
      ]),
    }),
  };
  o.guardians.forEach(
    (g, i) => (files[`guardians/0${i + 1}.json`] = canonical(g)),
  );
  return {
    label: r.label,
    context: {
      network_id: r.network_id,
      poll_id: r.poll_id,
      proposal_id: r.proposal_id,
      protocol_epoch: "1",
      guardian_ids: r.guardian_ids,
    },
    ceremony_bundle: bundle(files),
    ballots: o.ballots.map((b) =>
      bundle({ "ballot.json": canonical(b.compact) }),
    ),
    aggregate: canonical(o.tally.ciphertext),
    count: String(o.ballots.length),
    tally_bundle: bundle({ "tally.json": canonical(o.tally) }),
    yes: o.tally.tally,
    no: String(o.ballots.length - Number(o.tally.tally)),
  };
});
if (
  canonical(oracle.parameters) !==
  canonical(
    JSON.parse(
      readFileSync("spec/crypto/electionguard-v2.parameters.json", "utf8"),
    ),
  )
)
  throw Error("SDK group or parameter hash diverges");
write("spec/crypto/electionguard-v2.manifest-template.json", template);
write(
  "spec/fixtures/v0.4/crypto.json",
  canonical({ label: "PUBLIC_ELECTIONGUARD_2_0_VECTORS_NO_SECRETS", cases }),
);
write(
  "web/src/electionguard-parameters.ts",
  await format(
    "// Generated from pinned SDK parameters and the Civic manifest template.\nexport const electionguardParameters = " +
      JSON.stringify({ ...oracle.parameters, manifest_template: template }) +
      " as const;\n",
    { parser: "typescript" },
  ),
);
