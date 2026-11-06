// Application shell only. API data has explicit, network-scoped IndexedDB
// caching in the client. POSTs, ballots and secrets never enter this cache.
const VERSION = "civora-pwa-0.7.0",
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
self.addEventListener("install", (event) =>
  event.waitUntil(caches.open(VERSION).then((cache) => cache.addAll(SHELL))),
);
self.addEventListener("activate", (event) =>
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
      self.clients.claim(),
    ]),
  ),
);
self.addEventListener("fetch", (event) => {
  const req = event.request,
    url = new URL(req.url);
  if (
    req.method !== "GET" ||
    url.origin !== self.location.origin ||
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
          .then((value) => value || Response.error()),
      ),
  );
});
