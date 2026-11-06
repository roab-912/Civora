/// <reference lib="webworker" />
import { read, write } from "./storage.js";
import type { NotificationSettings } from "./notifications.js";
const sw = self as unknown as ServiceWorkerGlobalScope,
  VERSION = "civora-pwa-0.8.0",
  SHELL = [
    "/",
    "/index.html",
    "/app.js",
    "/worker.js",
    "/app.css",
    "/icon.svg",
    "/icon-192.png",
    "/icon-512.png",
    "/manifest.webmanifest",
    "/build.json",
    "/pwa-config.json",
  ];
sw.addEventListener("install", (event) =>
  event.waitUntil(caches.open(VERSION).then((cache) => cache.addAll(SHELL))),
);
sw.addEventListener("activate", (event) =>
  event.waitUntil(
    Promise.all([
      caches
        .keys()
        .then((keys) =>
          Promise.all(
            keys
              .filter((k) => k.startsWith("civora-pwa-") && k !== VERSION)
              .map((k) => caches.delete(k)),
          ),
        ),
      sw.clients.claim(),
    ]),
  ),
);
sw.addEventListener("fetch", (event) => {
  const req = event.request,
    url = new URL(req.url);
  if (
    req.method !== "GET" ||
    url.origin !== sw.location.origin ||
    !SHELL.includes(url.pathname)
  )
    return;
  event.respondWith(
    fetch(req)
      .then((response) => {
        if (response.ok) {
          const copy = response.clone();
          event.waitUntil(
            caches.open(VERSION).then((cache) => cache.put(req, copy)),
          );
        }
        return response;
      })
      .catch(() =>
        caches
          .open(VERSION)
          .then((cache) => cache.match(req))
          .then((value) => value ?? Response.error()),
      ),
  );
});
function payload(value: unknown): value is {
  v: string;
  type: string;
  event_id: string;
  network_id: string;
  poll_id: string;
  closes_at_ms: string;
} {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const p = value as Record<string, unknown>,
    keys = ["v", "type", "event_id", "network_id", "poll_id", "closes_at_ms"];
  return (
    Object.keys(p).length === keys.length &&
    keys.every((k) => typeof p[k] === "string") &&
    p.v === "1" &&
    p.type === "poll.opened" &&
    [p.event_id, p.network_id, p.poll_id].every((v) =>
      /^[0-9a-f]{64}$/.test(v as string),
    ) &&
    /^(0|[1-9][0-9]{0,19})$/.test(p.closes_at_ms as string) &&
    BigInt(p.closes_at_ms as string) <= 18446744073709551615n
  );
}
let pushQueue = Promise.resolve();
sw.addEventListener("push", (event) =>
  event.waitUntil(
    (pushQueue = pushQueue
      .then(async () => {
        let p: unknown;
        try {
          p = event.data?.json();
        } catch {
          return;
        }
        if (!payload(p) || BigInt(p.closes_at_ms) <= BigInt(Date.now())) return;
        const settings = await read<NotificationSettings>(
          "settings",
          "notifications",
        );
        if (!settings?.enabled || !settings.networks.includes(p.network_id))
          return;
        const key = "push-seen/" + p.network_id + "/" + p.event_id;
        if (await read("settings", key)) return;
        // Serialisation below is scoped to this worker instance. System tags provide
        // a second deduplication layer after a worker restart or delivery retry.
        await sw.registration.showNotification("Nouveau référendum", {
          body: "Consulter la question",
          icon: "/icon-192.png",
          badge: "/icon-192.png",
          tag: p.event_id,
          data: p,
        });
        await write("settings", key, p.closes_at_ms);
      })
      .catch(() => {})),
  ),
);
sw.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const p: unknown = event.notification.data;
  if (!payload(p) || BigInt(p.closes_at_ms) <= BigInt(Date.now())) return;
  const url =
    sw.location.origin + "/#/polls/" + p.poll_id + "?network=" + p.network_id;
  event.waitUntil(
    (async () => {
      for (const client of await sw.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      })) {
        if (new URL(client.url).origin === sw.location.origin) {
          await (client as WindowClient).navigate(url);
          await (client as WindowClient).focus();
          return;
        }
      }
      await sw.clients.openWindow(url);
    })(),
  );
});
sw.addEventListener("pushsubscriptionchange", (event) => {
  (event as ExtendableEvent).waitUntil(
    (async () => {
      const s = await read<NotificationSettings>("settings", "notifications");
      if (s)
        await write("settings", "notifications", {
          ...s,
          enabled: false,
          pending_delete: true,
        });
    })(),
  );
});
