// The previous NE-PULSE site installed an offline "earthquake alarm" service
// worker that cached its pages. That alarm is retired (it sounded a siren
// on local shaking alone, which is not a reliable earthquake signal). This
// worker replaces it, deletes its caches and unregisters itself, so returning
// visitors always load the current site.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.map((k) => caches.delete(k))))
      .then(() => self.registration.unregister())
      .then(() => self.clients.matchAll())
      .then((clients) => clients.forEach((c) => c.navigate(c.url))),
  );
});
