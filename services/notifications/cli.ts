import { createServer } from "node:http";
import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { NotificationService } from "./service.js";
const args = process.argv.slice(2),
  get = (key: string, fallback: string) => {
    const i = args.indexOf(key);
    return i < 0 ? fallback : args[i + 1]!;
  },
  file = resolve(get("--config", "pwa-networks.json")),
  config = JSON.parse(readFileSync(file, "utf8"));
const watches = config.networks.map((entry: any) => ({
  ...entry,
  manifest:
    typeof entry.manifest === "string"
      ? JSON.parse(readFileSync(resolve(dirname(file), entry.manifest), "utf8"))
      : entry.manifest,
}));
for (const watch of watches)
  watch.api_roots ??= watch.manifest.main.origins.map((o: any) => o.api);
const service = new NotificationService({
  directory: resolve(get("--store", ".dev-host/notifications")),
  watches,
  contact: get("--contact", "mailto:civora-development@example.invalid"),
});
const server = createServer(async (req, res) => {
  if (
    !(await service.handle(
      req,
      res,
      new URL(req.url!, "http://localhost").pathname,
    ))
  ) {
    res.writeHead(404);
    res.end();
  }
});
server.listen(Number(get("--port", "8793")), "127.0.0.1", () =>
  console.log(
    "Notifications API http://127.0.0.1:" +
      (server.address() as { port: number }).port +
      "/notifications",
  ),
);
async function close() {
  server.closeAllConnections();
  server.close();
  await service.close();
}
process.on("SIGINT", () => void close());
process.on("SIGTERM", () => void close());
