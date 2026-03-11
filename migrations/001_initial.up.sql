CREATE TABLE IF NOT EXISTS site_settings (
    key VARCHAR(100) PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS works (
    id SERIAL PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    price VARCHAR(100) NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    published BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 每筆 row = 一張圖，後端自動產生 preview/thumb/full 三個版本
-- 三個版本共用同一個 filename，存在不同目錄
CREATE TABLE IF NOT EXISTS work_images (
    id SERIAL PRIMARY KEY,
    work_id INT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    filename VARCHAR(255) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    is_cover BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_work_images_work_id ON work_images(work_id);
CREATE INDEX idx_works_published ON works(published, sort_order);

-- Phase 3: 備援域名相關表
CREATE TABLE IF NOT EXISTS fallback_domains (
    id SERIAL PRIMARY KEY,
    domain VARCHAR(255) NOT NULL UNIQUE,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS client_domain_assignments (
    id SERIAL PRIMARY KEY,
    client_id VARCHAR(64) NOT NULL,
    domain_id INT NOT NULL REFERENCES fallback_domains(id) ON DELETE CASCADE,
    assigned_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    is_valid BOOLEAN NOT NULL DEFAULT true,
    UNIQUE(client_id, domain_id)
);

-- 初始設定
INSERT INTO site_settings (key, value) VALUES ('unlock_password', 'demo123')
ON CONFLICT (key) DO NOTHING;
