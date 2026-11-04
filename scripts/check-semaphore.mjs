import { readFileSync, existsSync } from "node:fs";
import { createHash } from "node:crypto";
import { semaphoreLock } from "../dist/web/src/semaphore-lock.js";
const lock = JSON.parse(
  readFileSync("spec/crypto/semaphore-v4.lock.json", "utf8"),
);
if (JSON.stringify(lock) !== JSON.stringify(semaphoreLock))
  throw Error("Browser and Go artifact locks differ");
for (const name of ["identity", "group", "proof"]) {
  const pkg = JSON.parse(
    readFileSync(
      "node_modules/@semaphore-protocol/" + name + "/package.json",
      "utf8",
    ),
  );
  if (pkg.version !== lock.sdk_version) throw Error("SDK version mismatch");
}
const sdk = JSON.parse(
  readFileSync(
    "node_modules/@semaphore-protocol/proof/src/verification-keys.json",
    "utf8",
  ),
);
const sha = (bytes) => createHash("sha256").update(bytes).digest("hex");
for (const entry of lock.depths) {
  const bytes = readFileSync(entry.verification_key.file);
  if (
    sha(bytes) !== entry.verification_key.sha256 ||
    String(bytes.length) !== entry.verification_key.size
  )
    throw Error("Pinned verifier hash mismatch");
  const expected = {
    ...sdk,
    vk_delta_2: sdk.vk_delta_2[Number(entry.depth) - 1],
    IC: sdk.IC[Number(entry.depth) - 1],
  };
  if (JSON.stringify(JSON.parse(bytes)) !== JSON.stringify(expected))
    throw Error("Verifier key differs from the pinned SDK");
  for (const name of ["wasm", "zkey"]) {
    const file = ".tools/semaphore/" + entry[name].file;
    if (!existsSync(file)) {
      if (process.argv.includes("--require-artifacts"))
        throw Error("Run npm run artifacts:fetch first");
      continue;
    }
    const data = readFileSync(file);
    if (
      sha(data) !== entry[name].sha256 ||
      String(data.length) !== entry[name].size
    )
      throw Error("Artifact digest/size mismatch: " + file);
  }
}
console.log(
  "Semaphore SDK, verifier keys and installed artifacts match the committed lock.",
);
