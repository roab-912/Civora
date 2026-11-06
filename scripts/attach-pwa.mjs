// Preserve an already running development ledger and its guardian session while
// serving the v0.8 frontend and independent notification store on another port.
import { mkdirSync, writeFileSync } from "node:fs";
import { resolve, join } from "node:path";
const args = process.argv.slice(2),
  get = (key, fallback) => {
    const i = args.indexOf(key);
    return i < 0 ? fallback : args[i + 1];
  },
  upstream = new URL(get("--upstream", "http://127.0.0.1:8790")),
  directory = resolve(get("--directory", ".tools/pwa/v0.8/attached"));
if (
  upstream.protocol !== "http:" ||
  upstream.hostname !== "127.0.0.1" ||
  upstream.username ||
  upstream.password ||
  upstream.pathname !== "/" ||
  upstream.search ||
  upstream.hash
)
  throw Error("Explicit existing loopback workshop required");
const response = await fetch(new URL("/pwa-config.json", upstream), {
  redirect: "error",
  signal: AbortSignal.timeout(10000),
});
if (!response.ok) throw Error("Existing workshop unavailable");
const config = await response.json();
if (!config.development || !Array.isArray(config.networks))
  throw Error(
    "This attachment command is reserved for an existing development workshop",
  );
mkdirSync(directory, { recursive: true, mode: 0o700 });
const file = join(directory, "networks.json");
writeFileSync(
  file,
  JSON.stringify(
    {
      development: true,
      development_upstream: upstream.origin,
      networks: config.networks.map((n) => ({
        ...n,
        api_roots: n.api_roots.map((root) =>
          new URL(root, upstream).href.replace(/\/$/, ""),
        ),
      })),
      notifications: {
        directory: join(directory, "notifications"),
        ...(get("--contact") ? { contact: get("--contact") } : {}),
      },
    },
    null,
    2,
  ),
);
process.argv = [
  process.execPath,
  resolve("scripts/host-pwa.mjs"),
  "--config",
  file,
  "--port",
  get("--port", "8794"),
];
await import("./host-pwa.mjs");
