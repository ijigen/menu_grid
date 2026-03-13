// ===== State =====
let token = sessionStorage.getItem('admin_token') || '';
let works = [];
let settingsVisible = false;

// ===== DOM =====
const loginSection = document.getElementById('admin-login');
const dashboard = document.getElementById('dashboard');
const loginForm = document.getElementById('login-form');
const loginError = document.getElementById('login-error');
const adminPassword = document.getElementById('admin-password');

const worksTbody = document.getElementById('works-tbody');
const worksEmpty = document.getElementById('works-empty');
const workModal = document.getElementById('work-modal');
const workForm = document.getElementById('work-form');
const modalTitle = document.getElementById('modal-title');

const settingsSection = document.getElementById('settings-section');
const unlockPasswordInput = document.getElementById('unlock-password-input');

// ===== Init =====
if (token) {
    showDashboard();
} else {
    loginSection.style.display = 'flex';
}

// ===== Event Listeners =====
loginForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    const pw = adminPassword.value.trim();
    if (!pw) return;

    try {
        const res = await api('POST', '/api/admin/login', { password: pw });
        if (res.token) {
            token = res.token;
            sessionStorage.setItem('admin_token', token);
            loginError.style.display = 'none';
            showDashboard();
        }
    } catch {
        loginError.style.display = 'block';
    }
});

document.getElementById('btn-logout').addEventListener('click', () => {
    token = '';
    sessionStorage.removeItem('admin_token');
    dashboard.style.display = 'none';
    loginSection.style.display = 'flex';
    adminPassword.value = '';
});

document.getElementById('btn-add-work').addEventListener('click', () => openWorkModal());
document.getElementById('btn-cancel').addEventListener('click', closeWorkModal);
workModal.addEventListener('click', (e) => { if (e.target === workModal) closeWorkModal(); });

document.getElementById('btn-settings').addEventListener('click', () => {
    settingsVisible = !settingsVisible;
    settingsSection.style.display = settingsVisible ? 'block' : 'none';
    if (settingsVisible) loadSettings();
});

// Fallback domain management
let fallbackVisible = false;
const fallbackSection = document.getElementById('fallback-section');

document.getElementById('btn-fallback').addEventListener('click', () => {
    fallbackVisible = !fallbackVisible;
    fallbackSection.style.display = fallbackVisible ? 'block' : 'none';
    if (fallbackVisible) loadFallbackDomains();
});

document.getElementById('btn-regen-thumbs').addEventListener('click', async () => {
    if (!confirm('確定要重新生成所有縮圖與預覽圖？這可能需要一些時間。')) return;
    const btn = document.getElementById('btn-regen-thumbs');
    btn.disabled = true;
    btn.textContent = '生成中...';
    try {
        const result = await api('POST', '/api/admin/regenerate-thumbnails');
        alert(`完成！共 ${result.total} 張，成功 ${result.success} 張，失敗 ${result.failed} 張`);
    } catch {
        alert('重新生成失敗');
    } finally {
        btn.disabled = false;
        btn.textContent = '重新生成縮圖';
    }
});

document.getElementById('btn-add-fallback').addEventListener('click', async () => {
    const input = document.getElementById('fallback-domain-input');
    const domain = input.value.trim();
    if (!domain) return;
    try {
        await api('POST', '/api/admin/fallback/domains', { domain });
        input.value = '';
        await loadFallbackDomains();
    } catch (e) {
        alert('新增失敗（可能重複）');
    }
});

document.getElementById('btn-save-password').addEventListener('click', async () => {
    const pw = unlockPasswordInput.value.trim();
    if (!pw) return;
    await api('PUT', '/api/admin/settings', { unlock_password: pw });
    alert('已儲存');
    unlockPasswordInput.value = '';
});

workForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    await saveWork();
});

document.getElementById('btn-upload-image').addEventListener('click', uploadImage);

// ===== Functions =====
async function showDashboard() {
    loginSection.style.display = 'none';
    dashboard.style.display = 'block';
    await loadWorks();
}

async function loadWorks() {
    try {
        works = await api('GET', '/api/admin/works');
        renderWorks();
    } catch {
        works = [];
        renderWorks();
    }
}

function renderWorks() {
    worksTbody.innerHTML = '';
    if (!works || works.length === 0) {
        worksEmpty.style.display = 'block';
        return;
    }
    worksEmpty.style.display = 'none';

    works.forEach(work => {
        const cover = getCoverImage(work);
        const imgHtml = cover
            ? `<img src="/api/images/preview/${cover.filename}" alt="">`
            : '<span style="color:#ccc;">—</span>';

        const tr = document.createElement('tr');
        tr.draggable = true;
        tr.dataset.id = work.id;
        tr.innerHTML = `
            <td><span class="sort-handle" title="排序：${work.sort_order}">&#9776;</span></td>
            <td class="thumb-cell">${imgHtml}</td>
            <td>${escapeHtml(work.title)}</td>
            <td>${escapeHtml(work.price)}</td>
            <td><span class="status-badge ${work.published ? 'status-published' : 'status-draft'}">${work.published ? '已發布' : '草稿'}</span></td>
            <td>
                <button class="btn btn-outline" onclick="editWork(${work.id})">編輯</button>
                <button class="btn btn-danger" onclick="deleteWork(${work.id})">刪除</button>
            </td>
        `;
        worksTbody.appendChild(tr);
    });

    initDragSort();
}

// ===== Drag & Drop Sort =====
let dragRow = null;

function initDragSort() {
    const rows = worksTbody.querySelectorAll('tr');
    rows.forEach(row => {
        row.addEventListener('dragstart', (e) => {
            dragRow = row;
            row.style.opacity = '0.4';
            e.dataTransfer.effectAllowed = 'move';
        });
        row.addEventListener('dragend', () => {
            row.style.opacity = '1';
            dragRow = null;
            worksTbody.querySelectorAll('tr').forEach(r => r.classList.remove('drag-over'));
        });
        row.addEventListener('dragover', (e) => {
            e.preventDefault();
            e.dataTransfer.dropEffect = 'move';
            row.classList.add('drag-over');
        });
        row.addEventListener('dragleave', () => {
            row.classList.remove('drag-over');
        });
        row.addEventListener('drop', async (e) => {
            e.preventDefault();
            row.classList.remove('drag-over');
            if (!dragRow || dragRow === row) return;

            // Reorder in DOM
            const allRows = [...worksTbody.querySelectorAll('tr')];
            const fromIdx = allRows.indexOf(dragRow);
            const toIdx = allRows.indexOf(row);
            if (fromIdx < toIdx) {
                row.after(dragRow);
            } else {
                row.before(dragRow);
            }

            // Build new order and save
            const reordered = [...worksTbody.querySelectorAll('tr')].map((r, i) => ({
                id: parseInt(r.dataset.id),
                sort_order: i,
            }));
            try {
                await api('PUT', '/api/admin/works/reorder', { orders: reordered });
                await loadWorks();
            } catch {
                alert('排序儲存失敗');
                await loadWorks();
            }
        });
    });
}

function openWorkModal(work = null) {
    modalTitle.textContent = work ? '編輯作品' : '新增作品';
    document.getElementById('work-id').value = work ? work.id : '';
    document.getElementById('work-title').value = work ? work.title : '';
    document.getElementById('work-price').value = work ? work.price : '';
    document.getElementById('work-content').value = work ? work.content : '';
    document.getElementById('work-sort').value = work ? work.sort_order : 0;
    document.getElementById('work-published').checked = work ? work.published : false;

    // Show image section only for existing works
    const imgSection = document.getElementById('image-upload-section');
    if (work && work.id) {
        imgSection.style.display = 'block';
        renderUploadedImages(work);
    } else {
        imgSection.style.display = 'none';
    }

    workModal.classList.add('open');
}

function closeWorkModal() {
    workModal.classList.remove('open');
}

async function saveWork() {
    const id = document.getElementById('work-id').value;
    const data = {
        title: document.getElementById('work-title').value.trim(),
        price: document.getElementById('work-price').value.trim(),
        content: document.getElementById('work-content').value,
        sort_order: parseInt(document.getElementById('work-sort').value) || 0,
        published: document.getElementById('work-published').checked,
    };

    if (!data.title) return alert('請輸入作品名');

    if (id) {
        await api('PUT', `/api/admin/works/${id}`, data);
    } else {
        await api('POST', '/api/admin/works', data);
    }

    closeWorkModal();
    await loadWorks();
}

window.editWork = function(id) {
    const work = works.find(w => w.id === id);
    if (work) openWorkModal(work);
};

window.deleteWork = async function(id) {
    if (!confirm('確定要刪除此作品？')) return;
    await api('DELETE', `/api/admin/works/${id}`);
    await loadWorks();
};

function renderUploadedImages(work) {
    const container = document.getElementById('uploaded-images');
    container.innerHTML = '';
    if (!work.images || work.images.length === 0) {
        container.innerHTML = '<span style="color:#999;font-size:0.85rem;">尚未上傳圖片</span>';
        return;
    }

    work.images.forEach(img => {
        const div = document.createElement('div');
        div.className = 'uploaded-img';
        div.style.position = 'relative';
        const isCover = img.is_cover;
        div.innerHTML = `
            <img src="/api/images/preview/${img.filename}" alt="" style="border:2px solid ${isCover ? '#e63946' : 'transparent'};border-radius:6px;">
            ${isCover ? '<span style="position:absolute;bottom:2px;left:2px;background:#e63946;color:#fff;font-size:0.6rem;padding:1px 4px;border-radius:3px;">封面</span>' : ''}
            <div style="display:flex;gap:2px;margin-top:4px;">
                <button class="set-cover-btn" style="flex:1;font-size:0.7rem;padding:2px 4px;border:1px solid ${isCover ? '#e63946' : '#666'};background:${isCover ? '#e63946' : 'transparent'};color:${isCover ? '#fff' : '#999'};border-radius:3px;cursor:pointer;">${isCover ? '封面' : '設封面'}</button>
                <button class="remove-img" style="flex:0;font-size:0.7rem;padding:2px 6px;border:1px solid #666;background:transparent;color:#999;border-radius:3px;cursor:pointer;">&times;</button>
            </div>
        `;
        div.querySelector('.set-cover-btn').addEventListener('click', async () => {
            await api('PUT', `/api/admin/images/${img.id}/set-cover`);
            await loadWorks();
            const updatedWork = works.find(w => w.id === work.id);
            if (updatedWork) renderUploadedImages(updatedWork);
        });
        div.querySelector('.remove-img').addEventListener('click', async () => {
            if (!confirm('刪除此圖片？')) return;
            await api('DELETE', `/api/admin/images/${img.id}`);
            await loadWorks();
            const updatedWork = works.find(w => w.id === work.id);
            if (updatedWork) renderUploadedImages(updatedWork);
        });
        container.appendChild(div);
    });
}

async function uploadImage() {
    const workId = document.getElementById('work-id').value;
    if (!workId) return alert('請先儲存作品');

    const fileInput = document.getElementById('image-file');
    const files = Array.from(fileInput.files);
    if (files.length === 0) return alert('請選擇檔案');

    const progressEl = document.getElementById('upload-progress');
    const uploadBtn = document.getElementById('btn-upload-image');
    uploadBtn.disabled = true;

    let done = 0;
    let failed = 0;
    progressEl.textContent = `上傳中... 0 / ${files.length}`;

    // Upload up to 3 files concurrently
    const concurrency = 3;
    const queue = [...files];

    async function uploadNext() {
        while (queue.length > 0) {
            const file = queue.shift();
            const formData = new FormData();
            formData.append('file', file);
            formData.append('is_cover', 'false');

            try {
                const res = await fetch(`/api/admin/works/${workId}/images`, {
                    method: 'POST',
                    headers: { 'Authorization': `Bearer ${token}` },
                    body: formData,
                });
                if (!res.ok) failed++;
            } catch {
                failed++;
            }
            done++;
            progressEl.textContent = `上傳中... ${done} / ${files.length}${failed ? ` (${failed} 失敗)` : ''}`;
        }
    }

    const workers = [];
    for (let i = 0; i < Math.min(concurrency, files.length); i++) {
        workers.push(uploadNext());
    }
    await Promise.all(workers);

    progressEl.textContent = `完成！共 ${done} 張${failed ? `，${failed} 張失敗` : ''}`;
    setTimeout(() => { progressEl.textContent = ''; }, 3000);

    fileInput.value = '';
    uploadBtn.disabled = false;
    await loadWorks();
    const updatedWork = works.find(w => w.id === parseInt(workId));
    if (updatedWork) renderUploadedImages(updatedWork);
}

async function loadSettings() {
    try {
        const settings = await api('GET', '/api/admin/settings');
        unlockPasswordInput.value = '';
        unlockPasswordInput.placeholder = settings.unlock_password ? '目前已設定（輸入新密碼以覆蓋）' : '輸入密碼';
    } catch {}
}

// ===== Fallback Domains =====
async function loadFallbackDomains() {
    try {
        const domains = await api('GET', '/api/admin/fallback/domains');
        renderFallbackDomains(domains);
    } catch {
        renderFallbackDomains([]);
    }
}

function renderFallbackDomains(domains) {
    const tbody = document.getElementById('fallback-tbody');
    const empty = document.getElementById('fallback-empty');
    tbody.innerHTML = '';

    if (!domains || domains.length === 0) {
        empty.style.display = 'block';
        document.getElementById('fallback-table').style.display = 'none';
        return;
    }
    empty.style.display = 'none';
    document.getElementById('fallback-table').style.display = '';

    domains.forEach(d => {
        const tr = document.createElement('tr');
        tr.innerHTML = `
            <td>${escapeHtml(d.domain)}</td>
            <td>${d.active_assignments || 0}</td>
            <td><span class="status-badge ${d.is_active ? 'status-published' : 'status-draft'}">${d.is_active ? '啟用' : '停用'}</span></td>
            <td>
                <button class="btn btn-outline" onclick="toggleFallbackDomain(${d.id})">${d.is_active ? '停用' : '啟用'}</button>
                <button class="btn btn-danger" onclick="deleteFallbackDomain(${d.id})">刪除</button>
            </td>
        `;
        tbody.appendChild(tr);
    });
}

window.toggleFallbackDomain = async function(id) {
    await api('PUT', `/api/admin/fallback/domains/${id}/toggle`);
    await loadFallbackDomains();
};

window.deleteFallbackDomain = async function(id) {
    if (!confirm('確定要刪除此備援域名？')) return;
    await api('DELETE', `/api/admin/fallback/domains/${id}`);
    await loadFallbackDomains();
};

// ===== API Helper =====
async function api(method, url, body = null) {
    const opts = {
        method,
        headers: { 'Content-Type': 'application/json' },
    };
    if (token) opts.headers['Authorization'] = `Bearer ${token}`;
    if (body) opts.body = JSON.stringify(body);

    const res = await fetch(url, opts);
    if (res.status === 401) {
        token = '';
        sessionStorage.removeItem('admin_token');
        dashboard.style.display = 'none';
        loginSection.style.display = 'flex';
        throw new Error('unauthorized');
    }
    if (!res.ok) throw new Error(`API error ${res.status}`);
    return res.json();
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
