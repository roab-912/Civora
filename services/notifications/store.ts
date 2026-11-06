import { DatabaseSync } from "node:sqlite";
import { createHash, timingSafeEqual } from "node:crypto";
import { canonical } from "../../web/src/codec.js";
import { domain, validate } from "../../web/src/protocol.js";
import type {
  Subscription,
  StoredSubscription,
  News,
  Message,
  Sender,
} from "./types.js";
export const secretHash = (id: string, secret: string) =>
  domain("push/unsubscribe", [id, secret]);
interface Job {
  subscription_id: string;
  event_id: string;
  attempt: number;
  message: string;
  expires: number;
}
export class NotificationStore {
  readonly db: DatabaseSync;
  constructor(
    path: string,
    readonly networks: readonly string[],
  ) {
    this.db = new DatabaseSync(path);
    this.db.exec(
      "PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;",
    );
    this.db
      .exec(`CREATE TABLE IF NOT EXISTS subscriptions(id TEXT PRIMARY KEY,value TEXT NOT NULL);
      CREATE TABLE IF NOT EXISTS tombstones(id TEXT PRIMARY KEY,hash TEXT NOT NULL);
      CREATE TABLE IF NOT EXISTS news(id TEXT PRIMARY KEY,network TEXT NOT NULL,poll TEXT NOT NULL,height TEXT NOT NULL,value TEXT NOT NULL,UNIQUE(network,poll));
      CREATE TABLE IF NOT EXISTS cursors(network TEXT PRIMARY KEY,value TEXT NOT NULL);
      CREATE TABLE IF NOT EXISTS clocks(network TEXT PRIMARY KEY,height TEXT NOT NULL,time TEXT NOT NULL);
      CREATE TABLE IF NOT EXISTS pins(network TEXT PRIMARY KEY,genesis TEXT NOT NULL);
      CREATE TABLE IF NOT EXISTS jobs(subscription_id TEXT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,event_id TEXT NOT NULL REFERENCES news(id),state TEXT NOT NULL,attempt INTEGER NOT NULL DEFAULT 0,next INTEGER NOT NULL,expires INTEGER NOT NULL,lease INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(subscription_id,event_id));`);
  }
  close() {
    this.db.close();
  }
  observeClock(network: string, height: string, time: string) {
    if (
      !this.networks.includes(network) ||
      !/^\d+$/.test(height) ||
      !/^\d+$/.test(time)
    )
      throw Error("ERR_NETWORK");
    this.transaction(() => {
      const old = this.db
        .prepare("SELECT height,time FROM clocks WHERE network=?")
        .get(network) as { height: string; time: string } | undefined;
      if (
        old &&
        (BigInt(height) < BigInt(old.height) || BigInt(time) < BigInt(old.time))
      )
        throw Error("ERR_SOURCE_ROLLBACK");
      this.db
        .prepare(
          "INSERT INTO clocks(network,height,time) VALUES(?,?,?) ON CONFLICT(network) DO UPDATE SET height=excluded.height,time=excluded.time",
        )
        .run(network, height, time);
    });
  }
  effectiveTime(network: string, now: number): number {
    const row = this.db
      .prepare("SELECT time FROM clocks WHERE network=?")
      .get(network) as { time: string } | undefined;
    return Math.max(now, row ? Number(row.time) : now);
  }
  pin(network: string, genesis: string) {
    if (!this.networks.includes(network) || !/^[0-9a-f]{64}$/.test(genesis))
      throw Error("ERR_NETWORK");
    this.transaction(() => {
      const row = this.db
        .prepare("SELECT genesis FROM pins WHERE network=?")
        .get(network) as { genesis: string } | undefined;
      if (row && row.genesis !== genesis) throw Error("ERR_GENESIS_REPLACED");
      this.db
        .prepare("INSERT OR IGNORE INTO pins(network,genesis) VALUES(?,?)")
        .run(network, genesis);
    });
  }
  transaction<T>(fn: () => T): T {
    this.db.exec("BEGIN IMMEDIATE");
    try {
      const r = fn();
      this.db.exec("COMMIT");
      return r;
    } catch (e) {
      this.db.exec("ROLLBACK");
      throw e;
    }
  }
  private authorized(id: string, secret: string): StoredSubscription | null {
    const row = this.db
      .prepare("SELECT value FROM subscriptions WHERE id=?")
      .get(id) as { value: string } | undefined;
    if (!row) return null;
    const s = JSON.parse(row.value) as StoredSubscription,
      expected = Buffer.from(s.secret_hash, "hex"),
      got = Buffer.from(secretHash(id, secret), "hex");
    if (!timingSafeEqual(expected, got)) throw Error("ERR_SUBSCRIPTION_AUTH");
    return s;
  }
  subscribe(input: Subscription) {
    validate("push_subscription", input);
    if (input.networks.some((n) => !this.networks.includes(n)))
      throw Error("ERR_NETWORK");
    return this.transaction(() => {
      if (
        this.db
          .prepare("SELECT id FROM tombstones WHERE id=?")
          .get(input.subscription_id)
      )
        throw Error("ERR_SUBSCRIPTION_RETIRED");
      const previous = this.authorized(
        input.subscription_id,
        input.unsubscribe_secret,
      );
      const duplicate = this.db
        .prepare("SELECT id,value FROM subscriptions")
        .all() as { id: string; value: string }[];
      if (
        duplicate.length >= 10000 &&
        !duplicate.some((s) => s.id === input.subscription_id)
      )
        throw Error("ERR_CAPACITY");
      for (const row of duplicate)
        if (
          row.id !== input.subscription_id &&
          (JSON.parse(row.value) as StoredSubscription).endpoint ===
            input.endpoint
        )
          throw Error("ERR_SUBSCRIPTION_EXISTS");
      const { unsubscribe_secret, ...safe } = input,
        value: StoredSubscription = {
          ...safe,
          secret_hash: secretHash(input.subscription_id, unsubscribe_secret),
        };
      if (previous && canonical(previous) === canonical(value))
        return { subscription_id: input.subscription_id };
      this.db
        .prepare(
          "INSERT INTO subscriptions(id,value) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET value=excluded.value",
        )
        .run(input.subscription_id, canonical(value));
      // Preference changes cancel pending deliveries rather than restoring old alerts.
      this.db
        .prepare(
          "UPDATE jobs SET state='cancelled' WHERE subscription_id=? AND state IN ('pending','inflight')",
        )
        .run(input.subscription_id);
      return { subscription_id: input.subscription_id };
    });
  }
  unsubscribe(id: string, secret: string) {
    return this.transaction(() => {
      const current = this.authorized(id, secret);
      if (current) {
        this.db
          .prepare("INSERT OR IGNORE INTO tombstones(id,hash) VALUES(?,?)")
          .run(id, current.secret_hash);
        this.db.prepare("DELETE FROM subscriptions WHERE id=?").run(id);
      }
    });
  }
  cursor(network: string): string {
    return (
      (
        this.db
          .prepare("SELECT value FROM cursors WHERE network=?")
          .get(network) as { value: string } | undefined
      )?.value ?? ""
    );
  }
  project(
    network: string,
    cursor: string,
    events: News[],
    now: number,
    crash?: () => void,
  ) {
    if (!this.networks.includes(network)) throw Error("ERR_NETWORK");
    return this.transaction(() => {
      const prior = this.cursor(network);
      if (prior) {
        const [a, b] = prior.split(":").map(BigInt),
          [c, d] = cursor ? cursor.split(":").map(BigInt) : [-1n, -1n];
        if (c! < a! || (c === a && d! < b!)) throw Error("ERR_SOURCE_ROLLBACK");
      }
      for (const event of events) {
        validate("news_item", event);
        if (
          event.network_id !== network ||
          event.event_id !==
            domain("push/poll.opened", [network, event.poll_id])
        )
          throw Error("ERR_EVENT_CONTEXT");
        const inserted = this.db
          .prepare(
            "INSERT OR IGNORE INTO news(id,network,poll,height,value) VALUES(?,?,?,?,?)",
          )
          .run(
            event.event_id,
            network,
            event.poll_id,
            event.source_height.padStart(20, "0"),
            canonical(event),
          );
        if (!inserted.changes) continue;
        if (
          BigInt(event.closes_at_ms) <= BigInt(this.effectiveTime(network, now))
        )
          continue;
        for (const row of this.db
          .prepare("SELECT id,value FROM subscriptions")
          .all() as { id: string; value: string }[]) {
          const s = JSON.parse(row.value) as StoredSubscription;
          if (s.enabled && s.networks.includes(network))
            this.db
              .prepare(
                "INSERT OR IGNORE INTO jobs(subscription_id,event_id,state,next,expires) VALUES(?,?,'pending',?,?)",
              )
              .run(row.id, event.event_id, now, Number(event.closes_at_ms));
        }
      }
      crash?.();
      this.db
        .prepare(
          "INSERT INTO cursors(network,value) VALUES(?,?) ON CONFLICT(network) DO UPDATE SET value=excluded.value",
        )
        .run(network, cursor);
    });
  }
  news(
    network: string,
    after: string,
    limit = 50,
  ): { items: News[]; next_cursor: string } {
    if (
      !this.networks.includes(network) ||
      !Number.isInteger(limit) ||
      limit < 1 ||
      limit > 100
    )
      throw Error("ERR_NETWORK");
    if (after && !/^[0-9]{20}:[0-9a-f]{64}$/.test(after))
      throw Error("ERR_CURSOR");
    const rows = this.db
        .prepare(
          "SELECT height,id,value FROM news WHERE network=? AND (?='' OR (height||':'||id)<?) ORDER BY height DESC,id DESC LIMIT ?",
        )
        .all(network, after, after, limit + 1) as {
        height: string;
        id: string;
        value: string;
      }[],
      page = rows.slice(0, limit);
    return {
      items: page.map((r) => JSON.parse(r.value) as News),
      next_cursor:
        rows.length > limit ? `${page.at(-1)!.height}:${page.at(-1)!.id}` : "",
    };
  }
  private claim(now: number): Job | null {
    return this.transaction(() => {
      this.db
        .prepare(
          "UPDATE jobs SET state='expired' WHERE state IN ('pending','inflight') AND expires<=?",
        )
        .run(now);
      this.db
        .prepare(
          "UPDATE jobs SET state='pending' WHERE state='inflight' AND lease<=?",
        )
        .run(now);
      const row = this.db
        .prepare(
          "SELECT j.subscription_id,j.event_id,j.attempt,j.expires,n.value message FROM jobs j JOIN news n ON n.id=j.event_id WHERE j.state='pending' AND j.next<=? ORDER BY j.next,j.event_id,j.subscription_id LIMIT 1",
        )
        .get(now) as unknown as Job | undefined;
      if (!row) return null;
      this.db
        .prepare(
          "UPDATE jobs SET state='inflight',lease=?,attempt=attempt+1 WHERE subscription_id=? AND event_id=?",
        )
        .run(now + 60000, row.subscription_id, row.event_id);
      return { ...row, attempt: row.attempt + 1 };
    });
  }
  async deliver(
    sender: Sender,
    clock: number | (() => number),
    maximum = 100,
  ): Promise<number> {
    let count = 0;
    for (; count < maximum; count++) {
      const now = typeof clock === "function" ? clock() : clock;
      const job = this.claim(now);
      if (!job) break;
      const row = this.db
        .prepare("SELECT value FROM subscriptions WHERE id=?")
        .get(job.subscription_id) as { value: string } | undefined;
      const subscription = row
          ? (JSON.parse(row.value) as StoredSubscription)
          : null,
        news = JSON.parse(job.message) as News;
      const effective = this.effectiveTime(news.network_id, now);
      if (effective >= job.expires) {
        this.finish(job, "expired", effective);
        continue;
      }
      if (
        !subscription?.enabled ||
        !subscription.networks.includes(news.network_id)
      ) {
        this.finish(job, "cancelled", now);
        continue;
      }
      const {
        proposal_id: _,
        opened_at_ms: __,
        source_height: ___,
        ...message
      } = news;
      let status = 503,
        retryAfter = 0,
        expired = false;
      try {
        const result = await sender.send(subscription, message, effective);
        status = result.status;
        expired = result.expired ?? false;
        retryAfter = result.retryAfter ?? 0;
      } catch {
        /* No endpoint, keys or transport response are logged. */
      }
      if (expired) {
        this.finish(job, "expired", job.expires);
        continue;
      }
      if (status === 404 || status === 410) {
        this.db
          .prepare("DELETE FROM subscriptions WHERE id=?")
          .run(job.subscription_id);
        continue;
      }
      if (status >= 200 && status < 300) {
        this.finish(job, "delivered", now);
        continue;
      }
      if (status !== 408 && status !== 425 && status !== 429 && status < 500) {
        this.finish(job, "rejected", now);
        continue;
      }
      const jitter =
          createHash("sha256")
            .update(job.subscription_id + job.event_id + job.attempt)
            .digest()
            .readUInt16BE(0) % 1000,
        wait = Math.max(
          Math.min(3600000, 1000 * 2 ** Math.min(job.attempt - 1, 12)) + jitter,
          Math.min(retryAfter * 1000, 86400000),
        );
      const next = Math.min(job.expires, now + wait);
      this.finish(job, next >= job.expires ? "expired" : "pending", next);
    }
    return count;
  }
  private finish(job: Job, state: string, next: number) {
    this.db
      .prepare(
        "UPDATE jobs SET state=?,next=?,lease=0 WHERE subscription_id=? AND event_id=? AND state='inflight'",
      )
      .run(state, next, job.subscription_id, job.event_id);
  }
}
