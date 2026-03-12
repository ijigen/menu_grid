// ===== State =====
const state = {
    ageVerified: localStorage.getItem('age_verified') === 'true',
    passwordVerified: false,
    works: [],
    favorites: JSON.parse(localStorage.getItem('favorites') || '[]'),
    expandedWorkId: null,
    cryptoKey: null, // AES-GCM CryptoKey derived from password
    blobCache: {},   // filename -> { preview: blobUrl, thumb: blobUrl, full: blobUrl }
};

// ===== DOM =====
const dom = {
    ageGate: document.getElementById('age-gate'),
    passwordGate: document.getElementById('password-gate'),
    gallery: document.getElementById('gallery'),
    worksGrid: document.getElementById('works-grid'),
    passwordForm: document.getElementById('password-form'),
    passwordInput: document.getElementById('password-input'),
    passwordError: document.getElementById('password-error'),
    ageConfirm: document.getElementById('age-confirm'),
    ageDeny: document.getElementById('age-deny'),
    favoritesToggle: document.getElementById('favorites-toggle'),
    favoritesPanel: document.getElementById('favorites-panel'),
    favoritesOverlay: document.getElementById('favorites-overlay'),
    favoritesClose: document.getElementById('favorites-close'),
    favoritesList: document.getElementById('favorites-list'),
    favoritesCount: document.getElementById('favorites-count'),
};

// ===== Image URL helpers =====
function previewUrl(filename) { return `/api/images/preview/${filename}`; }
function thumbUrl(filename) { return `/api/images/thumb/${filename}`; }
function fullUrl(filename) { return `/api/images/full/${filename}`; }

// ===== Crypto: PBKDF2 key derivation + AES-GCM decryption =====
async function deriveKey(password, saltBase64, iterations) {
    const enc = new TextEncoder();
    const salt = Uint8Array.from(atob(saltBase64), c => c.charCodeAt(0));

    const keyMaterial = await crypto.subtle.importKey(
        'raw', enc.encode(password), 'PBKDF2', false, ['deriveKey']
    );

    return crypto.subtle.deriveKey(
        { name: 'PBKDF2', salt, iterations, hash: 'SHA-256' },
        keyMaterial,
        { name: 'AES-GCM', length: 256 },
        false,
        ['decrypt']
    );
}

async function decryptImage(encryptedBuffer) {
    if (!state.cryptoKey) throw new Error('No key');
    const data = new Uint8Array(encryptedBuffer);
    const iv = data.slice(0, 12);
    const ciphertext = data.slice(12);

    const decrypted = await crypto.subtle.decrypt(
        { name: 'AES-GCM', iv },
        state.cryptoKey,
        ciphertext
    );
    return new Blob([decrypted], { type: 'image/webp' });
}

// Fetch encrypted image, decrypt, return blob URL. Caches results.
async function loadEncryptedImage(filename, type) {
    const cacheKey = `${type}:${filename}`;
    if (state.blobCache[cacheKey]) return state.blobCache[cacheKey];

    const url = type === 'thumb' ? thumbUrl(filename) : fullUrl(filename);
    const res = await fetch(url);
    if (!res.ok) throw new Error('fetch failed');

    const blob = await decryptImage(await res.arrayBuffer());
    const blobUrl = URL.createObjectURL(blob);
    state.blobCache[cacheKey] = blobUrl;
    return blobUrl;
}

// ===== Init =====
function init() {
    setupEventListeners();
    updateGateState();
}

function setupEventListeners() {
    dom.ageConfirm.addEventListener('click', onAgeConfirm);
    dom.ageDeny.addEventListener('click', () => {
        window.location.href = 'about:blank';
    });
    dom.passwordForm.addEventListener('submit', onPasswordSubmit);
    dom.favoritesToggle.addEventListener('click', toggleFavorites);
    dom.favoritesClose.addEventListener('click', toggleFavorites);
    dom.favoritesOverlay.addEventListener('click', toggleFavorites);
}

// ===== Gate Logic =====
function updateGateState() {
    dom.ageGate.style.display = 'none';
    dom.passwordGate.style.display = 'none';
    dom.gallery.style.display = 'none';

    if (!state.ageVerified) {
        dom.ageGate.style.display = 'flex';
    } else if (!state.passwordVerified) {
        dom.passwordGate.style.display = 'flex';
        // Pre-fetch works data + preview images while user types password
        prefetchPreviews();
    } else {
        dom.gallery.style.display = 'block';
        loadWorks();
    }
}

// Pre-fetch works list and preview images before password verification.
// Data is cached in state but NOT rendered until both gates pass.
async function prefetchPreviews() {
    if (state.prefetched) return;
    state.prefetched = true;
    try {
        const res = await fetch('/api/works');
        const works = await res.json();
        state.works = works;
        // Pre-fetch all preview images into browser cache
        const previews = [];
        for (const work of works) {
            if (work.images) {
                for (const img of work.images) {
                    previews.push(previewUrl(img.filename));
                }
            }
        }
        // Fetch up to 6 at a time
        const queue = [...previews];
        async function worker() {
            while (queue.length > 0) {
                const url = queue.shift();
                try { await fetch(url); } catch {}
            }
        }
        const workers = [];
        for (let i = 0; i < Math.min(6, previews.length); i++) workers.push(worker());
        await Promise.all(workers);
    } catch {}
}

function onAgeConfirm() {
    state.ageVerified = true;
    localStorage.setItem('age_verified', 'true');
    updateGateState();
}

async function onPasswordSubmit(e) {
    e.preventDefault();
    const password = dom.passwordInput.value.trim();
    if (!password) return;

    try {
        const res = await fetch('/api/verify-password', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ password }),
        });

        if (res.ok) {
            const data = await res.json();
            // Derive AES key from password + salt
            state.cryptoKey = await deriveKey(password, data.salt, data.iterations);
            state.passwordVerified = true;
            dom.passwordError.style.display = 'none';
            updateGateState();
        } else {
            dom.passwordError.style.display = 'block';
        }
    } catch {
        dom.passwordError.textContent = '連線錯誤，請稍後再試。';
        dom.passwordError.style.display = 'block';
    }
}

// ===== Works =====
async function loadWorks() {
    try {
        // Use pre-fetched data if available, otherwise fetch fresh
        if (!state.works || state.works.length === 0) {
            const res = await fetch('/api/works');
            state.works = await res.json();
        }
        renderWorks();
    } catch {
        dom.worksGrid.innerHTML = '<p style="color:#666;padding:40px;text-align:center;">無法載入作品</p>';
    }
}

function renderWorks() {
    dom.worksGrid.innerHTML = '';
    if (!state.works || state.works.length === 0) {
        dom.worksGrid.innerHTML = '<p style="color:#666;padding:40px;text-align:center;grid-column:1/-1;">目前沒有作品</p>';
        return;
    }

    state.works.forEach(work => {
        const card = createWorkCard(work);
        dom.worksGrid.appendChild(card);
    });

    // Start prioritized image loading
    initLazyLoad();
}

function createWorkCard(work) {
    const card = document.createElement('div');
    card.className = 'work-card';
    card.dataset.id = work.id;

    const isFav = state.favorites.includes(work.id);
    const cover = getCoverImage(work);

    // Start with empty src — lazy loader will handle loading
    card.innerHTML = `
        <div class="work-card-thumb" data-work-id="${work.id}">
            ${cover
                ? `<img data-filename="${cover.filename}" src="" alt="${escapeHtml(work.title)}" class="card-img">`
                : '<div class="placeholder-img">&#128444;</div>'
            }
        </div>
        <div class="work-card-info">
            <span class="work-card-title">${escapeHtml(work.title)}</span>
            <span class="work-card-price">${escapeHtml(work.price)}</span>
        </div>
        <div class="work-card-actions">
            <button class="heart-btn ${isFav ? 'active' : ''}" data-work-id="${work.id}" title="收藏">&#9829;</button>
        </div>
    `;

    card.querySelector('.work-card-thumb').addEventListener('click', () => {
        toggleWorkDetail(work.id, card);
    });

    card.querySelector('.heart-btn').addEventListener('click', (e) => {
        e.stopPropagation();
        toggleFavorite(work.id, e);
    });

    return card;
}

// ===== Lazy Load / Priority Loading =====
// Level 1: viewport preview (plaintext, fast)
// Level 2: viewport thumb (encrypted)
// Level 3: off-viewport preview + thumb
let loadObserver = null;
const cardLoadState = new Map(); // card element -> { phase: 'none'|'preview'|'thumb' }

function initLazyLoad() {
    if (loadObserver) loadObserver.disconnect();

    const cards = document.querySelectorAll('.work-card');
    cards.forEach(c => cardLoadState.set(c, { phase: 'none' }));

    // Use IntersectionObserver to split viewport vs off-viewport
    const viewportCards = [];
    const offViewportCards = [];

    loadObserver = new IntersectionObserver((entries) => {
        // We only need the initial observation to classify cards
        entries.forEach(entry => {
            const card = entry.target;
            if (entry.isIntersecting) {
                viewportCards.push(card);
            } else {
                offViewportCards.push(card);
            }
        });

        // After first batch of observations, disconnect and start loading
        // (we wait a frame to ensure all entries are processed)
        requestAnimationFrame(() => {
            if (viewportCards.length + offViewportCards.length < cards.length) return;
            loadObserver.disconnect();
            startPriorityLoading(viewportCards, offViewportCards);
        });
    }, { threshold: 0 });

    cards.forEach(c => loadObserver.observe(c));

    // Fallback: if observer doesn't fire (all cards visible), trigger after short delay
    setTimeout(() => {
        if (viewportCards.length + offViewportCards.length < cards.length) {
            loadObserver.disconnect();
            // Treat all unclassified cards as viewport
            cards.forEach(c => {
                if (!viewportCards.includes(c) && !offViewportCards.includes(c)) {
                    viewportCards.push(c);
                }
            });
            startPriorityLoading(viewportCards, offViewportCards);
        }
    }, 200);
}

async function startPriorityLoading(viewportCards, offViewportCards) {
    // Phase 1: Load viewport previews (plaintext, parallel, fast)
    await loadPhase(viewportCards, loadCardPreview, 6);

    // Phase 2: Load viewport thumbs (encrypted, decrypt, slower)
    await loadPhase(viewportCards, loadCardThumb, 3);

    // Phase 3: Off-viewport previews
    await loadPhase(offViewportCards, loadCardPreview, 4);

    // Phase 4: Off-viewport thumbs
    await loadPhase(offViewportCards, loadCardThumb, 2);
}

async function loadPhase(cards, loadFn, concurrency) {
    const queue = [...cards];
    async function worker() {
        while (queue.length > 0) {
            const card = queue.shift();
            try { await loadFn(card); } catch {}
        }
    }
    const workers = [];
    for (let i = 0; i < Math.min(concurrency, cards.length); i++) {
        workers.push(worker());
    }
    await Promise.all(workers);
}

async function loadCardPreview(card) {
    const img = card.querySelector('.card-img');
    if (!img) return;
    const filename = img.dataset.filename;
    if (!filename) return;

    const st = cardLoadState.get(card);
    if (st && st.phase !== 'none') return; // already loaded

    return new Promise((resolve) => {
        img.onload = () => {
            if (st) st.phase = 'preview';
            resolve();
        };
        img.onerror = resolve;
        img.src = previewUrl(filename);
    });
}

async function loadCardThumb(card) {
    const img = card.querySelector('.card-img');
    if (!img) return;
    const filename = img.dataset.filename;
    if (!filename || !state.cryptoKey) return;

    const st = cardLoadState.get(card);
    if (st && st.phase === 'thumb') return; // already loaded

    try {
        const blobUrl = await loadEncryptedImage(filename, 'thumb');
        img.src = blobUrl;
        if (st) st.phase = 'thumb';
    } catch {}
}

// ===== Expand Detail =====
function closeOpenDetail() {
    const existing = document.querySelector('.work-detail');
    if (existing) {
        existing.remove();
        const prev = state.expandedWorkId;
        state.expandedWorkId = null;
        return prev;
    }
    return null;
}

function toggleWorkDetail(workId, card) {
    const prevId = closeOpenDetail();
    if (prevId === workId) return;

    const work = state.works.find(w => w.id === workId);
    if (!work) return;

    const detail = document.createElement('div');
    detail.className = 'work-detail';
    detail.innerHTML = renderWorkDetail(work);

    card.after(detail);
    state.expandedWorkId = workId;

    detail.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    detail.querySelector('.work-detail-close')?.addEventListener('click', closeOpenDetail);

    // Load main image with blur-to-clear
    setupDetailMainImage(detail, work);

    // Sub-thumb clicks
    detail.querySelectorAll('.work-detail-sub-thumbs img').forEach(subImg => {
        subImg.addEventListener('click', () => {
            switchDetailMainImage(detail, subImg.dataset.filename);
            detail.querySelectorAll('.work-detail-sub-thumbs img').forEach(i => i.classList.remove('active'));
            subImg.classList.add('active');
        });
    });

    // Load sub-thumbnails (encrypted thumbs)
    loadDetailSubThumbs(detail);
}

function renderWorkDetail(work) {
    const images = work.images || [];
    const firstImg = images[0];

    let mainImgHtml = '';
    if (firstImg) {
        mainImgHtml = `
            <div class="work-detail-img-wrap">
                <img class="img-preview" src="${previewUrl(firstImg.filename)}" alt="">
                <img class="img-clear" src="" alt="${escapeHtml(work.title)}" data-filename="${firstImg.filename}">
            </div>
        `;
    }

    let subThumbsHtml = '';
    if (images.length > 1) {
        subThumbsHtml = '<div class="work-detail-sub-thumbs">';
        images.forEach((img, idx) => {
            subThumbsHtml += `<img src="" data-filename="${img.filename}" class="${idx === 0 ? 'active' : ''}" alt="">`;
        });
        subThumbsHtml += '</div>';
    }

    return `
        <div class="work-detail-inner">
            <button class="work-detail-close">收合</button>
            ${mainImgHtml}
            ${subThumbsHtml}
            ${work.content ? `<div class="work-detail-content">${escapeHtml(work.content)}</div>` : ''}
        </div>
    `;
}

async function setupDetailMainImage(detail, work) {
    const clearImg = detail.querySelector('.img-clear');
    const previewImg = detail.querySelector('.img-preview');
    if (!clearImg || !previewImg) return;

    const filename = clearImg.dataset.filename;
    if (!filename) return;

    try {
        const blobUrl = await loadEncryptedImage(filename, 'full');
        clearImg.src = blobUrl;

        const reveal = () => {
            requestAnimationFrame(() => {
                requestAnimationFrame(() => {
                    clearImg.classList.add('loaded');
                    previewImg.classList.add('hidden');
                });
            });
        };

        if (clearImg.complete && clearImg.naturalWidth > 0) {
            reveal();
        } else {
            clearImg.addEventListener('load', reveal, { once: true });
        }
    } catch {
        // Decryption failed — keep showing preview
    }
}

async function switchDetailMainImage(detail, filename) {
    const wrap = detail.querySelector('.work-detail-img-wrap');
    if (!wrap) return;

    const previewImg = wrap.querySelector('.img-preview');
    const clearImg = wrap.querySelector('.img-clear');

    // Reset to blurry preview
    previewImg.classList.remove('hidden');
    previewImg.src = previewUrl(filename);
    clearImg.classList.remove('loaded');
    clearImg.src = '';
    clearImg.dataset.filename = filename;

    try {
        const blobUrl = await loadEncryptedImage(filename, 'full');
        clearImg.src = blobUrl;

        const reveal = () => {
            requestAnimationFrame(() => {
                requestAnimationFrame(() => {
                    clearImg.classList.add('loaded');
                    previewImg.classList.add('hidden');
                });
            });
        };

        if (clearImg.complete && clearImg.naturalWidth > 0) {
            reveal();
        } else {
            clearImg.addEventListener('load', reveal, { once: true });
        }
    } catch {}
}

async function loadDetailSubThumbs(detail) {
    const subImgs = detail.querySelectorAll('.work-detail-sub-thumbs img');
    for (const img of subImgs) {
        const filename = img.dataset.filename;
        if (!filename) continue;
        try {
            const blobUrl = await loadEncryptedImage(filename, 'thumb');
            img.src = blobUrl;
        } catch {
            // Fallback to preview
            img.src = previewUrl(filename);
        }
    }
}

// ===== Favorites =====
function toggleFavorite(workId, event) {
    const idx = state.favorites.indexOf(workId);
    const btn = event.currentTarget;

    if (idx === -1) {
        state.favorites.push(workId);
        btn.classList.add('active');
        animateFlyingHeart(btn);
    } else {
        state.favorites.splice(idx, 1);
        btn.classList.remove('active');
    }

    localStorage.setItem('favorites', JSON.stringify(state.favorites));
    updateFavoritesCount();
    renderFavoritesList();
}

function animateFlyingHeart(btn) {
    const heart = document.createElement('span');
    heart.className = 'flying-heart';
    heart.textContent = '\u2665';

    const btnRect = btn.getBoundingClientRect();
    const targetBtn = dom.favoritesToggle.getBoundingClientRect();

    heart.style.left = btnRect.left + btnRect.width / 2 + 'px';
    heart.style.top = btnRect.top + 'px';
    document.body.appendChild(heart);

    requestAnimationFrame(() => {
        heart.classList.add('fly');
        heart.style.left = targetBtn.left + targetBtn.width / 2 + 'px';
        heart.style.top = targetBtn.top + targetBtn.height / 2 + 'px';
    });

    setTimeout(() => heart.remove(), 700);
}

function updateFavoritesCount() {
    dom.favoritesCount.textContent = state.favorites.length;
}

function toggleFavorites() {
    dom.favoritesPanel.classList.toggle('open');
    dom.favoritesOverlay.classList.toggle('open');
}

function renderFavoritesList() {
    if (state.favorites.length === 0) {
        dom.favoritesList.innerHTML = '<p class="empty-msg">尚無收藏</p>';
        return;
    }

    dom.favoritesList.innerHTML = '';
    state.favorites.forEach(workId => {
        const work = state.works.find(w => w.id === workId);
        if (!work) return;

        const cover = getCoverImage(work);
        const imgSrc = cover ? previewUrl(cover.filename) : '';

        const item = document.createElement('div');
        item.className = 'fav-item';
        item.innerHTML = `
            ${imgSrc ? `<img src="${imgSrc}" alt="">` : ''}
            <div class="fav-item-info">
                <div class="fav-item-title">${escapeHtml(work.title)}</div>
                <div class="fav-item-price">${escapeHtml(work.price)}</div>
            </div>
            <button class="fav-item-remove" data-id="${workId}">&times;</button>
        `;
        item.querySelector('.fav-item-remove').addEventListener('click', () => {
            const i = state.favorites.indexOf(workId);
            if (i !== -1) state.favorites.splice(i, 1);
            localStorage.setItem('favorites', JSON.stringify(state.favorites));
            updateFavoritesCount();
            renderFavoritesList();
            const heartBtn = document.querySelector(`.heart-btn[data-work-id="${workId}"]`);
            if (heartBtn) heartBtn.classList.remove('active');
        });
        dom.favoritesList.appendChild(item);
    });
}

// ===== Helpers =====
function getCoverImage(work) {
    if (!work.images || work.images.length === 0) return null;
    return work.images.find(i => i.is_cover) || work.images[0];
}

function escapeHtml(str) {
    if (!str) return '';
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

// ===== Phase 3: Service Worker + Fallback Domains =====

// Generate or retrieve a stable client ID for fallback domain assignment
function getClientId() {
    let id = localStorage.getItem('client_id');
    if (!id) {
        id = crypto.randomUUID ? crypto.randomUUID() : (
            'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, c => {
                const r = Math.random() * 16 | 0;
                return (c === 'x' ? r : (r & 0x3 | 0x8)).toString(16);
            })
        );
        localStorage.setItem('client_id', id);
    }
    return id;
}

// Fetch fallback domains from server and pass to Service Worker
async function initFallbackDomains() {
    try {
        const clientId = getClientId();
        const res = await fetch(`/api/fallback/domains?client_id=${encodeURIComponent(clientId)}`);
        if (!res.ok) return;
        const data = await res.json();
        const domains = data.domains || [];
        if (domains.length === 0) return;

        // Store in SW via postMessage
        if (navigator.serviceWorker && navigator.serviceWorker.controller) {
            navigator.serviceWorker.controller.postMessage({
                type: 'store-fallback-domains',
                domains,
            });
        }
    } catch {
        // Silently fail — fallback domains are optional
    }
}

// Handle SW messages (e.g., failed fallback domain)
function setupSWMessageHandler() {
    if (!navigator.serviceWorker) return;
    navigator.serviceWorker.addEventListener('message', async (event) => {
        if (event.data.type === 'fallback-domain-failed') {
            const failedDomain = event.data.domain;
            const clientId = getClientId();

            // Report failure to server
            try {
                await fetch('/api/fallback/report-failure', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ client_id: clientId, domain: failedDomain }),
                });
            } catch {}

            // Request replacement
            try {
                const res = await fetch('/api/fallback/request-replacement', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ client_id: clientId, failed_domain: failedDomain }),
                });
                if (!res.ok) return;
                const data = await res.json();
                if (data.domain) {
                    // Re-fetch updated domain list and update SW
                    await initFallbackDomains();
                }
            } catch {}
        }
    });
}

// Register Service Worker
async function registerServiceWorker() {
    if (!('serviceWorker' in navigator)) return;
    try {
        await navigator.serviceWorker.register('/sw.js');
        setupSWMessageHandler();
        // Wait for SW to activate then send fallback domains
        if (navigator.serviceWorker.controller) {
            initFallbackDomains();
        } else {
            navigator.serviceWorker.addEventListener('controllerchange', () => {
                initFallbackDomains();
            }, { once: true });
        }
    } catch {}
}

// ===== Start =====
updateFavoritesCount();
init();
registerServiceWorker();
