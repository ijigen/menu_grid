const CACHE_STATIC = 'static-v2';
const CACHE_API = 'api-v1';
const CACHE_IMAGES = 'images-v1';

const STATIC_ASSETS = [
    '/',
    '/static/css/style.css',
    '/static/js/app.js',
];

// ===== Install: pre-cache static assets =====
self.addEventListener('install', (event) => {
    event.waitUntil(
        caches.open(CACHE_STATIC).then(cache => cache.addAll(STATIC_ASSETS))
    );
    self.skipWaiting();
});

// ===== Activate: clean old caches =====
self.addEventListener('activate', (event) => {
    event.waitUntil(
        caches.keys().then(keys =>
            Promise.all(keys.filter(k => ![CACHE_STATIC, CACHE_API, CACHE_IMAGES].includes(k)).map(k => caches.delete(k)))
        )
    );
    self.clients.claim();
});

// ===== Fetch: strategy depends on request type =====
self.addEventListener('fetch', (event) => {
    const url = new URL(event.request.url);

    // Only handle same-origin requests
    if (url.origin !== self.location.origin) return;

    // Skip admin routes — admin panel doesn't need offline support
    if (url.pathname.startsWith('/admin') || url.pathname.startsWith('/api/admin')) return;

    // Preview images: cache-first (plaintext, safe to cache)
    if (url.pathname.startsWith('/api/images/preview/')) {
        event.respondWith(cacheFirst(event.request, CACHE_IMAGES));
        return;
    }

    // Encrypted images (thumb/full): cache-first
    if (url.pathname.startsWith('/api/images/thumb/') || url.pathname.startsWith('/api/images/full/')) {
        event.respondWith(cacheFirst(event.request, CACHE_IMAGES));
        return;
    }

    // verify-password: never cache (password may change; offline uses local verification)
    if (url.pathname === '/api/verify-password') return;

    // API data (works list, fallback): network-first
    if (url.pathname.startsWith('/api/')) {
        event.respondWith(networkFirst(event.request, CACHE_API));
        return;
    }

    // Static assets (HTML, CSS, JS): stale-while-revalidate
    event.respondWith(staleWhileRevalidate(event.request, CACHE_STATIC));
});

// ===== Cache strategies =====

async function cacheFirst(request, cacheName) {
    const cached = await caches.match(request);
    if (cached) return cached;

    try {
        const response = await fetchWithFallback(request);
        if (response.ok) {
            const cache = await caches.open(cacheName);
            cache.put(request, response.clone());
        }
        return response;
    } catch {
        return new Response('', { status: 503 });
    }
}

async function networkFirst(request, cacheName) {
    try {
        const response = await fetchWithFallback(request);
        if (response.ok) {
            const cache = await caches.open(cacheName);
            cache.put(request, response.clone());
        }
        return response;
    } catch {
        const cached = await caches.match(request);
        if (cached) return cached;
        return new Response(JSON.stringify({ error: 'offline' }), {
            status: 503,
            headers: { 'Content-Type': 'application/json' },
        });
    }
}

async function staleWhileRevalidate(request, cacheName) {
    const cache = await caches.open(cacheName);
    const cached = await cache.match(request);

    const fetchPromise = fetchWithFallback(request).then(response => {
        if (response.ok) cache.put(request, response.clone());
        return response;
    }).catch(() => null);

    return cached || (await fetchPromise) || new Response('', { status: 503 });
}

// ===== Fallback domain support =====
// When main domain fails, try fallback domains.

async function fetchWithFallback(request) {
    try {
        const response = await fetch(request);
        // Main domain works — return response
        return response;
    } catch (mainError) {
        // Main domain failed — try fallback domains
        const domains = await getFallbackDomains();
        if (!domains || domains.length === 0) throw mainError;

        const url = new URL(request.url);
        for (const domain of domains) {
            try {
                const fallbackUrl = `${url.protocol}//${domain}${url.pathname}${url.search}`;
                const fallbackRequest = new Request(fallbackUrl, {
                    method: request.method,
                    headers: request.headers,
                    body: request.method !== 'GET' ? request.body : undefined,
                    mode: 'cors',
                });
                const response = await fetch(fallbackRequest);
                if (response.ok) {
                    // Return response as if it came from main domain
                    return new Response(response.body, {
                        status: response.status,
                        statusText: response.statusText,
                        headers: response.headers,
                    });
                }
            } catch {
                // This fallback failed, try next
                reportDomainFailure(domain);
            }
        }

        // All fallbacks failed
        throw mainError;
    }
}

// Get fallback domains from IndexedDB (stored by main page)
async function getFallbackDomains() {
    try {
        const cache = await caches.open(CACHE_API);
        const response = await cache.match('/_internal/fallback-domains');
        if (!response) return [];
        const data = await response.json();
        return data.domains || [];
    } catch {
        return [];
    }
}

// Report domain failure back to main thread
function reportDomainFailure(domain) {
    self.clients.matchAll().then(clients => {
        clients.forEach(client => {
            client.postMessage({ type: 'fallback-domain-failed', domain });
        });
    });
}

// Listen for messages from main thread
self.addEventListener('message', (event) => {
    if (event.data.type === 'store-fallback-domains') {
        // Store domains in cache for SW access
        const response = new Response(JSON.stringify({ domains: event.data.domains }));
        caches.open(CACHE_API).then(cache => {
            cache.put('/_internal/fallback-domains', response);
        });
    }
});
