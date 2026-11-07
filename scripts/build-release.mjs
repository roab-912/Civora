// Local, allowlisted release preparation. No upload, install or protocol activation.
import { spawnSync } from "node:child_process";
import {
  readFileSync,
  writeFileSync,
  mkdirSync,
  existsSync,
  readdirSync,
  copyFileSync,
  cpSync,
  lstatSync,
} from "node:fs";
import { resolve, join, dirname, relative, isAbsolute } from "node:path";
import { randomUUID } from "node:crypto";
import { createRequire } from "node:module";
import { build } from "esbuild";
import {
  releaseVersion,
  json,
  fileRef,
  formatCatalogue,
  validateManifest,
  requireExperimental,
  collectSourceInputs,
  copySourceSnapshot,
} from "./distribution-lib.mjs";
requireExperimental(process.argv.slice(2));
const args = process.argv.slice(2),
  get = (k, fallback) => {
    const i = args.indexOf(k);
    if (i >= 0 && (!args[i + 1] || args[i + 1].startsWith("--")))
      throw Error("Missing " + k);
    return i < 0 ? fallback : args[i + 1];
  };
const supported = new Set(["--out", "--targets", "--epoch"]);
for (let i = 0; i < args.length; i += 2)
  if (!supported.has(args[i])) throw Error("Unknown option: " + args[i]);
const epoch = get(
  "--epoch",
  process.env.SOURCE_DATE_EPOCH
    ? new Date(Number(process.env.SOURCE_DATE_EPOCH) * 1000)
        .toISOString()
        .replace(".000Z", "Z")
    : "",
);
if (
  !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(epoch) ||
  new Date(epoch).toISOString().replace(".000Z", "Z") !== epoch
)
  throw Error(
    "Explicit reproducible --epoch YYYY-MM-DDTHH:mm:ssZ or SOURCE_DATE_EPOCH required",
  );
const targets = get("--targets", "windows-amd64,linux-amd64").split(",").sort();
if (
  !targets.length ||
  new Set(targets).size !== targets.length ||
  targets.some(
    (t) => !["windows-amd64", "linux-amd64", "linux-arm64"].includes(t),
  )
)
  throw Error("Unsupported or duplicate targets");
const output = resolve(
  get("--out", ".tools/releases/" + releaseVersion + "-" + randomUUID()),
);
if (existsSync(output)) throw Error("Output must be a new directory");
const outputPath = relative(resolve(".tools"), output);
if (
  !outputPath ||
  outputPath === ".." ||
  outputPath.startsWith("../") ||
  outputPath.startsWith("..\\") ||
  isAbsolute(outputPath)
)
  throw Error("Output must be inside this workspace's .tools directory");
mkdirSync(output, { recursive: true });
const work = join(output, "work"),
  common = join(work, "common"),
  payload = join(output, "payload");
for (const d of [work, common, payload]) mkdirSync(d, { recursive: true });
const require = createRequire(import.meta.url);
const go =
  process.env.CIVORA_GO ??
  (existsSync(".tools/go/bin/go.exe") ? resolve(".tools/go/bin/go.exe") : "go");
const workspace = process.cwd(),
  tsc = require.resolve("typescript/bin/tsc"),
  workerFile = require.resolve("web-worker"),
  artifactCache = resolve(
    process.env.CIVORA_ARTIFACT_DIR ?? ".tools/semaphore",
  );
const env = {
  ...process.env,
  GOPATH: process.env.GOPATH ?? resolve(".tools/gopath"),
  GOMODCACHE: process.env.GOMODCACHE ?? resolve(".tools/gomodcache"),
  GOCACHE: process.env.GOCACHE ?? resolve(".tools/gocache"),
  GOFLAGS: "-buildvcs=false -mod=readonly",
  CGO_ENABLED: "0",
};
function run(command, params, extra = {}) {
  const r = spawnSync(command, params, {
    env,
    encoding: "utf8",
    maxBuffer: 32 * 1024 * 1024,
    ...extra,
  });
  if (r.error || r.status !== 0)
    throw Error(command + " failed: " + (r.error ?? r.stderr ?? r.stdout));
  return r.stdout;
}
console.log("Preparing experimental distribution " + output);
// Check the installed, pinned proof material before building an isolated source snapshot.
run(process.execPath, [tsc, "-p", "tsconfig.json"]);
run(process.execPath, ["scripts/check-semaphore.mjs", "--require-artifacts"]);
const sourceDir = join(work, "source");
copySourceSnapshot(workspace, sourceDir, collectSourceInputs(workspace));
// Every compiler and bundler now reads the archived source snapshot, never a changing editor buffer.
process.chdir(sourceDir);
const native = join(
  work,
  process.platform === "win32" ? "civdist.exe" : "civdist",
);
run(go, [
  "-C",
  "tools/distribution",
  "build",
  "-trimpath",
  "-ldflags=-buildid=",
  "-o",
  native,
  "./cmd/civdist",
]);
function archive(source, destination) {
  run(native, ["archive", "--source", source, "--output", destination]);
}
const sourceName = "civora-" + releaseVersion + "-source.zip";
archive(sourceDir, join(payload, sourceName));
const sourceRef = fileRef(join(payload, sourceName), sourceName);
run(process.execPath, [tsc, "-p", "tsconfig.json"]);
run(process.execPath, ["scripts/build-pwa.mjs", "--out", join(common, "web")]);
function copy(file, dest = file) {
  if (!lstatSync(file).isFile() || lstatSync(file).isSymbolicLink())
    throw Error("Nonregular release input: " + file);
  const to = join(common, dest);
  mkdirSync(dirname(to), { recursive: true });
  copyFileSync(file, to);
}
for (const dir of [
  "spec/schemas",
  "spec/crypto",
  "spec/profiles",
  "spec/distribution",
  "spec/manual",
]) {
  for (const name of readdirSync(dir).sort())
    if (/\.(json|md)$/.test(name) && lstatSync(join(dir, name)).isFile())
      copy(join(dir, name));
}
for (const f of [
  "README.md",
  "spec/Protocole-Civique-v1.md",
  "spec/reports/v0.9-qualification.json",
  "spec/qualification/README.md",
  "spec/fixtures/v0.6/network.json",
])
  copy(f);
const lock = JSON.parse(readFileSync("spec/crypto/semaphore-v4.lock.json"));
for (const d of lock.depths)
  for (const kind of ["wasm", "zkey"]) {
    const a = d[kind],
      file = join(artifactCache, a.file);
    const r = fileRef(file, "");
    if (r.sha256 !== a.sha256 || r.size !== a.size)
      throw Error("Artifact lock mismatch");
    copy(file, "artifacts/semaphore/" + a.file);
  }
// Public verifiers and the host have no package-manager dependency at execution time.
for (const name of ["audit-citizen", "audit-electionguard", "host-pwa"]) {
  const dest = join(common, "tools", name + ".mjs");
  await build({
    entryPoints: ["scripts/" + name + ".mjs"],
    outfile: dest,
    bundle: true,
    platform: "node",
    format: "esm",
    target: "node24",
    sourcemap: false,
    minify: true,
    banner: {
      js: 'import { createRequire as __civoraRequire } from "node:module"; const require = __civoraRequire(import.meta.url);',
    },
    plugins: [
      {
        name: "self-referencing-node-worker",
        setup(b) {
          b.onResolve({ filter: /^web-worker$/ }, () => ({
            path: "./runtime/web-worker.cjs",
            external: true,
          }));
        },
      },
    ],
  });
}
// This worker starts its own physical __filename; keep the pinned, unmodified CommonJS file.
copy(workerFile, "tools/runtime/web-worker.cjs");
copy(
  join(dirname(dirname(workerFile)), "LICENSE"),
  "tools/runtime/web-worker.LICENSE",
);
writeFileSync(
  join(common, "pwa-networks.example.json"),
  json({ development: false, networks: [] }),
);
const catalogue = formatCatalogue();
writeFileSync(join(common, "formats.json"), json(catalogue));
writeFileSync(join(payload, "formats.json"), json(catalogue));

const rootCommands = [
  "civnode",
  "civnet",
  "civcrypto",
  "civeligibility",
  "civqual",
  "civrecovery",
];
const moduleList = (dir, target) => {
  const [GOOS, GOARCH] = target.split("-");
  const text = run(
    go,
    [
      ...(dir ? ["-C", dir] : []),
      "list",
      "-deps",
      "-f",
      "{{with .Module}}{{.Path}}|{{.Version}}|{{.Sum}}|{{.Main}}|{{if .Replace}}REPLACED{{end}}{{end}}",
      ...(dir ? ["./cmd/civdist"] : rootCommands.map((c) => "./cmd/" + c)),
    ],
    { env: { ...env, GOOS, GOARCH } },
  );
  return [...new Set(text.trim().split(/\r?\n/).filter(Boolean))]
    .sort()
    .map((line) => {
      const [Path, Version, Sum, Main, Replace] = line.split("|");
      if (Replace) throw Error("Unreviewed module replacement");
      return {
        path: Path,
        version: Version || "local_source",
        sum: Sum || null,
        main: Main === "true",
      };
    });
};
const npmLock = JSON.parse(readFileSync("package-lock.json"));
const inventory = {
  format: "civora-build-dependency-inventory-2",
  source: sourceRef,
  platforms: Object.fromEntries(
    targets.map((target) => [
      target,
      {
        go: moduleList(undefined, target),
        distribution_go: moduleList("tools/distribution", target),
      },
    ]),
  ),
  npm: Object.entries(npmLock.packages)
    .filter(([p]) => p)
    .map(([path, p]) => ({
      path,
      version: p.version,
      integrity: p.integrity ?? null,
      resolved: p.resolved ?? null,
    })),
  locks: ["go.sum", "package-lock.json", "tools/distribution/go.sum"].map((f) =>
    fileRef(f, f),
  ),
};
writeFileSync(join(payload, "dependencies.json"), json(inventory));
const { assessCampaign, artifactReader, qualificationJSON } = await import(
  (await import("node:url")).pathToFileURL(
    join(sourceDir, "dist/services/qualification/campaign.js"),
  ).href
);
const campaignFile = "spec/qualification/campaign.example.json";
const assessment = assessCampaign(
  qualificationJSON(readFileSync(campaignFile)),
  {
    load: artifactReader(dirname(campaignFile)),
    suiteQualified:
      JSON.parse(readFileSync("spec/crypto/electionguard-v2.lock.json"))
        .public_pilot_qualified === true,
  },
);
writeFileSync(join(payload, "qualification.json"), json(assessment));
const packages = [];
for (const target of targets) {
  console.log("Building " + target);
  const [GOOS, GOARCH] = target.split("-"),
    stage = join(work, target);
  cpSync(common, stage, { recursive: true });
  mkdirSync(join(stage, "bin"));
  for (const cmd of [
    "civnode",
    "civnet",
    "civcrypto",
    "civeligibility",
    "civqual",
    "civrecovery",
    "civdist",
  ]) {
    const cmdEnv = { ...env, GOOS, GOARCH };
    run(
      go,
      [
        ...(cmd === "civdist" ? ["-C", "tools/distribution"] : []),
        "build",
        "-trimpath",
        "-ldflags=-buildid=",
        "-o",
        join(stage, "bin", cmd + (GOOS === "windows" ? ".exe" : "")),
        "./cmd/" + cmd,
      ],
      { env: cmdEnv },
    );
  }
  const name = `civora-${releaseVersion}-${target}.zip`;
  archive(stage, join(payload, name));
  packages.push(fileRef(join(payload, name), name));
}
const provenance = {
  _type: "https://in-toto.io/Statement/v1",
  subject: packages.map((p) => ({
    name: p.path,
    digest: { sha256: p.sha256 },
  })),
  predicateType: "https://slsa.dev/provenance/v1",
  predicate: {
    buildDefinition: {
      buildType: "https://civora.invalid/build/experimental-release/v1",
      externalParameters: { epoch, targets, version: releaseVersion },
      internalParameters: {
        go: run(go, ["version"]).trim(),
        node: process.version,
        platform: process.platform,
        arch: process.arch,
        CGO_ENABLED: "0",
        flags: [
          "-trimpath",
          "-buildvcs=false",
          "-mod=readonly",
          "-ldflags=-buildid=",
        ],
      },
      resolvedDependencies: [
        { uri: sourceName, digest: { sha256: sourceRef.sha256 } },
        ...inventory.locks.map((f) => ({
          uri: f.path,
          digest: { sha256: f.sha256 },
        })),
      ],
    },
    runDetails: {
      builder: { id: "https://civora.invalid/builders/local-self-attested" },
      metadata: { invocationId: "sha256:" + sourceRef.sha256 },
      byproducts: [
        {
          name: "assurance",
          content: Buffer.from(
            "Local self-attestation; no SLSA level; no independent second build or review.",
          ).toString("base64"),
        },
      ],
    },
  },
};
writeFileSync(join(payload, "provenance.intoto.json"), json(provenance));
const manifest = validateManifest({
  schema_version: "1",
  release_version: releaseVersion,
  channel: "experimental",
  public_qualified: false,
  verified_capacity_ballots: null,
  created: epoch,
  targets,
  files: readdirSync(payload)
    .sort()
    .map((f) => fileRef(join(payload, f), f)),
  protocol_activation: false,
});
writeFileSync(join(payload, "release.json"), json(manifest));
console.log(
  json({
    output,
    manifest: join(payload, "release.json"),
    mode: "experimental",
    public_qualified: false,
    independent_builds: "pending",
    sealing:
      "Use civdist seal-test explicitly for a local exercise; no public signing keys are generated.",
  }),
);
