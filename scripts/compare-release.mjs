import { readFileSync, readdirSync } from "node:fs";
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import {
  sha256,
  validateManifest,
  assertConfinedRegular,
} from "./distribution-lib.mjs";

export function compareRelease(first, second) {
  const lists = [first, second].map((d) => {
    const names = readdirSync(d).sort();
    const manifest = validateManifest(
      JSON.parse(readFileSync(join(d, "release.json"))),
    );
    const expected = [
      ...manifest.files.map((f) => f.path),
      "release.json",
    ].sort();
    if (JSON.stringify(names) !== JSON.stringify(expected))
      throw Error("Unlisted release files");
    for (const f of manifest.files) {
      assertConfinedRegular(d, resolve(d, f.path));
      const bytes = readFileSync(join(d, f.path));
      if (String(bytes.length) !== f.size || sha256(bytes) !== f.sha256)
        throw Error("Release integrity mismatch: " + f.path);
    }
    return names.map((name) => ({
      name,
      sha256: sha256(readFileSync(join(d, name))),
    }));
  });
  if (JSON.stringify(lists[0]) !== JSON.stringify(lists[1]))
    throw Error("Release builds differ");
  return {
    equal: true,
    files: lists[0],
    independent_build_environments: false,
  };
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  if (process.argv.length !== 4)
    throw Error("usage: compare-release first/payload second/payload");
  console.log(
    JSON.stringify(
      compareRelease(resolve(process.argv[2]), resolve(process.argv[3])),
      null,
      2,
    ),
  );
}
