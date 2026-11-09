import { build } from "esbuild";
import { Ajv2020 } from "ajv/dist/2020.js";
import standalone from "ajv/dist/standalone/index.js";
import {
  readFileSync,
  readdirSync,
  mkdirSync,
  writeFileSync,
  copyFileSync,
} from "node:fs";
import { resolve, join, dirname } from "node:path";
import { createHash } from "node:crypto";
const outIndex = process.argv.indexOf("--out");
if (outIndex >= 0 && !process.argv[outIndex + 1]) throw Error("Missing --out");
const directory = resolve(
  outIndex < 0 ? ".tools/pwa/v0.8/public" : process.argv[outIndex + 1],
);
mkdirSync(directory, { recursive: true });
const ajv = new Ajv2020({
    strict: true,
    allErrors: false,
    code: { source: true, esm: true },
  }),
  exports = {};
for (const f of readdirSync("spec/schemas").filter((f) =>
  f.endsWith(".schema.json"),
)) {
  const s = JSON.parse(readFileSync("spec/schemas/" + f, "utf8"));
  ajv.addSchema(s);
  if (f !== "protocol.schema.json")
    exports["v_" + f.replace(".schema.json", "")] = s.$id;
}
const generated = resolve(".tools/pwa/validators.mjs");
mkdirSync(dirname(generated), { recursive: true });
writeFileSync(generated, standalone(ajv, exports));
const result = await build({
  entryPoints: {
    app: "web/src/pwa/app.ts",
    worker: "web/src/pwa/worker.ts",
    sw: "web/src/pwa/service-worker.ts",
  },
  outdir: directory,
  bundle: true,
  platform: "browser",
  format: "esm",
  target: ["chrome120", "safari17.4"],
  minify: true,
  metafile: true,
  sourcemap: false,
  inject: ["web/src/pwa/buffer.ts"],
  plugins: [
    {
      name: "browser-protocol",
      setup(b) {
        b.onResolve({ filter: /^(\.\/|\.\.\/)protocol\.js$/ }, (args) => {
          if (
            !args.importer.endsWith("pwa/protocol.ts") &&
            !args.importer.endsWith("pwa/network.ts") &&
            !args.importer.endsWith("pwa/vault.ts") &&
            !args.importer.endsWith("pwa/app.ts") &&
            !args.importer.endsWith("pwa/worker.ts")
          )
            return { path: resolve("web/src/pwa/protocol.ts") };
        });
        b.onResolve({ filter: /^civic-validators$/ }, () => ({
          path: generated,
        }));
      },
    },
  ],
});
for (const input of Object.keys(result.metafile.inputs))
  if (input.includes("node:") || input.includes("web/src/protocol.ts"))
    throw Error("Node reference code leaked into browser bundle: " + input);
for (const file of [
  "app.css",
  "manifest.webmanifest",
  "icon.svg",
  "icon-192.png",
  "icon-512.png",
])
  copyFileSync(join("web/pwa", file), join(directory, file));
const releaseVersion = JSON.parse(readFileSync("package.json", "utf8")).version;
if (!/^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/.test(releaseVersion))
  throw Error("Invalid release version");
writeFileSync(
  join(directory, "index.html"),
  readFileSync("web/pwa/index.html", "utf8").replaceAll(
    "__CIVORA_RELEASE_VERSION__",
    releaseVersion,
  ),
);
const assets = {};
for (const name of readdirSync(directory).sort()) {
  if (name === "build.json") continue;
  const b = readFileSync(join(directory, name));
  assets[name] = {
    sha256: createHash("sha256").update(b).digest("hex"),
    size: String(b.length),
  };
}
writeFileSync(
  join(directory, "build.json"),
  JSON.stringify({ version: "0.8.0", assets }, null, 2),
);
console.log(
  "PWA built with local dependencies, precompiled schemas and no source maps: " +
    directory,
);
