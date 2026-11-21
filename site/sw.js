// Caches the page and the inspector so that Sphinx keeps working offline,
// which is also a simple way to be sure files are not sent anywhere.
const VERSION = "sphinx-v2";
const ASSETS = [
  "./", "index.html", "style.css", "app.js", "wasm_exec.js", "sphinx.wasm", "logo.svg", "manifest.webmanifest",
  "examples/cropped-screenshot.png", "examples/redacted-memo.pdf", "examples/cv.pdf", "examples/holiday.jpg",
  "examples/offer-letter.docx", "examples/logo.svg", "examples/shared-folder.zip",
];

self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(VERSION).then((c) => c.addAll(ASSETS)).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (e) => {
  e.waitUntil(caches.keys()
    .then((keys) => Promise.all(keys.filter((k) => k !== VERSION).map((k) => caches.delete(k))))
    .then(() => self.clients.claim()));
});

// Network first, so a new release is picked up when online; the cache
// answers when it is not.
self.addEventListener("fetch", (e) => {
  if (e.request.method !== "GET" || new URL(e.request.url).origin !== location.origin) return;
  e.respondWith(fetch(e.request)
    .then((res) => {
      const copy = res.clone();
      caches.open(VERSION).then((c) => c.put(e.request, copy));
      return res;
    })
    .catch(() => caches.match(e.request)));
});
