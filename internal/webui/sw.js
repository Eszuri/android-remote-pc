const CACHE_NAME = "arpc-shell";
const SHELL_ASSETS = [
  "/",
  "/index.html",
  "/favicon.ico",
  "/manifest.json",
  "/css/app.css",
  "/img/icon.svg",
  "/img/icon-192.png",
  "/js/crypto_primitives.js",
  "/js/crypto_aead.js",
  "/js/state.js",
  "/js/webrtc.js",
  "/js/connection.js",
  "/js/input.js",
  "/js/views.js",
  "/js/boot.js",
  "/vendor/jsqr.min.js"
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(SHELL_ASSETS))
  );
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k)))
    ).then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  if (event.request.method !== "GET" || url.pathname.startsWith("/ws") || url.pathname.startsWith("/api") || url.pathname === "/arpc.apk") {
    return;
  }
  event.respondWith(
    fetch(event.request)
      .then((res) => {
        const clone = res.clone();
        caches.open(CACHE_NAME).then((c) => c.put(event.request, clone));
        return res;
      })
      .catch(() => caches.match(event.request).then((r) => r || caches.match("/index.html")))
  );
});
