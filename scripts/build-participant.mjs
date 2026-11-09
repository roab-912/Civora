import { spawnSync } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import {
  readFileSync,
  readdirSync,
  existsSync,
  mkdirSync,
  writeFileSync,
  renameSync,
} from "node:fs";
import { resolve, join } from "node:path";

const hash = (raw) => createHash("sha256").update(raw).digest("hex");
export function buildParticipantBinaries() {
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
    CGO_ENABLED: "0",
  };
  const run = (args, extra = {}) => {
    const result = spawnSync(go, args, {
      env: { ...env, ...extra },
      encoding: "utf8",
      windowsHide: true,
    });
    if (result.error || result.status !== 0)
      throw Error(result.error?.message ?? result.stderr);
    return result.stdout.trim();
  };
  const inputs = ["go.mod", "go.sum", "spec/schema.go"];
  const visit = (directory) => {
    for (const entry of readdirSync(directory, { withFileTypes: true }).sort(
      (a, b) => (a.name < b.name ? -1 : 1),
    )) {
      const file = directory + "/" + entry.name;
      if (entry.isDirectory()) visit(file);
      else if (/\.(go|json)$/.test(entry.name)) inputs.push(file);
    }
  };
  for (const directory of ["cmd/civparticipant", "internal", "spec/schemas"])
    visit(directory);
  const source = hash(
    JSON.stringify([
      run(["version"]),
      inputs.sort().map((file) => [file, hash(readFileSync(file))]),
    ]),
  );
  const directory = resolve(".tools/network/participants", source);
  const load = () => {
    const record = JSON.parse(
      readFileSync(join(directory, "build.json"), "utf8"),
    );
    for (const entry of Object.values(record.targets)) {
      if (hash(readFileSync(join(directory, entry.name))) !== entry.sha256)
        throw Error("Observer binary cache integrity failure");
      entry.file = join(directory, entry.name);
    }
    return record;
  };
  if (existsSync(directory)) return load();
  const stage = directory + "." + randomUUID();
  mkdirSync(stage, { recursive: true });
  const record = {
    format: "civora-observer-build-1",
    source_sha256: source,
    targets: {},
  };
  for (const target of ["windows-amd64", "linux-amd64"]) {
    const [GOOS, GOARCH] = target.split("-");
    const name = "civora-reseau-" + target + (GOOS === "windows" ? ".exe" : "");
    run(
      [
        "build",
        "-trimpath",
        "-buildvcs=false",
        "-mod=readonly",
        "-ldflags=-buildid=",
        "-o",
        join(stage, name),
        "./cmd/civparticipant",
      ],
      { GOOS, GOARCH },
    );
    record.targets[target] = {
      name,
      sha256: hash(readFileSync(join(stage, name))),
    };
  }
  writeFileSync(join(stage, "build.json"), JSON.stringify(record, null, 2));
  try {
    renameSync(stage, directory);
  } catch (error) {
    if (!existsSync(directory)) throw error;
  }
  return load();
}

if (process.argv.includes("--build-participant"))
  console.log(JSON.stringify(buildParticipantBinaries(), null, 2));
