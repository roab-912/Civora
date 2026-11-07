import assert from "node:assert/strict";
import { spawnSync, spawn } from "node:child_process";
import {
  readFileSync,
  writeFileSync,
  mkdirSync,
  existsSync,
  readdirSync,
} from "node:fs";
import { resolve, join, dirname } from "node:path";
import { randomUUID } from "node:crypto";
import {
  validateManifest,
  formatCatalogue,
  requireExperimental,
  isSourceFile,
  sha256,
  readSourceSnapshot,
  copySourceSnapshot,
  sourceFingerprint,
} from "./distribution-lib.mjs";
import { compareRelease } from "./compare-release.mjs";

const root = resolve("."),
  dir = resolve(".tools/distribution-tests/" + randomUUID());
mkdirSync(dir, { recursive: true });
const goCommand =
  process.env.CIVORA_GO ??
  (existsSync(".tools/go/bin/go.exe") ? resolve(".tools/go/bin/go.exe") : "go");
const goRootProbe = spawnSync(goCommand, ["env", "GOROOT"], {
  encoding: "utf8",
  windowsHide: true,
});
if (goRootProbe.error || goRootProbe.status !== 0)
  throw Error(
    "Installed Go required: " + (goRootProbe.error ?? goRootProbe.stderr),
  );
const go = join(
  goRootProbe.stdout.trim(),
  "bin",
  process.platform === "win32" ? "go.exe" : "go",
);
const env = {
  ...process.env,
  GOPATH: join(root, ".tools/gopath"),
  GOMODCACHE: join(root, ".tools/gomodcache"),
  GOCACHE: join(root, ".tools/gocache"),
  GOFLAGS: "-buildvcs=false",
};
function run(exe, args, extra = {}) {
  const r = spawnSync(exe, args, {
    env,
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
    windowsHide: true,
    ...extra,
  });
  if (r.error || r.status !== 0)
    throw Error(
      (r.error ?? r.stderr ?? r.stdout) + "\n" + exe + " " + args.join(" "),
    );
  return r.stdout;
}
const checks = [];
function checked(name, fn) {
  fn();
  checks.push(name);
  console.log("PASS " + name);
}
checked(
  "strict manifest, unknown fields, numeric sizes, duplicate files and false capacity",
  () => {
    const m = {
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
    validateManifest(m);
    for (const bad of [
      { ...m, extra: true },
      { ...m, public_qualified: true },
      { ...m, verified_capacity_ballots: "10000000" },
      { ...m, files: [{ ...m.files[0], size: 1 }] },
      { ...m, files: [...m.files, ...m.files] },
    ])
      assert.throws(() => validateManifest(bad));
  },
);
checked("format catalogue preserves all schema IDs and bytes", () => {
  const c = formatCatalogue();
  assert.equal(c.protocol_epochs_changed, false);
  for (const f of c.schemas) {
    assert.equal(f.sha256, sha256(readFileSync(f.path)));
    assert.match(f.id, /^https:\/\/civora.invalid\/v0.1\//);
  }
});
checked(
  "source allowlist excludes untracked secrets and operational files",
  () => {
    for (const p of [
      "spec/operator-key.json",
      "spec/crypto/private.json",
      ".tools/pwa/lan-tls/ca.key",
      ".dev-host/notifications.json",
      "secret.pem",
    ])
      assert.equal(isSourceFile(p, false), false);
    assert.equal(isSourceFile("scripts/build-release.mjs", false), true);
    assert.equal(isSourceFile("internal/ledger/store.go", true), true);
  },
);
checked("public publication blocked before writing output", () => {
  assert.throws(() => requireExperimental(["--public"]), /blocked/);
  const output = join(dir, "forbidden");
  const p = spawnSync(
    process.execPath,
    ["scripts/build-release.mjs", "--public", "--out", output],
    { encoding: "utf8", windowsHide: true },
  );
  assert.equal(p.status, 1);
  assert.match(p.stderr, /Public publication blocked/);
  assert.equal(existsSync(output), false);
});
checked(
  "Go TUF adversarial, rotation, replay and abrupt process exit tests",
  () => {
    console.log(
      run(go, [
        "-C",
        "tools/distribution",
        "test",
        "./...",
        "-count=1",
        "-timeout",
        "90s",
      ]),
    );
    run(go, ["-C", "tools/distribution", "vet", "./..."]);
  },
);
const target =
  process.platform === "win32"
    ? "windows-amd64"
    : process.arch === "arm64"
      ? "linux-arm64"
      : "linux-amd64";
const epoch = new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
const outputs = {
  first: join(dir, "first"),
  second: join(dir, "standalone-source/.tools/second"),
};
for (const name of ["first", "second"]) {
  console.log("Building repeatable native package " + name);
  let cwd = root,
    buildEnv = { ...env, CIVORA_GO: go };
  if (name === "second") {
    cwd = join(dir, "standalone-source");
    copySourceSnapshot(join(dir, "first/work/source"), cwd);
    buildEnv = {
      ...env,
      CIVORA_GO: go,
      CIVORA_ARTIFACT_DIR: join(root, ".tools/semaphore"),
      PATH: dirname(process.execPath),
    };
    const probe = spawnSync("git", ["--version"], {
      env: buildEnv,
      encoding: "utf8",
      windowsHide: true,
    });
    assert.notEqual(
      probe.status,
      0,
      "Git must be unavailable in the archive rebuild test",
    );
  }
  run(
    process.execPath,
    [
      join(cwd, "scripts/build-release.mjs"),
      "--targets",
      target,
      "--out",
      outputs[name],
      "--epoch",
      epoch,
    ],
    { cwd, env: buildEnv },
  );
}
checked(
  "Git-free source snapshot rebuild matches the original native package",
  () => {
    const report = compareRelease(
      join(outputs.first, "payload"),
      join(outputs.second, "payload"),
    );
    assert.equal(report.equal, true);
    assert.equal(report.independent_build_environments, false);
  },
);
const stage = join(dir, "first/work", target),
  dist = join(
    stage,
    "bin/civdist" + (process.platform === "win32" ? ".exe" : ""),
  );
const mirror = join(dir, "mirror"),
  rootFile = join(mirror, "root.TEST_ONLY.json");
checked(
  "CLI local sealing, default refusal, authenticated manifest and every target",
  () => {
    run(dist, [
      "seal-test",
      "--payload",
      join(dir, "first/payload"),
      "--repository",
      mirror,
      "--epoch",
      epoch,
      "--allow-test-keys",
    ]);
    const pin = sha256(readFileSync(rootFile)),
      args = [
        "verify",
        "--repository",
        mirror,
        "--root",
        rootFile,
        "--root-sha256",
        pin,
        "--state",
        join(dir, "trust.db"),
      ];
    const refused = spawnSync(dist, args, {
      encoding: "utf8",
      windowsHide: true,
    });
    assert.equal(refused.status, 1);
    assert.match(refused.stderr, /TEST KEYS/);
    const report = JSON.parse(run(dist, [...args, "--allow-test-keys"]));
    assert.equal(report.mode, "development_TEST_ONLY");
    assert.equal(report.protocol_activation, false);
    assert.equal(
      report.targets.length,
      readdirSync(join(dir, "first/payload")).length,
    );
    run(dist, [...args, "--allow-test-keys"]);
  },
);
checked(
  "bundled independent citizen audit runs without source tree or npm dependencies",
  () => {
    const output = run(
      process.execPath,
      [
        join(stage, "tools/audit-citizen.mjs"),
        "-input",
        join(root, "spec/fixtures/v0.6/record"),
        "-manifest",
        join(root, "spec/fixtures/v0.6/network.json"),
      ],
      { cwd: stage },
    );
    assert.match(output, /certified/);
  },
);
checked(
  "bundled encrypted audit runs without source tree or npm dependencies",
  () => {
    const archive = join(dir, "archive.json"),
      store = join(dir, "replay");
    const node = join(
      stage,
      "bin/civnode" + (process.platform === "win32" ? ".exe" : ""),
    );
    const crypto = join(
      stage,
      "bin/civcrypto" + (process.platform === "win32" ? ".exe" : ""),
    );
    run(node, [
      "replay",
      "-fixture",
      join(root, "spec/fixtures/v0.4/replay-mixed.json"),
      "-store",
      store,
    ]);
    run(node, [
      "replay",
      "-fixture",
      join(root, "spec/fixtures/v0.4/replay-mixed.json"),
      "-store",
      store,
    ]);
    run(crypto, ["export", "-store", store, "-out", archive]);
    run(crypto, ["audit", "-in", archive]);
    const output = run(
      process.execPath,
      [join(stage, "tools/audit-electionguard.mjs"), "-in", archive],
      { cwd: stage },
    );
    assert.match(output, /verified|passed|valid|audit/i);
  },
);
checked("package contains no runtime TLS, VAPID or account stores", () => {
  const walk = (d, prefix = "") =>
    readdirSync(d, { withFileTypes: true }).flatMap((f) =>
      f.isDirectory()
        ? walk(join(d, f.name), prefix + f.name + "/")
        : [prefix + f.name],
    );
  for (const p of walk(stage))
    assert.doesNotMatch(
      p,
      /(?:^|\/)(?:node_modules|\.dev-host|\.dev-store|sessions|lan-tls|notifications)(?:\/|$)|\.(?:pem|key|db|bolt)$/i,
    );
});
const hostConfig = join(dir, "host.json");
writeFileSync(
  hostConfig,
  JSON.stringify({
    networks: [],
    notifications: { directory: join(dir, "notifications-runtime") },
  }),
);
const host = spawn(
  process.execPath,
  [
    join(stage, "tools/host-pwa.mjs"),
    "--config",
    hostConfig,
    "--public-dir",
    "web",
    "--artifact-dir",
    "artifacts/semaphore",
    "--port",
    "0",
  ],
  { cwd: stage, env, windowsHide: true, stdio: ["ignore", "pipe", "pipe"] },
);
try {
  const origin = await new Promise((yes, no) => {
    let output = "",
      errors = "";
    const timer = setTimeout(
      () => no(Error("Bundled host startup timeout: " + errors)),
      15000,
    );
    host.stderr.on("data", (b) => (errors += b));
    host.once("exit", (code) => {
      clearTimeout(timer);
      no(Error("Bundled host exited " + code + ": " + errors));
    });
    host.stdout.on("data", (b) => {
      output += b;
      const match = /http:\/\/127.0.0.1:\d+/.exec(output);
      if (match) {
        clearTimeout(timer);
        yes(match[0]);
      }
    });
  });
  const page = await fetch(origin);
  assert.equal(page.status, 200);
  assert.match(await page.text(), /Civora/);
  const wasm = await fetch(origin + "/crypto/semaphore/16/semaphore.wasm");
  assert.equal(wasm.status, 200);
  const lock = JSON.parse(
    readFileSync(join(stage, "spec/crypto/semaphore-v4.lock.json")),
  );
  assert.equal(
    sha256(new Uint8Array(await wasm.arrayBuffer())),
    lock.depths[0].wasm.sha256,
  );
  checks.push(
    "bundled PWA host serves browser assets and pinned proof material without source checkout",
  );
  console.log("PASS " + checks.at(-1));
} finally {
  host.kill();
  await new Promise((done) =>
    host.exitCode !== null ? done() : host.once("exit", done),
  );
}
const report = {
  milestone: "v1.0",
  status: "local_experimental_distribution_tested",
  public_qualified: false,
  independent_build_environments: false,
  passed: true,
  source_sha256: sourceFingerprint(
    readSourceSnapshot(join(dir, "first/work/source")),
  ),
  native_package_sha256: JSON.parse(
    readFileSync(join(dir, "first/payload/release.json")),
  ).files.find((f) => f.path === `civora-1.0.0-${target}.zip`).sha256,
  epoch,
  checks,
  output: dir,
};
writeFileSync(join(dir, "report.json"), JSON.stringify(report, null, 2) + "\n");
const reportArg = process.argv.indexOf("--report");
if (reportArg >= 0) {
  if (!process.argv[reportArg + 1]) throw Error("Missing --report");
  const file = resolve(process.argv[reportArg + 1]);
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, JSON.stringify(report, null, 2) + "\n");
}
console.log(JSON.stringify(report, null, 2));
