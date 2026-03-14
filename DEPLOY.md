# 部署指南（Proxmox LXC 裸機部署）

本文件說明如何在 Proxmox VE 的 LXC 容器中部署 Menu Grid，不使用 Docker。

---

## 一、建立 LXC 容器

在 Proxmox Web UI 中建立 CT：

| 項目 | 建議值 |
|------|--------|
| 模板 | Debian 12 (Bookworm) |
| CPU | 1 核 |
| 記憶體 | 512 MB |
| Swap | 256 MB |
| 硬碟 | 10 GB（依圖片量調整） |
| 網路 | DHCP 或靜態 IP |

不需要開啟 nesting 或其他特殊功能。

---

## 二、系統初始化

進入 CT 後執行：

```bash
apt update && apt upgrade -y
apt install -y git gcc libwebp-dev curl postgresql
```

### 安裝 Go

```bash
curl -fsSL https://go.dev/dl/go1.25.7.linux-amd64.tar.gz | tar -C /usr/local -xz
echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
source /etc/profile
go version
```

---

## 三、設定 PostgreSQL

```bash
systemctl enable --now postgresql

sudo -u postgres psql <<EOF
CREATE DATABASE menu_grid;
CREATE USER menu_grid WITH PASSWORD '在此填入資料庫密碼';
GRANT ALL PRIVILEGES ON DATABASE menu_grid TO menu_grid;
ALTER DATABASE menu_grid OWNER TO menu_grid;
EOF
```

驗證連線：

```bash
psql -U menu_grid -d menu_grid -h localhost -c "SELECT 1;"
```

---

## 四、部署應用程式

### 取得原始碼

```bash
mkdir -p /opt/menu_grid
cd /opt/menu_grid
git clone <你的 Git 倉庫網址> .
```

### 編譯

```bash
cd /opt/menu_grid
CGO_ENABLED=1 go build -o server ./cmd/server
```

### 建立目錄與設定檔

```bash
# 圖片上傳目錄
mkdir -p /opt/menu_grid/uploads/{full,thumb,preview}

# 環境變數設定
cat > /opt/menu_grid/.env <<'EOF'
DATABASE_URL=postgres://menu_grid:在此填入資料庫密碼@localhost:5432/menu_grid?sslmode=disable
ADMIN_PASSWORD=在此填入後台管理密碼
JWT_SECRET=在此填入隨機字串
PORT=8080
UPLOAD_DIR=/opt/menu_grid/uploads
EOF

# 保護設定檔
chmod 600 /opt/menu_grid/.env
```

### 環境變數說明

| 變數 | 說明 |
|------|------|
| `DATABASE_URL` | PostgreSQL 連線字串 |
| `ADMIN_PASSWORD` | 後台管理員登入密碼 |
| `JWT_SECRET` | JWT 簽章用的隨機字串，建議 32 字元以上 |
| `PORT` | 服務監聽埠，預設 8080 |
| `UPLOAD_DIR` | 圖片儲存路徑 |

> `ADMIN_PASSWORD` 是後台登入密碼，前台解鎖密碼在後台「設定」中另外設定。

---

## 五、建立 Systemd 服務

```bash
cat > /etc/systemd/system/menu-grid.service <<'EOF'
[Unit]
Description=Menu Grid Server
After=postgresql.service
Requires=postgresql.service

[Service]
Type=simple
WorkingDirectory=/opt/menu_grid
ExecStart=/opt/menu_grid/server
EnvironmentFile=/opt/menu_grid/.env
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now menu-grid
```

確認服務狀態：

```bash
systemctl status menu-grid
curl -s http://localhost:8080 | head -5
```

---

## 六、設定反向代理（Caddy）

Caddy 會自動申請並續期 HTTPS 憑證。

### 安裝 Caddy

```bash
apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
apt update && apt install -y caddy
```

### 設定 Caddyfile

```bash
cat > /etc/caddy/Caddyfile <<'EOF'
yourdomain.com {
    reverse_proxy localhost:8080
    request_body {
        max_size 50MB
    }
}
EOF

systemctl restart caddy
```

將 `yourdomain.com` 替換為你的實際域名，並確保 DNS A 記錄已指向此 CT 的外部 IP。

> 如果 CT 位於 NAT 後方，需在 Proxmox host 或路由器上設定 port forward（80、443 → CT IP）。

---

## 七、防火牆（選用）

```bash
apt install -y ufw
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 22/tcp
ufw enable
```

---

## 八、備份

### 資料庫備份

```bash
# 手動備份
pg_dump -U menu_grid menu_grid > /opt/menu_grid/backup/db_$(date +%Y%m%d).sql

# 排程備份（每天凌晨 3 點）
mkdir -p /opt/menu_grid/backup
cat > /etc/cron.d/menu-grid-backup <<'EOF'
0 3 * * * root pg_dump -U menu_grid menu_grid > /opt/menu_grid/backup/db_$(date +\%Y\%m\%d).sql && find /opt/menu_grid/backup -name "db_*.sql" -mtime +7 -delete
EOF
```

### 圖片備份

圖片存放在 `/opt/menu_grid/uploads/`，建議定期備份此目錄，或搭配 Proxmox 的 CT 快照功能。

---

## 九、更新程式

```bash
cd /opt/menu_grid
git pull
CGO_ENABLED=1 go build -o server ./cmd/server
systemctl restart menu-grid
```

---

## 十、常用指令

```bash
# 查看服務狀態
systemctl status menu-grid

# 查看即時日誌
journalctl -u menu-grid -f

# 重啟服務
systemctl restart menu-grid

# 停止服務
systemctl stop menu-grid

# 進入資料庫
psql -U menu_grid -d menu_grid -h localhost
```

---

## 故障排除

| 問題 | 排查方式 |
|------|----------|
| 服務啟動失敗 | `journalctl -u menu-grid -n 50` 看錯誤訊息 |
| 資料庫連不上 | 確認 `systemctl status postgresql` 是否在運行 |
| 圖片上傳失敗 | 確認 `uploads/` 目錄權限，`ls -la /opt/menu_grid/uploads/` |
| HTTPS 憑證失敗 | 確認域名 DNS 已生效，`journalctl -u caddy -n 50` |
| 502 Bad Gateway | 確認 menu-grid 服務是否在運行，port 8080 是否正確 |

---