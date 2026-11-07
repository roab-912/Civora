import { createHash } from "node:crypto";
import {
  readFileSync,
  readdirSync,
  lstatSync,
  realpathSync,
  mkdirSync,
  copyFileSync,
  writeFileSync,
  existsSync,
} from "node:fs";
import { spawnSync } from "node:child_process";
import { resolve, relative, sep, isAbsolute, join, dirname } from "node:path";
import { Ajv2020 } from "ajv/dist/2020.js";

export const releaseVersion = "1.0.0";
export const sourceManifestName = ".civora-source.json";
export const sha256 = (b) => createHash("sha256").update(b).digest("hex");
export const json = (v) => JSON.stringify(v, null, 2) + "\n";
export const fileRef = (file, path) => {
  const bytes = readFileSync(file);
  return { path, sha256: sha256(bytes), size: String(bytes.length) };
};

export function isSourceFile(name, tracked) {
  const newFiles = new Set([
    "scripts/build-release.mjs",
    "scripts/distribution-lib.mjs",
    "scripts/compare-release.mjs",
    "scripts/test-distribution.mjs",
    "spec/distribution/release.schema.json",
    "spec/distribution/source.schema.json",
    "spec/distribution/profiles.json",
    "spec/manual/README.md",
    "spec/decisions/0017-distribution-v1.0.md",
    "spec/decisions/0018-finalisation-experimentale-v1.0.md",
    "scripts/finalize-release.mjs",
    "scripts/distribution.test.mjs",
    "spec/reports/v1.0-distribution.json",
    "tools/distribution/go.mod",
    "tools/distribution/go.sum",
    "tools/distribution/cmd/civdist/main.go",
    "tools/distribution/internal/distribution/verify.go",
    "tools/distribution/internal/distribution/repository.go",
    "tools/distribution/internal/distribution/distribution_test.go",
  ]);
  const fixture =
    /^spec\/fixtures\/v1\.0\/distribution\/(root\.TEST_ONLY\.json|metadata\/(1\.root|1\.snapshot|1\.targets|timestamp)\.json|targets\/[0-9a-f]{64}\.sample\.TEST_ONLY\.json)$/.test(
      name,
    );
  if (!tracked && !newFiles.has(name) && !fixture) return false;
  return (
    (/^(cmd|internal|web|services|scripts|spec|tools)\//.test(name) &&
      /\.(go|mod|sum|ts|mjs|json|md|ps1|sh|html|css|svg|png|webmanifest|kt|kts)$/.test(
        name,
      )) ||
    [
      "go.mod",
      "go.sum",
      "package.json",
      "package-lock.json",
      "tsconfig.json",
      "README.md",
      ".gitignore",
      ".gitattributes",
    ].includes(name)
  );
}

export function formatCatalogue() {
  return {
    schema_version: "1",
    status: "experimental_release",
    release: releaseVersion,
    protocol_epochs_changed: false,
    schemas: readdirSync("spec/schemas")
      .filter((f) => f.endsWith(".schema.json"))
      .sort()
      .map((f) => {
        const file = "spec/schemas/" + f;
        return {
          ...fileRef(file, file),
          id: JSON.parse(readFileSync(file)).$id,
        };
      }),
    distribution_schema: fileRef(
      "spec/distribution/release.schema.json",
      "spec/distribution/release.schema.json",
    ),
    source_schema: fileRef(
      "spec/distribution/source.schema.json",
      "spec/distribution/source.schema.json",
    ),
    encoding:
      "Existing protocol codecs and schema identifiers are preserved; TUF and SLSA use their own standard encodings.",
  };
}

export function validateManifest(value) {
  const schema = JSON.parse(
    readFileSync("spec/distribution/release.schema.json"),
  );
  const validate = new Ajv2020({ strict: true }).compile(schema);
  if (!validate(value))
    throw Error("Invalid release manifest: " + JSON.stringify(validate.errors));
  if (new Set(value.files.map((x) => x.path)).size !== value.files.length)
    throw Error("Duplicate release file");
  if (
    new Date(value.created).toISOString().replace(".000Z", "Z") !==
    value.created
  )
    throw Error("Invalid release creation time");
  return value;
}

function validateSource(value, base) {
  const schema = JSON.parse(
    readFileSync(join(base, "spec/distribution/source.schema.json")),
  );
  const check = new Ajv2020({ strict: true }).compile(schema);
  if (!check(value))
    throw Error("Invalid source manifest: " + JSON.stringify(check.errors));
  const seen = new Set();
  let previous = "";
  for (const f of value.files) {
    if (
      !isSourceFile(f.path, true) ||
      f.path
        .split("/")
        .some((p) => p === "." || p === ".." || p === "" || p.endsWith(".")) ||
      seen.has(f.path.toLowerCase()) ||
      f.path <= previous
    )
      throw Error("Unsafe or duplicate source path");
    seen.add(f.path.toLowerCase());
    previous = f.path;
    const file = join(base, f.path);
    assertConfinedRegular(base, file);
    if (
      String(lstatSync(file).size) !== f.size ||
      lstatSync(file).size > 64 * 1024 * 1024 ||
      sha256(readFileSync(file)) !== f.sha256
    )
      throw Error("Source integrity mismatch: " + f.path);
  }
  for (const required of [
    "package.json",
    "package-lock.json",
    "go.mod",
    "go.sum",
    "tsconfig.json",
    "scripts/build-release.mjs",
    "scripts/distribution-lib.mjs",
    "spec/distribution/source.schema.json",
  ])
    if (!seen.has(required)) throw Error("Missing source input: " + required);
  return value;
}

export function readSourceSnapshot(base) {
  const raw = readFileSync(join(base, sourceManifestName));
  if (raw.length > 4 * 1024 * 1024) throw Error("Oversized source manifest");
  return validateSource(JSON.parse(raw), base);
}

export function collectSourceInputs(base = process.cwd()) {
  if (existsSync(join(base, sourceManifestName)))
    return readSourceSnapshot(base);
  const git = (...args) => {
    const r = spawnSync(
      "git",
      [
        "-c",
        "safe.directory=" + resolve(base).replaceAll("\\", "/"),
        "-c",
        "core.excludesFile=",
        ...args,
      ],
      { cwd: base, encoding: "utf8", windowsHide: true },
    );
    if (r.error || r.status !== 0)
      throw Error(
        "Source checkout requires Git or " +
          sourceManifestName +
          ": " +
          (r.error ?? r.stderr),
      );
    return r.stdout.split("\0").filter(Boolean);
  };
  const tracked = new Set(git("ls-files", "--cached", "-z"));
  const files = git(
    "ls-files",
    "--cached",
    "--others",
    "--exclude-standard",
    "-z",
  )
    .filter((f) => isSourceFile(f, tracked.has(f)))
    .sort();
  for (const f of files) assertConfinedRegular(base, resolve(base, f));
  return validateSource(
    {
      format: "civora-source-1",
      files: files.map((f) => fileRef(join(base, f), f)),
    },
    base,
  );
}

export function sourceFingerprint(manifest = collectSourceInputs()) {
  // The generated final report is an output, excluded to avoid a circular build/test dependency.
  return sha256(
    json(
      manifest.files.filter(
        (f) => f.path !== "spec/reports/v1.0-distribution.json",
      ),
    ),
  );
}

export function validateLocalChecks(value, expectedSource) {
  if (
    value?.v !== "1" ||
    value.scope !== "local_regression" ||
    value.passed !== true ||
    value.source_sha256 !== expectedSource ||
    value.source_unchanged !== true
  )
    throw Error(
      "Successful checks bound to the exact release sources are required",
    );
  const flags = value.flags;
  if (
    !flags ||
    flags.skip_network !== false ||
    flags.skip_pwa !== false ||
    flags.distribution !== true
  )
    throw Error(
      "Full network, browser and standalone distribution checks are required",
    );
  for (const field of ["started", "completed"])
    if (
      typeof value[field] !== "string" ||
      !Number.isFinite(Date.parse(value[field]))
    )
      throw Error("Invalid check time");
  if (Date.parse(value.completed) < Date.parse(value.started))
    throw Error("Invalid check interval");
  return value;
}

// Copies only authenticated, listed files. Extra files in an extracted tree never reach the compilers.
export function copySourceSnapshot(
  base,
  destination,
  manifest = readSourceSnapshot(base),
) {
  validateSource(manifest, base);
  mkdirSync(destination, { recursive: true });
  for (const f of manifest.files) {
    const out = join(destination, f.path);
    mkdirSync(dirname(out), { recursive: true });
    copyFileSync(join(base, f.path), out);
  }
  writeFileSync(join(destination, sourceManifestName), json(manifest));
  validateSource(manifest, destination);
  return manifest;
}

export function assertConfinedRegular(base, file) {
  const dir = realpathSync(base),
    absolute = resolve(file),
    rel = relative(dir, absolute);
  if (
    rel === "" ||
    isAbsolute(rel) ||
    rel.startsWith(".." + sep) ||
    rel === ".." ||
    resolve(dir, rel) !== absolute
  )
    throw Error("File outside package staging");
  let current = dir;
  for (const part of rel.split(sep)) {
    current = resolve(current, part);
    if (lstatSync(current).isSymbolicLink())
      throw Error("Package input is a symlink");
  }
  if (!lstatSync(absolute).isFile())
    throw Error("Package input is not a regular file");
}

// This candidate intentionally has no path that turns local fixtures into a public release.
// Qualification requires authenticated campaign evidence and an independently reviewed publication workflow.
export function requireExperimental(args) {
  if (args.includes("--public"))
    throw Error(
      "Public publication blocked: v0.9 evidence, external audits, two independent build environments and independent release reviews remain required. This candidate only prepares experimental distributions.",
    );
}
