// Download only the artifacts identified by the reviewed, committed lock.
import {
  readFileSync,
  mkdirSync,
  writeFileSync,
  renameSync,
  existsSync,
  rmSync,
} from "node:fs";
import { createHash } from "node:crypto";
const lock = JSON.parse(
  readFileSync("spec/crypto/semaphore-v4.lock.json", "utf8"),
);
const sha = (bytes) => createHash("sha256").update(bytes).digest("hex");
for (const depth of lock.depths)
  for (const kind of ["wasm", "zkey"]) {
    const file = depth[kind],
      path = ".tools/semaphore/" + file.file;
    if (
      existsSync(path) &&
      sha(readFileSync(path)) === file.sha256 &&
      String(readFileSync(path).length) === file.size
    )
      continue;
    mkdirSync(`.tools/semaphore/${depth.depth}`, { recursive: true });
    const response = await fetch(file.source);
    if (!response.ok)
      throw Error(`download ${response.status}: ${file.source}`);
    const bytes = new Uint8Array(await response.arrayBuffer());
    if (sha(bytes) !== file.sha256 || String(bytes.length) !== file.size)
      throw Error("Artifact digest/size mismatch: " + file.source);
    const temporary = path + ".pending";
    writeFileSync(temporary, bytes);
    if (existsSync(path)) rmSync(path);
    renameSync(temporary, path);
  }
