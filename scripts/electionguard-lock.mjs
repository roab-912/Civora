import { readFileSync, writeFileSync } from "node:fs";
import { canonical, sha } from "../dist/web/src/protocol.js";
const files = [
  "internal/electionguard/bundle.go",
  "internal/electionguard/crypto.go",
  "internal/electionguard/scale.go",
  "web/src/electionguard.ts",
  "web/src/electionguard-scale.ts",
  "web/src/electionguard-parameters.ts",
  "tools/electionguard/CivicVectors.kt",
  "tools/electionguard/civic-task.gradle.kts",
  "spec/crypto/electionguard-v2.parameters.json",
  "spec/crypto/electionguard-v2.manifest-template.json",
];
const pin = (file) => {
  const b = readFileSync(file);
  return { file, size: String(b.length), sha256: sha(b).toString("hex") };
};
export function implementationDigest() {
  return sha(
    Buffer.from(
      canonical(
        files.slice(0, 3).map((file) => ({ file, sha256: pin(file).sha256 })),
      ),
    ),
  ).toString("hex");
}
const data = {
  suite: "eg-2.0-civic-1",
  design_version: "2.0.0",
  serialization_reference: "0.0.1_with_explicit_Civic_compact_mapping",
  encoding: "eg-civic-compact-1",
  development_interop_verified: true,
  public_pilot_qualified: false,
  implementation_sha256: implementationDigest(),
  artifacts: files.slice(-2).map((file) => pin(file).sha256),
  source_artifacts: files.map(pin),
  vectors: [
    "crypto",
    "reject-crypto",
    "scale",
    "memberships",
    "oracle.TEST_ONLY",
  ].map((n) => pin(`spec/fixtures/v0.4/${n}.json`)),
  sdk: {
    repository:
      "https://github.com/votingworks/electionguard-kotlin-multiplatform",
    commit: "194b9c788ccaecda8719c227bd41ce57a3b857be",
    library_version: "2.0.4-SNAPSHOT",
    archive: {
      url: "https://codeload.github.com/votingworks/electionguard-kotlin-multiplatform/zip/194b9c788ccaecda8719c227bd41ce57a3b857be",
      size: "18297329",
      sha256:
        "f79eaa269238f895b43e76ff43bef763afecb99cf6636879123ca9e7c24b0adf",
    },
    gradle_version: "8.6",
    gradle_sha256:
      "9631d53cf3e74bfa726893aee1f8994fee4e060c401335946dba2156f440f24c",
    jdk_windows: {
      url: "https://github.com/adoptium/temurin17-binaries/releases/download/jdk-17.0.21%2B9/OpenJDK17U-jdk_x64_windows_hotspot_17.0.21_9.zip",
      sha256:
        "98518ecf554465057ee5509462631deeee374bc368c12a411d225cd29381f84b",
      directory: "jdk-17.0.21+9",
    },
  },
  qualification_scope:
    "development_interoperability_and_replay_only_not_external_security_audit_or_public_capacity",
};
const path = "spec/crypto/electionguard-v2.lock.json",
  raw = canonical(data);
if (process.argv.includes("--write")) writeFileSync(path, raw);
else if (readFileSync(path, "utf8") !== raw)
  throw Error(
    "ElectionGuard implementation/artifact/vector lock differs; investigate before explicit --write",
  );
