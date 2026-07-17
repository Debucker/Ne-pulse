// NE-PULSE offline app shell — deliberately hand-written (no next-pwa/
// Workbox dependency) so it stays simple to reason about. Its only real
// job: once a device has loaded /dashboard/lite at least once while
// online, the earthquake alarm keeps working — screen, siren, strobe,
// survival instructions — with zero network at all, since local shake
// detection never needed the network in the first place.
const CACHE_NAME = "ne-pulse-offline-v1";
const OFFLINE_URLS = ["/dashboard/lite", "/manifest.webmanifest", "/icon", "/pwa-icon-192", "/pwa-icon-512"];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(CACHE_NAME)
      .then((cache) => cache.addAll(OFFLINE_URLS))
      .then(() => self.skipWaiting()),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (event) => {
  const { request } = event;
  if (request.method !== "GET") return;

  // Page loads: network-first (so an online user always gets the latest
  // build), falling back to whatever's cached the instant connectivity
  // drops — and if this exact URL was never cached, fall back to the
  // alarm page itself rather than a bare browser offline error.
  if (request.mode === "navigate") {
    event.respondWith(
      fetch(request)
        .then((response) => {
          const copy = response.clone();
          caches.open(CACHE_NAME).then((cache) => cache.put(request, copy));
          return response;
        })
        .catch(() => caches.match(request).then((cached) => cached || caches.match("/dashboard/lite"))),
    );
    return;
  }

  // Everything else (JS/CSS/icons): serve from cache immediately if
  // present, refreshing it in the background — keeps the app shell
  // working even on a flaky or fully offline connection.
  event.respondWith(
    caches.match(request).then((cached) => {
      const networkFetch = fetch(request)
        .then((response) => {
          const copy = response.clone();
          caches.open(CACHE_NAME).then((cache) => cache.put(request, copy));
          return response;
        })
        .catch(() => cached);
      return cached || networkFetch;
    }),
  );
});
