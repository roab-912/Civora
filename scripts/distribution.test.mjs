import { test } from "node:test";
import assert from "node:assert/strict";
import {
  mkdtempSync,
  mkdirSync,
  writeFileSync,
  readFileSync,
  existsSync,
  rmSync,
} from "node:fs";
import { join, dirname, resolve, basename } from "node:path";
import { tmpdir } from "node:os";
import {
  fileRef,
  json,
  copySourceSnapshot,
  readSourceSnapshot,
  sourceManifestName,
  sourceFingerprint,
  validateLocalChecks,
  validateManifest,
} from "./distribution-lib.mjs";

function source(t) {
  const base = mkdtempSync(join(tmpdir(), "civora-source-"));
  t.after(() => {
    assert.equal(dirname(resolve(base)), resolve(tmpdir()));
    assert.ok(basename(base).startsWith("civora-source-"));
    rmSync(base, { recursive: true, force: true });
  });
  const names = [
    "package.json",
    "package-lock.json",
    "go.mod",
    "go.sum",
    "tsconfig.json",
    "scripts/build-release.mjs",
    "scripts/distribution-lib.mjs",
    "spec/distribution/source.schema.json",
  ].sort();
  for (const f of names) {
    const dest = join(base, f);
    mkdirSync(dirname(dest), { recursive: true });
    writeFileSync(
      dest,
      f.endsWith("source.schema.json")
        ? readFileSync("spec/distribution/source.schema.json")
        : "test source",
    );
  }
  const manifest = {
    format: "civora-source-1",
    files: names.map((f) => fileRef(join(base, f), f)),
  };
  writeFileSync(join(base, sourceManifestName), json(manifest));
  return { base, manifest };
}

test("source snapshot copies exact bytes, keeps its identity and excludes unlisted compiler inputs", (t) => {
  const { base, manifest } = source(t),
    dest = join(base, "copy");
  writeFileSync(join(base, "scripts/extra.mjs"), "must not be compiled");
  copySourceSnapshot(base, dest);
  assert.deepEqual(readSourceSnapshot(dest), manifest);
  assert.equal(existsSync(join(dest, "scripts/extra.mjs")), false);
  assert.equal(
    sourceFingerprint(readSourceSnapshot(dest)),
    sourceFingerprint(manifest),
  );
});
test("source snapshot refuses corrupted or missing bytes", (t) => {
  const { base } = source(t);
  writeFileSync(join(base, "go.mod"), "altered");
  assert.throws(() => readSourceSnapshot(base), /integrity/);
  assert.throws(() => copySourceSnapshot(base, join(base, "out")), /integrity/);
  assert.equal(existsSync(join(base, "out")), false);
});
test("source snapshot rejects traversal, extra fields, duplicate case aliases and unsorted entries", (t) => {
  const { base, manifest } = source(t);
  for (const bad of [
    { ...manifest, unknown: true },
    {
      ...manifest,
      files: [
        { ...manifest.files[0], path: "../go.mod" },
        ...manifest.files.slice(1),
      ],
    },
    {
      ...manifest,
      files: [
        ...manifest.files,
        { ...manifest.files[0], path: manifest.files[0].path.toUpperCase() },
      ],
    },
    { ...manifest, files: [...manifest.files].reverse() },
  ]) {
    writeFileSync(join(base, sourceManifestName), json(bad));
    assert.throws(() => readSourceSnapshot(base));
  }
});
test("local finalization requires complete checks tied to the exact source", () => {
  const hash = "a".repeat(64),
    record = {
      v: "1",
      scope: "local_regression",
      passed: true,
      source_sha256: hash,
      source_unchanged: true,
      started: "2026-11-07T10:00:00Z",
      completed: "2026-11-07T11:00:00Z",
      flags: { skip_network: false, skip_pwa: false, distribution: true },
    };
  validateLocalChecks(record, hash);
  for (const bad of [
    { ...record, passed: false },
    { ...record, passed: "true" },
    { ...record, source_sha256: "b".repeat(64) },
    { ...record, source_unchanged: false },
    { ...record, flags: { ...record.flags, skip_network: true } },
    { ...record, flags: { ...record.flags, skip_pwa: true } },
    { ...record, flags: { ...record.flags, distribution: false } },
    { ...record, completed: "invalid" },
    { ...record, completed: "2026-11-06T00:00:00Z" },
  ])
    assert.throws(() => validateLocalChecks(bad, hash));
});
test("final experimental manifest cannot claim public qualification or impossible time", () => {
  const manifest = {
    schema_version: "1",
    release_version: "1.0.0",
    channel: "experimental",
    public_qualified: false,
    verified_capacity_ballots: null,
    created: "2026-11-07T12:00:00Z",
    targets: ["windows-amd64"],
    files: [{ path: "sample.zip", sha256: "a".repeat(64), size: "1" }],
    protocol_activation: false,
  };
  validateManifest(manifest);
  for (const bad of [
    { ...manifest, public_qualified: true },
    { ...manifest, verified_capacity_ballots: "10000000" },
    { ...manifest, created: "2026-99-07T12:00:00Z" },
    { ...manifest, release_version: "1.0.0-rc.1" },
  ])
    assert.throws(() => validateManifest(bad));
});
