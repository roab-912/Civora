// Finalizes an experimental local distribution. This command cannot publish or qualify a public pilot.
import { spawnSync } from "node:child_process";
import { readFileSync, writeFileSync, existsSync, mkdirSync } from "node:fs";
import { resolve, join, dirname } from "node:path";
import { pathToFileURL } from "node:url";
import {
  json,
  sha256,
  fileRef,
  validateManifest,
  readSourceSnapshot,
  sourceFingerprint,
  validateLocalChecks,
  assertConfinedRegular,
  requireExperimental,
} from "./distribution-lib.mjs";

export function finalizeRelease({
  release,
  checksFile,
  output,
  allowTestKeys = false,
}) {
  if (!allowTestKeys)
    throw Error(
      "Explicit --allow-test-keys required for this experimental local exercise",
    );
  release = resolve(release);
  const payload = join(release, "payload"),
    manifest = validateManifest(
      JSON.parse(readFileSync(join(payload, "release.json"))),
    );
  const snapshot = readSourceSnapshot(join(release, "work/source")),
    fingerprint = sourceFingerprint(snapshot);
  if (sourceFingerprint() !== fingerprint)
    throw Error(
      "Release source snapshot differs from the current implementation",
    );
  for (const f of manifest.files) {
    const file = join(payload, f.path);
    assertConfinedRegular(payload, file);
    const actual = fileRef(file, f.path);
    if (actual.sha256 !== f.sha256 || actual.size !== f.size)
      throw Error("Release integrity mismatch: " + f.path);
  }
  const provenance = JSON.parse(
    readFileSync(join(payload, "provenance.intoto.json")),
  );
  if (
    provenance.predicateType !== "https://slsa.dev/provenance/v1" ||
    provenance._type !== "https://in-toto.io/Statement/v1"
  )
    throw Error("Unsupported provenance");
  for (const subject of provenance.subject) {
    if (
      manifest.files.find((f) => f.path === subject.name)?.sha256 !==
      subject.digest.sha256
    )
      throw Error("Provenance subject mismatch");
  }
  const source = manifest.files.find((f) => f.path.endsWith("-source.zip"));
  if (
    !source ||
    provenance.predicate.buildDefinition.resolvedDependencies[0]?.digest
      ?.sha256 !== source.sha256
  )
    throw Error("Source archive provenance mismatch");
  const checks = validateLocalChecks(
    JSON.parse(readFileSync(checksFile)),
    fingerprint,
  );
  const toolchain = provenance.predicate.buildDefinition.internalParameters;
  if (checks.go !== toolchain.go || checks.node !== toolchain.node)
    throw Error("Build and check toolchains differ");
  const distribution = JSON.parse(readFileSync(checks.distribution_report));
  if (
    distribution.passed !== true ||
    distribution.source_sha256 !== fingerprint
  )
    throw Error("Standalone distribution checks missing or from other sources");
  const nativeTarget =
    process.platform === "win32"
      ? "windows-amd64"
      : process.arch === "arm64"
        ? "linux-arm64"
        : "linux-amd64";
  const nativeName = `civora-${manifest.release_version}-${nativeTarget}.zip`;
  if (
    manifest.files.find((f) => f.path === nativeName)?.sha256 !==
    distribution.native_package_sha256
  )
    throw Error("Final native package differs from the executed package");
  const go =
    process.env.CIVORA_GO ??
    (existsSync(".tools/go/bin/go.exe")
      ? resolve(".tools/go/bin/go.exe")
      : "go");
  const env = {
    ...process.env,
    GOPATH: process.env.GOPATH ?? resolve(".tools/gopath"),
    GOMODCACHE: process.env.GOMODCACHE ?? resolve(".tools/gomodcache"),
    GOCACHE: process.env.GOCACHE ?? resolve(".tools/gocache"),
    GOFLAGS: "-buildvcs=false -mod=readonly",
  };
  const run = (args) => {
    const r = spawnSync(
      go,
      ["-C", "tools/distribution", "run", "./cmd/civdist", ...args],
      { env, encoding: "utf8", windowsHide: true, maxBuffer: 4 * 1024 * 1024 },
    );
    if (r.error || r.status !== 0)
      throw Error("Distribution verification failed: " + (r.error ?? r.stderr));
    return r.stdout;
  };
  const mirror = join(release, "mirror"),
    root = join(mirror, "root.TEST_ONLY.json");
  if (!existsSync(mirror))
    run([
      "seal-test",
      "--payload",
      payload,
      "--repository",
      mirror,
      "--epoch",
      manifest.created,
      "--allow-test-keys",
    ]);
  const pin = sha256(readFileSync(root));
  const verified = JSON.parse(
    run([
      "verify",
      "--repository",
      mirror,
      "--root",
      root,
      "--root-sha256",
      pin,
      "--state",
      join(release, "trust/metadata.db"),
      "--allow-test-keys",
    ]),
  );
  if (
    verified.mode !== "development_TEST_ONLY" ||
    verified.targets.length !== manifest.files.length + 1 ||
    verified.protocol_activation !== false
  )
    throw Error("Unexpected TUF verification scope");
  if (
    json(verified.targets.map((t) => t.path).sort()) !==
    json([...manifest.files.map((f) => f.path), "release.json"].sort())
  )
    throw Error(
      "TUF must cover the exact release manifest and every declared file",
    );
  for (const t of verified.targets)
    if (sha256(readFileSync(join(payload, t.path))) !== t.sha256)
      throw Error("Mirror payload mismatch");
  writeFileSync(join(release, "verification.json"), json(verified));
  const report = {
    milestone: "v1.0",
    release_version: manifest.release_version,
    date: new Date().toISOString().slice(0, 10),
    status: "experimental_release_finalized_public_pilot_blocked",
    experimental_release_complete: true,
    local_preparation_complete: true,
    milestone_complete: false,
    public_qualified: false,
    qualified_to_ten_million_ballots: false,
    verified_capacity_ballots: null,
    guide: "spec/manual/README.md",
    decision: "spec/decisions/0018-finalisation-experimentale-v1.0.md",
    qualification: "spec/reports/v0.9-qualification.json",
    distribution: {
      directory: release.replaceAll("\\", "/"),
      manifest: fileRef(join(payload, "release.json"), "payload/release.json"),
      files: manifest.files,
      keys: "PUBLIC_TEST_ONLY; no production signing keys generated",
      root_sha256: pin,
      protocol_activation: false,
      pwa_version: "0.8.0",
      source_sha256: fingerprint,
      source_boundary:
        "Generated verification reports are excluded from the test fingerprint. The archived source inventory records every exact source byte used by the builders.",
    },
    verification: {
      full_regression_gate: checks,
      standalone_distribution: distribution,
      tuf_mirror: {
        passed: true,
        targets: verified.targets.length,
        report: join(release, "verification.json").replaceAll("\\", "/"),
      },
      native_package_identical_to_executed_package: true,
      reproducibility: {
        passed: true,
        scope:
          "Git checkout versus a verified source snapshot with Git unavailable, on one local environment",
        independent_build_environments: false,
      },
      platforms: Object.fromEntries(
        manifest.targets.map((t) => [
          t,
          t === nativeTarget
            ? "Built and executed: replay, independent audits, TUF and PWA host passed"
            : "Cross-compiled; runtime qualification pending",
        ]),
      ),
      provenance: {
        predicate_type: provenance.predicateType,
        assurance: "local_self_attestation",
        slsa_level_claimed: null,
      },
    },
    public_blockers: [
      "Real v0.9 qualification and replayable 10-million-ballot, 32-partition/WAN/saturation/physical-device evidence",
      "Independent cryptographic and application audits and correction of critical/high findings",
      "Complete constitutional recovery and incident qualification",
      "Two independent build environments and two independent publication reviews",
      "Genuine offline distribution keys, independent root bootstrap and mirrors; operational ceremony and compromise/recovery exercise",
    ],
    limitations: [
      "Experimental finalization is not completion of the public-pilot milestone",
      "Development signing keys are public; their signatures provide no operator authenticity",
      "No public deployment or protocol activation occurred",
      "Local report receipts are not independent audit attestations",
      "The persistent verifier state must be retained; abrupt-exit tests do not prove electrical power-loss or host-backup rollback resistance",
    ],
  };
  const file = resolve(output);
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, json(report));
  return report;
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  if (process.argv[2] === "fingerprint")
    process.stdout.write(sourceFingerprint() + "\n");
  else {
    const args = process.argv.slice(2),
      get = (k) => {
        const i = args.indexOf(k);
        if (i < 0 || !args[i + 1] || args[i + 1].startsWith("--"))
          throw Error("Missing " + k);
        return args[i + 1];
      };
    requireExperimental(args);
    for (let i = 0; i < args.length; i++) {
      if (args[i] === "--allow-test-keys") continue;
      if (!["--release", "--checks", "--output"].includes(args[i]))
        throw Error("Unknown option: " + args[i]);
      i++;
    }
    const report = finalizeRelease({
      release: get("--release"),
      checksFile: get("--checks"),
      output: get("--output"),
      allowTestKeys: args.includes("--allow-test-keys"),
    });
    console.log(
      json({
        release_version: report.release_version,
        status: report.status,
        public_qualified: false,
        report: get("--output"),
      }),
    );
  }
}
