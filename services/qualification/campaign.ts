import { createHash, createPublicKey, verify } from "node:crypto";
import {
  readFileSync,
  realpathSync,
  statSync,
  openSync,
  readSync,
  closeSync,
} from "node:fs";
import { isAbsolute, relative, resolve } from "node:path";
import { canonical, parse, uint, validate } from "../../web/src/protocol.js";
// A single final LF frames a CLI record; the JSON itself is canonical.
export function qualificationJSON(raw: Uint8Array): unknown {
  const b =
    raw.length && raw[raw.length - 1] === 10
      ? raw.subarray(0, raw.length - 1)
      : raw;
  return parse(b);
}
export const scenarios = [
  "wan-loss-delay-partition",
  "quorum-loss-no-double-sign",
  "targeted-saturation-and-second-attempts",
  "full-backlog-after-close",
  "archive-unavailable-no-certification",
  "privacy-storage-logs-traffic-limitations",
  "snapshot-corruption-and-restart",
  "compatible-patch-differential-replay",
  "mixed-version-activation",
  "incident-at-close-no-pending-effect",
  "forged-replayed-emergency-orders",
  "distribution-compromise",
  "security-module-rotation",
  "offline-recovery-root-and-mirrors",
  "no-safe-replacement-read-only",
  "receipt-retrieval-and-third-party-replay",
  "fuzz-parsers-and-proofs",
  "post-push-read-surge",
] as const;
interface Artifact {
  path: string;
  sha256: string;
  size: string;
}
interface Policy {
  v: string;
  mode: string;
  reviewers: { organization: string; public_key: string }[];
}
export interface Campaign {
  v: string;
  network_id: string;
  source_revision: string;
  mode: string;
  installation: Artifact | null;
  receipts: {
    nominal: Artifact | null;
    peak: Artifact | null;
    hot_lane: Artifact | null;
    raw_samples: Artifact[];
  };
  verification: {
    envelopes: string;
    full_checks_per_second: string;
    backlog_recovered_ms: string;
    certified_after_close_ms: string;
    challenge_window_ms: string;
    independent_implementations: string[];
    evidence: Artifact | null;
  };
  mobile: {
    physical: boolean;
    generation_p95_ms: string;
    evidence: Artifact | null;
  };
  scenarios: { name: string; passed: boolean; evidence: Artifact }[];
  audits: {
    kind: string;
    organization: string;
    source_revision: string;
    report: Artifact;
  }[];
  findings: {
    id: string;
    severity: string;
    status: string;
    remediation: Artifact | null;
  }[];
  attestations: { public_key: string; signature: string }[];
}
interface ReceiptReport {
  network_id: string;
  poll_id: string;
  window_start_ms: string;
  window_end_ms: string;
  partitions: string;
  attempts: string;
  verified_unique_receipts: string;
  duplicates: string;
  failed_attempts: string;
  receipt_p95_ms: string;
  receipt_p99_ms: string;
  minimum_receipts_per_second: string;
  per_second: string[];
  per_lane: string[];
  average_envelope_bytes: string;
  maximum_envelope_bytes: string;
}
export function artifactReader(directory: string) {
  const root = realpathSync(directory);
  return (artifact: Artifact, contents = true): Uint8Array => {
    if (isAbsolute(artifact.path)) throw Error("absolute evidence path");
    const file = realpathSync(resolve(root, artifact.path)),
      child = relative(root, file);
    if (
      child === ".." ||
      child.startsWith("../") ||
      child.startsWith("..\\") ||
      isAbsolute(child)
    )
      throw Error("evidence escapes campaign directory");
    const size = statSync(file).size;
    if (String(size) !== artifact.size || (contents && size > 64 * 1024 * 1024))
      throw Error(
        "evidence size mismatch or exceeds 64 MiB per artifact; split large traces",
      );
    if (contents) {
      const b = readFileSync(file);
      if (
        b.length !== size ||
        createHash("sha256").update(b).digest("hex") !== artifact.sha256
      )
        throw Error("evidence hash/size mismatch");
      return b;
    }
    const hash = createHash("sha256"),
      fd = openSync(file, "r"),
      chunk = Buffer.alloc(1024 * 1024);
    let total = 0;
    try {
      let count: number;
      while ((count = readSync(fd, chunk, 0, chunk.length, null)) > 0) {
        hash.update(chunk.subarray(0, count));
        total += count;
      }
    } finally {
      closeSync(fd);
    }
    if (total !== size || hash.digest("hex") !== artifact.sha256)
      throw Error("evidence hash/size mismatch");
    return new Uint8Array();
  };
}
export function assessCampaign(
  value: unknown,
  options: {
    load: (artifact: Artifact, contents?: boolean) => Uint8Array;
    suiteQualified: boolean;
    policy?: unknown;
    policyPinVerified?: boolean;
  },
) {
  const c = validate<Campaign>("qualification_campaign", value),
    checks: { name: string; passed: boolean; reason: string }[] = [];
  const check = (name: string, passed: boolean, reason: string) =>
    checks.push({ name, passed, reason });
  const load = (
    name: string,
    a: Artifact | null,
    contents = false,
  ): Uint8Array | undefined => {
    if (!a) {
      check(name, false, "Evidence is missing");
      return;
    }
    try {
      const b = options.load(a, contents);
      check(name, true, "Exact bytes, size and SHA-256 checked");
      return b;
    } catch (e) {
      check(name, false, String(e));
      return;
    }
  };
  check(
    "real-campaign",
    c.mode === "real",
    "Development fixtures do not qualify public capacity",
  );
  check(
    "cryptographic-suite-audited-and-qualified",
    options.suiteQualified,
    "The pinned voting-suite lock must authorize public qualification",
  );
  load(
    "documented-installation-32-partitions-7-independent-seats",
    c.installation,
  );
  for (const a of c.receipts.raw_samples)
    load("receipt-raw-samples:" + a.path, a);
  check(
    "raw-receipt-samples-present",
    c.receipts.raw_samples.length > 0,
    "Measurements require immutable raw traces and reproducible verification",
  );
  const polls = new Set<string>();
  for (const kind of ["nominal", "peak", "hot_lane"] as const) {
    const bytes = load("receipts:" + kind, c.receipts[kind], true);
    if (!bytes) continue;
    try {
      const r = validate<ReceiptReport>(
          "qualification_receipt_report",
          qualificationJSON(bytes),
        ),
        duration = uint(r.window_end_ms) - uint(r.window_start_ms);
      if (
        duration <= 0n ||
        duration > 86400000n ||
        duration % 1000n ||
        BigInt(r.per_second.length) !== duration / 1000n ||
        r.per_lane.length !== 32 ||
        uint(r.partitions) !== 32n ||
        r.network_id !== c.network_id
      )
        throw Error("invalid window, network or partition layout");
      const n = uint(r.verified_unique_receipts),
        sum = (xs: string[]) => xs.reduce((a, b) => a + uint(b), 0n);
      if (
        sum(r.per_second) !== n ||
        sum(r.per_lane) !== n ||
        uint(r.attempts) !== n + uint(r.duplicates) + uint(r.failed_attempts)
      )
        throw Error("inconsistent receipt counters");
      const minimum = r.per_second.reduce(
        (a, b) => (uint(b) < a ? uint(b) : a),
        uint(r.per_second[0]!),
      );
      if (uint(r.minimum_receipts_per_second) !== minimum)
        throw Error("sustained throughput omits empty seconds");
      check(
        kind + ":receipt-latency",
        n > 0n &&
          uint(r.receipt_p95_ms) <= 10000n &&
          uint(r.receipt_p99_ms) <= 30000n,
        "p95 <= 10 s and p99 <= 30 s; transport errors are published separately",
      );
      if (kind === "nominal")
        check(
          "ten-million-distinct-authenticated-receipts",
          n >= 10000000n && r.per_lane.every((x) => uint(x) > 0n),
          "10 million unique envelopes, all 32 partitions",
        );
      if (kind === "peak")
        check(
          "5000-per-second-for-900-seconds",
          duration >= 900000n && minimum >= 5000n,
          "Each full second must meet the target; averages cannot hide stalls",
        );
      if (kind === "hot_lane")
        check(
          "hot-lane-320-per-second",
          duration >= 60000n &&
            minimum >= 320n &&
            r.per_lane.filter((x) => uint(x) > 0n).length === 1,
          "A single partition sustains 320/s for at least 60 s",
        );
      check(
        kind + ":real-envelope-sizes",
        uint(r.average_envelope_bytes) > 0n &&
          uint(r.maximum_envelope_bytes) >= uint(r.average_envelope_bytes),
        "Real envelope sizes must be published",
      );
      polls.add(r.poll_id);
    } catch (e) {
      check("receipts:" + kind + ":consistent", false, String(e));
    }
  }
  check(
    "three-separate-scenarios",
    polls.size === 3,
    "Nominal, peak and hot-lane runs use distinct poll identities",
  );
  const v = c.verification;
  load("full-verification-and-backlog-evidence", v.evidence);
  check(
    "twenty-million-full-checks",
    uint(v.envelopes) >= 20000000n && uint(v.full_checks_per_second) >= 500n,
    "Two possible admissions per elector; 500 complete checks/s",
  );
  check(
    "backlog-and-certification-within-24h",
    uint(v.backlog_recovered_ms) > 0n &&
      uint(v.backlog_recovered_ms) <= 86400000n &&
      uint(v.certified_after_close_ms) > 0n &&
      uint(v.certified_after_close_ms) <= 86400000n &&
      uint(v.challenge_window_ms) >= 1800000n,
    "Include full catch-up, all audits, tally and 30-minute challenge window",
  );
  check(
    "independent-verifier-implementations",
    new Set(v.independent_implementations).size >= 2,
    "Two independently implemented complete verifiers",
  );
  load("physical-mobile-evidence", c.mobile.evidence);
  check(
    "physical-mobile-generation-p95",
    c.mobile.physical &&
      uint(c.mobile.generation_p95_ms) > 0n &&
      uint(c.mobile.generation_p95_ms) <= 8000n,
    "Physical reference devices, p95 <= 8 s",
  );
  const names = new Set<string>();
  for (const s of c.scenarios) {
    if (names.has(s.name)) throw Error("duplicate scenario");
    names.add(s.name);
    load("scenario-evidence:" + s.name, s.evidence);
  }
  for (const name of scenarios)
    check(
      "scenario:" + name,
      c.scenarios.some((s) => s.name === name && s.passed),
      "Scenario must pass against this installation/revision with replayable evidence",
    );
  const auditOrganizations = new Set<string>();
  for (const a of c.audits) {
    load("audit:" + a.kind, a.report);
    if (a.source_revision === c.source_revision)
      auditOrganizations.add(a.organization);
  }
  check(
    "external-cryptographic-and-application-audits",
    ["cryptographic", "application"].every((kind) =>
      c.audits.some(
        (a) => a.kind === kind && a.source_revision === c.source_revision,
      ),
    ) && auditOrganizations.size >= 2,
    "Independent organizations, both audits cover the exact revision",
  );
  for (const f of c.findings) {
    if (f.severity === "critical" || f.severity === "high") {
      check(
        "finding:" + f.id,
        f.status === "fixed",
        "Critical/high findings cannot be accepted as residual risk",
      );
      load("remediation:" + f.id, f.remediation);
    }
  }
  let trusted = 0;
  if (options.policy && options.policyPinVerified) {
    const policy = validate<Policy>("qualification_policy", options.policy),
      seen = new Set<string>(),
      orgs = new Set<string>();
    if (
      new Set(policy.reviewers.map((r) => r.public_key)).size !== 5 ||
      new Set(policy.reviewers.map((r) => r.organization)).size !== 5
    )
      throw Error(
        "reviewer policy requires five independent identities and keys",
      );
    const { attestations: _, ...statement } = c;
    const message = Buffer.from(
      "CIVIC/QUALIFICATION/1\n" + canonical(statement),
    );
    for (const a of c.attestations) {
      if (seen.has(a.public_key)) throw Error("duplicate qualification signer");
      seen.add(a.public_key);
      const reviewer = policy.reviewers.find(
        (r) => r.public_key === a.public_key,
      );
      if (!reviewer) continue;
      const key = createPublicKey({
        key: Buffer.concat([
          Buffer.from("302a300506032b6570032100", "hex"),
          Buffer.from(a.public_key, "hex"),
        ]),
        format: "der",
        type: "spki",
      });
      if (verify(null, message, key, Buffer.from(a.signature, "hex")))
        orgs.add(reviewer.organization);
    }
    if (policy.mode === "operator") trusted = orgs.size;
  }
  check(
    "independent-qualification-attestations",
    trusted >= 3,
    "3/5 independently installed reviewer keys sign the exact campaign and all artifact references",
  );
  return {
    milestone: "v0.9",
    status: checks.every((x) => x.passed) ? "qualified" : "not_qualified",
    public_qualified: checks.every((x) => x.passed),
    network_id: c.network_id,
    source_revision: c.source_revision,
    checks,
  };
}
