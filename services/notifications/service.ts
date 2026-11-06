import { mkdirSync, readFileSync, writeFileSync, existsSync } from "node:fs";
import { join } from "node:path";
import type { IncomingMessage, ServerResponse } from "node:http";
import { canonical, parse, validate } from "../../web/src/protocol.js";
import { newVapid, vapidPublic } from "./crypto.js";
import { validateSubscription, WebPushSender } from "./transport.js";
import { NotificationStore } from "./store.js";
import { FinalizedNewsSource, type Watch } from "./source.js";
import type { Sender, Subscription } from "./types.js";
export interface ServiceOptions {
  directory: string;
  watches: Watch[];
  contact?: string;
  sender?: Sender;
  validateEndpoint?: typeof validateSubscription;
  vapidSecret?: string;
  clock?: () => number;
  sources?: NewsSource[];
}
export interface NewsSource {
  watch: { manifest: Record<string, any> };
  sync(store: NotificationStore, now: number): Promise<number>;
}
export class NotificationService {
  readonly store: NotificationStore;
  readonly publicKey: string;
  private readonly sources: NewsSource[];
  private readonly sender: Sender;
  private readonly clock: () => number;
  private readonly validateEndpoint: typeof validateSubscription;
  private busy: Promise<void> | null = null;
  private sending: Promise<void> | null = null;
  private closing = false;
  private timer: ReturnType<typeof setInterval>;
  private available = new Map<string, boolean>();
  private rate = new Map<string, { start: number; count: number }>();
  constructor(options: ServiceOptions) {
    for (const watch of options.watches)
      validate("citizen_manifest", watch.manifest);
    mkdirSync(options.directory, { recursive: true, mode: 0o700 });
    const file = join(options.directory, "vapid-private.json");
    if (!existsSync(file))
      writeFileSync(
        file,
        JSON.stringify({
          purpose: "VAPID_ONLY_LOCAL_PRIVATE",
          secret: options.vapidSecret ?? newVapid(),
        }),
        { mode: 0o600 },
      );
    const secret = (
      JSON.parse(readFileSync(file, "utf8")) as { secret: string }
    ).secret;
    this.publicKey = vapidPublic(secret);
    const db = join(options.directory, "notifications.db");
    if (!existsSync(db)) writeFileSync(db, "", { mode: 0o600 });
    this.store = new NotificationStore(
      db,
      options.watches.map((w) => w.manifest.network_id as string),
    );
    for (const watch of options.watches)
      this.store.pin(
        watch.manifest.network_id,
        watch.manifest.main.genesis.sha256,
      );
    this.sources =
      options.sources ?? options.watches.map((w) => new FinalizedNewsSource(w));
    this.sender =
      options.sender ??
      new WebPushSender(
        secret,
        options.contact ?? "mailto:civora-development@example.invalid",
      );
    this.clock = options.clock ?? Date.now;
    this.validateEndpoint = options.validateEndpoint ?? validateSubscription;
    this.timer = setInterval(() => void this.tick(), 1000);
    this.timer.unref();
  }
  tick(): Promise<void> {
    if (this.busy) return this.busy;
    this.busy = (async () => {
      for (const source of this.sources)
        try {
          await source.sync(this.store, this.clock());
          this.available.set(source.watch.manifest.network_id, true);
        } catch {
          this.available.set(source.watch.manifest.network_id, false);
        }
    })()
      .catch(() => {
        /* No endpoint or subscription data in logs. */
      })
      .finally(() => {
        this.busy = null;
      });
    void this.busy.then(() => {
      if (this.closing || this.sending) return;
      this.sending = this.store
        .deliver(this.sender, this.clock, 10)
        .then(() => {})
        .catch(() => {})
        .finally(() => {
          this.sending = null;
        });
    });
    return this.busy;
  }
  async close() {
    this.closing = true;
    clearInterval(this.timer);
    await this.busy;
    await this.sending;
    this.store.close();
  }
  async handle(
    req: IncomingMessage,
    res: ServerResponse,
    path: string,
  ): Promise<boolean> {
    if (!path.startsWith("/notifications/")) return false;
    const send = (status: number, value: unknown) => {
      res.writeHead(status, {
        "Content-Type": "application/json; charset=utf-8",
        "Cache-Control": "no-store",
        "X-Content-Type-Options": "nosniff",
      });
      res.end(canonical(value));
    };
    const url = new URL(req.url!, "http://localhost");
    try {
      if (path === "/notifications/v1/config" && req.method === "GET") {
        send(200, {
          v: "1",
          enabled: true,
          vapid_public_key: this.publicKey,
          networks: this.store.networks,
        });
        return true;
      }
      if (path === "/notifications/v1/news" && req.method === "GET") {
        if (this.busy) await this.busy;
        await this.tick();
        const network = url.searchParams.get("network_id") ?? "",
          limit = Number(url.searchParams.get("limit") ?? "50");
        send(200, {
          v: "1",
          network_id: network,
          ...this.store.news(
            network,
            url.searchParams.get("cursor") ?? "",
            limit,
          ),
          source_available: this.available.get(network) ?? false,
        });
        return true;
      }
      if (req.method !== "POST") {
        send(405, { code: "ERR_METHOD" });
        return true;
      }
      if (
        req.headers.origin &&
        req.headers.origin !== `http://${req.headers.host}` &&
        req.headers.origin !== `https://${req.headers.host}`
      ) {
        send(403, { code: "ERR_ORIGIN" });
        return true;
      }
      if (req.headers["sec-fetch-site"] === "cross-site") {
        send(403, { code: "ERR_ORIGIN" });
        return true;
      }
      const chunks: Buffer[] = [];
      let size = 0;
      for await (const chunk of req) {
        size += chunk.length;
        if (size > 8192) throw Error("ERR_CAPACITY");
        chunks.push(Buffer.from(chunk));
      }
      const value = parse(Buffer.concat(chunks));
      if (path === "/notifications/v1/subscriptions") {
        const input = validate<Subscription>("push_subscription", value),
          ip = req.socket.remoteAddress ?? "unknown",
          now = this.clock();
        let bucket = this.rate.get(ip);
        if (!bucket || now - bucket.start > 3600000) {
          bucket = { start: now, count: 0 };
          this.rate.set(ip, bucket);
        }
        if (++bucket.count > 60) {
          send(429, { code: "ERR_RATE_LIMIT" });
          return true;
        }
        if (this.rate.size > 10000) this.rate.clear();
        await this.validateEndpoint(input.keys, input.endpoint);
        send(200, { v: "1", ...this.store.subscribe(input) });
        return true;
      }
      if (path === "/notifications/v1/unsubscribe") {
        const input = validate<{
          subscription_id: string;
          unsubscribe_secret: string;
        }>("push_unsubscribe", value);
        this.store.unsubscribe(input.subscription_id, input.unsubscribe_secret);
        send(200, { v: "1", removed: true });
        return true;
      }
      send(404, { code: "ERR_NOT_FOUND" });
      return true;
    } catch (e) {
      const message = e instanceof Error ? e.message : "ERR_NOTIFICATIONS";
      const code = message.startsWith("ERR_")
        ? message.split(" ")[0]!
        : "ERR_NOTIFICATIONS";
      send(code === "ERR_SUBSCRIPTION_AUTH" ? 403 : 400, { code });
      return true;
    }
  }
}
