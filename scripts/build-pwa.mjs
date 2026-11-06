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
import { resolve, join } from "node:path";
import { createHash } from "node:crypto";
const directory = resolve(".tools/pwa/public");
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
writeFileSync(generated, standalone(ajv, exports));
const result = await build({
  entryPoints: { app: "web/src/pwa/app.ts", worker: "web/src/pwa/worker.ts" },
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
  "index.html",
  "app.css",
  "manifest.webmanifest",
  "icon.svg",
  "icon-192.png",
  "icon-512.png",
  "sw.js",
])
  copyFileSync(join("web/pwa", file), join(directory, file));
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
  JSON.stringify({ version: "0.7.0", assets }, null, 2),
);
console.log(
  "PWA built with local dependencies, precompiled schemas and no source maps: " +
    directory,
);
