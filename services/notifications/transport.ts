import { lookup } from "node:dns/promises";
import { isIP, Socket, BlockList } from "node:net";
import { Agent, request } from "node:https";
import { createHash } from "node:crypto";
import { canonical } from "../../web/src/codec.js";
import { encryptPush, vapidAuthorization, unb64 } from "./crypto.js";
import type { StoredSubscription, Message, Delivery, Sender } from "./types.js";
export function pushTopic(subscriptionID: string, eventID: string): string {
  return createHash("sha256")
    .update("CIVIC/1/push/topic\n" + subscriptionID + "\n" + eventID)
    .digest("base64url")
    .slice(0, 32);
}
const denied4 = new BlockList(),
  denied6 = new BlockList();
for (const [ip, prefix] of [
  ["0.0.0.0", 8],
  ["10.0.0.0", 8],
  ["100.64.0.0", 10],
  ["127.0.0.0", 8],
  ["169.254.0.0", 16],
  ["172.16.0.0", 12],
  ["192.0.0.0", 24],
  ["192.0.2.0", 24],
  ["192.168.0.0", 16],
  ["198.18.0.0", 15],
  ["198.51.100.0", 24],
  ["203.0.113.0", 24],
  ["224.0.0.0", 4],
  ["240.0.0.0", 4],
] as const)
  denied4.addSubnet(ip, prefix, "ipv4");
for (const [ip, prefix] of [
  ["::", 96],
  ["::ffff:0:0", 96],
  ["64:ff9b::", 96],
  ["64:ff9b:1::", 48],
  ["100::", 64],
  ["2001::", 23],
  ["2001:db8::", 32],
  ["2002::", 16],
  ["3fff::", 20],
  ["fc00::", 7],
  ["fe80::", 10],
  ["ff00::", 8],
] as const)
  denied6.addSubnet(ip, prefix, "ipv6");
export function publicAddress(ip: string): boolean {
  const family = isIP(ip);
  if (
    !family ||
    (family === 4 ? denied4.check(ip, "ipv4") : denied6.check(ip, "ipv6"))
  )
    return false;
  return family === 4 || /^[23]/i.test(ip);
}
export function endpointURL(value: string): URL {
  const u = new URL(value);
  if (
    u.protocol !== "https:" ||
    u.username ||
    u.password ||
    u.hash ||
    (u.port && u.port !== "443") ||
    u.href.length > 4096 ||
    !u.hostname ||
    u.hostname.endsWith(".") ||
    /\.(localhost|local|internal)$/i.test(u.hostname) ||
    u.hostname === "localhost"
  )
    throw Error("ERR_PUSH_DESTINATION");
  const host = u.hostname.replace(/^\[|\]$/g, "");
  if (isIP(host) && !publicAddress(host)) throw Error("ERR_PUSH_DESTINATION");
  return u;
}
export async function resolvePublic(
  endpoint: string,
  resolve = lookup,
): Promise<{ address: string; family: number }[]> {
  const u = endpointURL(endpoint),
    host = u.hostname.replace(/^\[|\]$/g, "");
  const entries = isIP(host)
    ? [{ address: host, family: isIP(host) }]
    : await new Promise<{ address: string; family: number }[]>((yes, no) => {
        const timer = setTimeout(() => no(Error("ERR_PUSH_DNS_TIMEOUT")), 8000);
        timer.unref();
        resolve(host, { all: true, verbatim: true }).then(
          (value) => {
            clearTimeout(timer);
            yes(value);
          },
          (error) => {
            clearTimeout(timer);
            no(error);
          },
        );
      });
  if (!entries.length || entries.some((e) => !publicAddress(e.address)))
    throw Error("ERR_PUSH_DESTINATION");
  return entries.toSorted((a, b) => a.address.localeCompare(b.address));
}
export async function validatePushKeys(keys: {
  p256dh: string;
  auth: string;
}): Promise<void> {
  const { createECDH } = await import("node:crypto"),
    e = createECDH("prime256v1");
  e.generateKeys();
  e.computeSecret(unb64(keys.p256dh, 65));
  unb64(keys.auth, 16);
}
export async function validateSubscription(
  keys: { p256dh: string; auth: string },
  endpoint: string,
): Promise<void> {
  await validatePushKeys(keys);
  await resolvePublic(endpoint);
}
export class WebPushSender implements Sender {
  constructor(
    private secret: string,
    private contact: string,
  ) {}
  async send(
    subscription: StoredSubscription,
    message: Message,
    now: number,
  ): Promise<Delivery> {
    const started = Date.now();
    const destination = endpointURL(subscription.endpoint),
      ips = await resolvePublic(subscription.endpoint),
      remaining = Math.floor(
        (Number(message.closes_at_ms) - now - (Date.now() - started)) / 1000,
      );
    if (remaining <= 0) return { status: 0, expired: true };
    const body = encryptPush(
        Buffer.from(canonical(message)),
        subscription.keys,
      ),
      agent = new Agent({
        proxyEnv: {},
        keepAlive: false,
        lookup: (_host, options, callback) => {
          const ip = ips[0]!;
          if (options.all) callback(null, ips);
          else callback(null, ip.address, ip.family);
        },
      });
    try {
      return await new Promise((yes, no) => {
        const req = request(
          destination,
          {
            method: "POST",
            agent,
            timeout: 12000,
            headers: {
              Authorization: vapidAuthorization(
                this.secret,
                destination.href,
                this.contact,
                now,
              ),
              "Content-Encoding": "aes128gcm",
              "Content-Type": "application/octet-stream",
              "Content-Length": body.length,
              TTL: String(Math.min(remaining, 86400)),
              Urgency: "normal",
              Topic: pushTopic(subscription.subscription_id, message.event_id),
            },
          },
          (res) => {
            res.on("error", () => {});
            res.resume();
            const raw = res.headers["retry-after"],
              value = typeof raw === "string" ? raw : "";
            const seconds = /^\d+$/.test(value)
              ? Number(value)
              : Math.max(0, (Date.parse(value) - now) / 1000);
            yes({
              status: res.statusCode ?? 503,
              ...(Number.isFinite(seconds) ? { retryAfter: seconds } : {}),
            });
          },
        );
        req.on("error", no);
        req.on("timeout", () => req.destroy(Error("ERR_PUSH_TIMEOUT")));
        req.end(body);
      });
    } finally {
      agent.destroy();
    }
  }
}
