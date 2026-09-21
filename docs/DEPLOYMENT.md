# BookLoop 部署与上线操作手册

本文档用于把 BookLoop 从 GitHub 部署到一台 Ubuntu 云服务器。建议先在本地完成 Phase 8 的 Compose 全流程验收，再按本文操作服务器环境。

可以分两步上线：

1. **公网 IP 测试部署**：只要有云服务器和公网 IP 就能完成，用 `http://服务器公网IP` 验证完整业务流程。
2. **域名 HTTPS 正式部署**：准备域名后增加 DNS 和 TLS，完成 Phase 9 的正式验收。

公网 IP 模式可以验证应用是否可用，但登录凭证通过明文 HTTP 传输，不应开放给真实用户长期使用。

生产拓扑：

```text
Internet
   │ 测试阶段 HTTP :80 / 正式阶段 HTTPS :443
   ▼
宿主机 Nginx + Let's Encrypt
   │ HTTP 127.0.0.1:8088
   ▼
Compose Nginx
   ├── Vue 静态文件
   ├── /uploads/ → book_uploads 卷
   └── /api/     → Go backend → MySQL / Redis
```

MySQL、Redis 和 Go backend 不发布宿主机端口。Compose 入口只绑定 `127.0.0.1:8088`，公网只开放宿主机 Nginx 的 80/443。

> 重要：命令中的 `bookloop.example.com`、邮箱、`SERVER_IP` 等均为占位内容，必须替换。不要把 `.env`、密码、JWT 密钥、私钥或数据库备份提交到 Git。

## 1. 上线前准备

你需要：

- 一台 64 位 Ubuntu 22.04、24.04 或 26.04 LTS 云服务器，建议至少 2 核 CPU、2 GB 内存、30 GB 磁盘。
- 一台带固定公网 IPv4 的云服务器；公网 IP 测试阶段不需要域名。
- 正式 HTTPS 上线时再准备域名及其 DNS 管理权限，例如 `bookloop.example.com`。
- 可使用 `sudo` 的服务器账号。
- 已配置到服务器的 SSH 公钥。
- 一个服务器之外的备份保存位置。

本手册假设代码目录是：

```text
/srv/bookloop
```

## 2. 选择部署阶段

### 阶段 A：先使用公网 IP 测试

只有云服务器时，先完成第 3～8 节，然后通过下面的地址访问：

```text
http://SERVER_IP
```

阶段 A 暂时跳过 DNS、HTTPS 证书、HTTP 自动跳转 HTTPS，以及 HSTS 等仅适用于 HTTPS 的配置。

阶段 A 仍然必须使用随机生产密码和 JWT 密钥，只开放 22 和 80 端口，保持 MySQL、Redis、backend 和 8088 不对公网开放，并完成完整业务闭环及备份恢复演练。

### 阶段 B：增加域名和 HTTPS

准备好域名后，再执行下面的 DNS 配置以及第 9 节 HTTPS 配置。

### 2.1 配置 DNS（阶段 B）

在域名服务商控制台添加：

| 类型 | 主机记录 | 值 |
|---|---|---|
| A | `bookloop` | 云服务器公网 IPv4 |

如果直接使用根域名，主机记录通常填写 `@`。等待解析生效后，在本机检查：

```bash
nslookup bookloop.example.com
```

返回的地址必须是服务器公网 IP，之后才能申请 HTTPS 证书。阶段 A 没有域名时直接跳过本小节。

## 3. 初始化 Ubuntu 服务器

登录服务器：

```bash
ssh YOUR_USER@SERVER_IP
```

更新系统并安装基础工具：

```bash
sudo apt update
sudo apt upgrade -y
sudo apt install -y ca-certificates curl git nginx snapd ufw openssl
```

### 3.1 安装 Docker Engine 与 Compose

生产服务器应使用 Docker 官方软件源，不建议使用便利安装脚本。执行：

```bash
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
```

用编辑器创建 `/etc/apt/sources.list.d/docker.sources`：

```text
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: noble
Components: stable
Architectures: amd64
Signed-By: /etc/apt/keyrings/docker.asc
```

上面的 `Suites` 和 `Architectures` 必须与服务器匹配：Ubuntu 22.04 使用 `jammy`，24.04 使用 `noble`，26.04 使用 `resolute`；ARM64 服务器使用 `arm64`。

然后安装并验证：

```bash
sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo systemctl enable --now docker
sudo docker version
sudo docker compose version
```

可选：允许当前用户执行 Docker。注意，`docker` 组拥有接近 root 的权限。

```bash
sudo usermod -aG docker "$USER"
```

执行后退出 SSH 并重新登录。若不加入该组，本文所有 `docker` 命令前加 `sudo`。

Docker 官方安装说明：<https://docs.docker.com/engine/install/ubuntu/>

### 3.2 配置容器日志轮转

Docker 默认的 `json-file` 日志可能持续占用磁盘。建议用编辑器创建或合并 `/etc/docker/daemon.json`：

```json
{
  "log-driver": "local",
  "log-opts": {
    "max-size": "20m",
    "max-file": "5"
  }
}
```

验证配置后重启 Docker：

```bash
sudo dockerd --validate --config-file=/etc/docker/daemon.json
sudo systemctl restart docker
docker info --format '{{.LoggingDriver}}'
```

输出应为 `local`。该设置只对之后新建的容器生效。

Docker 日志说明：<https://docs.docker.com/engine/logging/configure/>

## 4. 拉取项目代码

创建部署目录并赋予当前用户：

```bash
sudo mkdir -p /srv/bookloop
sudo chown "$USER":"$USER" /srv/bookloop
git clone git@github.com:gxiaopang/bookloop.git /srv/bookloop
cd /srv/bookloop
git switch main
git status
```

如果服务器还没有 GitHub SSH key，先在服务器生成公钥并添加到 GitHub。不要复制或上传私钥。

部署前记录版本：

```bash
git rev-parse HEAD
```

## 5. 创建生产环境变量

复制模板并限制权限：

```bash
cd /srv/bookloop
cp .env.example .env
chmod 600 .env
```

分别生成随机值，并暂时保存在安全的密码管理器中：

```bash
openssl rand -hex 24
openssl rand -hex 24
openssl rand -hex 24
openssl rand -hex 32
```

前三个值分别用于 MySQL 用户密码、MySQL root 密码和 Redis 密码；最后一个用于 JWT 密钥。每项必须使用不同值。

编辑 `.env`：

```bash
nano .env
```

至少修改为：

```dotenv
APP_ENV=production
LOG_LEVEL=info

BOOKLOOP_BIND_ADDRESS=127.0.0.1
BOOKLOOP_HTTP_PORT=8088

MYSQL_DATABASE=bookloop
MYSQL_USER=bookloop
MYSQL_PASSWORD=替换为独立随机值
MYSQL_ROOT_PASSWORD=替换为独立随机值
DB_MAX_OPEN_CONNS=25
DB_MAX_IDLE_CONNS=10

REDIS_PASSWORD=替换为独立随机值
REDIS_MAXMEMORY=128mb

JWT_SECRET=替换为至少32字节的独立随机值
JWT_ISSUER=bookloop
JWT_EXPIRES_IN=15m
BCRYPT_COST=12

UPLOAD_MAX_BYTES=10485760
ORDER_PAYMENT_TIMEOUT=15m
IDEMPOTENCY_PROCESSING_TTL=30s
IDEMPOTENCY_RESULT_TTL=24h
IDEMPOTENCY_ALLOW_DEGRADED=true
```

不要在变量值两侧添加多余空格。完成后检查权限与 Compose 配置：

```bash
stat -c '%a %n' .env
docker compose config --quiet
```

权限应为 `600`，配置检查不应输出错误。不要执行会把完整生产环境变量打印到终端或日志的命令。

## 6. 首次启动 Compose

执行：

```bash
cd /srv/bookloop
docker compose up --build -d --wait
```

检查服务与迁移：

```bash
docker compose ps
docker compose logs migrate
docker compose logs --tail=100 backend nginx
curl --fail http://127.0.0.1:8088/healthz
curl --fail http://127.0.0.1:8088/readyz
```

预期结果：

- MySQL、Redis、backend、Nginx 都是 `healthy`。
- `migrate` 状态为成功退出。
- `/healthz` 返回 `alive`。
- `/readyz` 返回 `ready`，MySQL 和 Redis 均为 `up`。

若迁移失败，不要反复删除数据卷重试。先执行 `docker compose logs migrate mysql` 找到原因。

## 7. 配置宿主机 Nginx

### 7.1 公网 IP 测试配置（阶段 A）

创建 `/etc/nginx/sites-available/bookloop`：

```nginx
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;

    server_tokens off;
    client_max_body_size 12m;

    location / {
        proxy_pass http://127.0.0.1:8088;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_connect_timeout 3s;
        proxy_read_timeout 30s;
        proxy_send_timeout 30s;
    }
}
```

启用配置：

```bash
sudo ln -s /etc/nginx/sites-available/bookloop /etc/nginx/sites-enabled/bookloop
sudo rm -f /etc/nginx/sites-enabled/default
sudo nginx -t
sudo systemctl enable --now nginx
sudo systemctl reload nginx
```

从你自己的电脑访问：

```text
http://SERVER_IP
```

检查公网入口：

```bash
curl --fail http://SERVER_IP/healthz
curl --fail http://SERVER_IP/readyz
```

如果无法访问，先检查云厂商安全组和第 8 节 UFW 配置。不要为了排错而把 3306、6379 或 8088 开放到公网。

### 7.2 域名配置（阶段 B）

已有域名时，将 `/etc/nginx/sites-available/bookloop` 修改为下面的配置，并将域名替换为真实值：

```nginx
server {
    listen 80;
    listen [::]:80;
    server_name bookloop.example.com;

    server_tokens off;
    client_max_body_size 12m;

    location / {
        proxy_pass http://127.0.0.1:8088;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_connect_timeout 3s;
        proxy_read_timeout 30s;
        proxy_send_timeout 30s;
    }
}
```

检查并重新加载：

```bash
sudo nginx -t
sudo systemctl reload nginx
```

此时下面的 HTTP 地址应能访问：

```text
http://bookloop.example.com
```

## 8. 配置防火墙

先确认当前 SSH 连接正常。阶段 A 的云厂商安全组只需允许 22、80；阶段 B 再增加 443。阶段 A 执行：

```bash
sudo ufw allow OpenSSH
sudo ufw allow 'Nginx HTTP'
sudo ufw enable
sudo ufw status verbose
```

升级到阶段 B 时执行：

```bash
sudo ufw allow 'Nginx Full'
sudo ufw delete allow 'Nginx HTTP'
sudo ufw status verbose
```

公网规则只应允许必要端口。不要开放 3306、6379、8080 或 8088。Compose 已将入口绑定在 `127.0.0.1:8088`。

> Docker 官方提醒：公开映射的容器端口可能绕过部分 UFW 规则，因此不能仅依赖 UFW；本项目同时通过只绑定 loopback 和不发布 MySQL/Redis 端口降低风险。

## 9. 申请 HTTPS 证书（仅阶段 B）

确保 DNS 已生效、80/443 已开放、HTTP 网站可访问，然后安装 Certbot：

```bash
sudo snap install --classic certbot
sudo ln -s /snap/bin/certbot /usr/local/bin/certbot
```

申请证书并让 Certbot 修改 Nginx 配置：

```bash
sudo certbot --nginx -d bookloop.example.com
```

按提示选择把 HTTP 重定向到 HTTPS。验证：

```bash
curl -I http://bookloop.example.com
curl --fail https://bookloop.example.com/healthz
curl --fail https://bookloop.example.com/readyz
sudo certbot renew --dry-run
systemctl list-timers | grep certbot
```

预期 HTTP 返回 301/308，HTTPS 健康检查返回 200。

Certbot 官方说明：<https://certbot.eff.org/instructions?os=snap&ws=nginx>

## 10. 初始化首个管理员

1. 先通过网页正常注册一个专用管理员账号。
2. 在服务器打开 MySQL 客户端：

```bash
cd /srv/bookloop
docker compose exec mysql sh -c 'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" "$MYSQL_DATABASE"'
```

3. 在 MySQL 提示符中执行，并替换邮箱：

```sql
UPDATE users
SET role = 'admin'
WHERE email = 'admin@example.com' AND status = 'active';

SELECT id, username, email, role, status
FROM users
WHERE email = 'admin@example.com';
```

确认结果只有目标用户且 `role` 为 `admin`，然后执行 `exit`。管理员需要退出网页并重新登录，前端才会显示“管理”入口。

## 11. 上线验收

### 11.1 公网 IP 测试验收（阶段 A）

- [ ] `http://SERVER_IP` 可以从自己的电脑访问。
- [ ] `/healthz` 和 `/readyz` 返回 200。
- [ ] 刷新任意 Vue 页面不会出现 404。
- [ ] 注册两个普通用户，密码没有出现在 API 响应或日志中。
- [ ] 卖家创建图书、上传 1～6 张 JPEG/PNG、排序并上架。
- [ ] 图片通过公网 IP 正常显示。
- [ ] 买家收藏图书。
- [ ] 买家创建订单，同一本书不能被第二个买家重复购买。
- [ ] 买家完成模拟支付与确认完成。
- [ ] 图书最终为已售，订单最终为已完成。
- [ ] 重启容器后用户、订单和图片仍存在。
- [ ] 公网无法访问 3306、6379、8080、8088。
- [ ] 完成一次 MySQL 与图片联合备份和恢复演练。

阶段 A 通过后，说明应用已经能在公网运行，但因为没有 HTTPS，只能用于你自己的短期验收。

### 11.2 域名 HTTPS 正式验收（阶段 B）

- [ ] HTTP 自动跳转 HTTPS。
- [ ] HTTPS 证书域名正确且浏览器无警告。
- [ ] `/healthz` 和 `/readyz` 返回 200。
- [ ] 图片通过 HTTPS 正常显示。
- [ ] 公网无法访问 3306、6379、8080、8088。
- [ ] 管理员可以查询订单并封禁/恢复图书。
- [ ] Certbot 自动续期演练成功。
- [ ] 再执行一次注册、登录和核心交易冒烟测试。

重启持久化验证：

```bash
docker compose down
docker compose up -d --wait
docker compose ps
```

不要添加 `-v`；`docker compose down -v` 会删除数据库和图片卷。

## 12. 日常发布更新

每次发布前先备份，记录当前提交：

```bash
cd /srv/bookloop
git status
git rev-parse HEAD
git fetch origin
git log --oneline --decorate HEAD..origin/main
```

服务器工作区必须干净。更新并发布：

```bash
git pull --ff-only origin main
docker compose build
docker compose run --rm migrate
docker compose up -d --wait
docker compose ps
```

根据当前阶段执行一条外部就绪检查：

```bash
# 阶段 A：公网 IP 测试
curl --fail http://SERVER_IP/readyz

# 阶段 B：域名 HTTPS 正式部署
curl --fail https://bookloop.example.com/readyz
```

随后重新执行核心交易冒烟测试。不要在服务器上直接修改源码；修改应先进入 GitHub。

## 13. MySQL 与图片联合备份

核心事实数据在 MySQL，图片在 `bookloop_book_uploads` 卷。两者必须作为同一批次备份。为了获得清晰的一致性边界，下面的手工流程会短暂停止业务入口和后台订单过期任务，但不会停止 MySQL。

```bash
cd /srv/bookloop
BOOKLOOP_BACKUP_STAMP=$(date -u +%Y%m%dT%H%M%SZ)
BOOKLOOP_BACKUP_DIR="/srv/bookloop-backups/$BOOKLOOP_BACKUP_STAMP"
sudo install -d -m 700 -o "$USER" -g "$USER" "$BOOKLOOP_BACKUP_DIR"

docker compose stop nginx backend

docker compose exec -T mysql sh -c 'exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction --routines --triggers --events "$MYSQL_DATABASE"' > "$BOOKLOOP_BACKUP_DIR/mysql.sql"

docker run --rm \
  -v bookloop_book_uploads:/data:ro \
  -v "$BOOKLOOP_BACKUP_DIR":/backup \
  alpine:3.22 \
  tar -czf /backup/uploads.tar.gz -C /data .

sha256sum "$BOOKLOOP_BACKUP_DIR/mysql.sql" "$BOOKLOOP_BACKUP_DIR/uploads.tar.gz" > "$BOOKLOOP_BACKUP_DIR/SHA256SUMS"

docker compose up -d --wait backend nginx
```

立即验证：

```bash
cd "$BOOKLOOP_BACKUP_DIR"
sha256sum -c SHA256SUMS
test -s mysql.sql
tar -tzf uploads.tar.gz | head
```

要求：

- 每天至少一次自动备份；重要发布前额外备份。
- 备份目录权限保持为 700。
- 将备份复制到服务器之外的受控位置。
- 制定保留周期，例如每日 7 份、每周 4 份、每月 6 份。
- 不能只确认文件存在；必须定期完成恢复演练。

## 14. 在新服务器进行恢复演练

恢复演练优先使用独立服务器，避免覆盖生产数据。

1. 安装相同版本的 Docker 和 Compose。
2. 检出备份时对应的 Git 提交。
3. 创建新的生产 `.env`，其中数据库名保持一致。
4. 把 `mysql.sql`、`uploads.tar.gz` 和 `SHA256SUMS` 放入受限目录。
5. 验证校验和：

```bash
sha256sum -c SHA256SUMS
```

6. 启动数据库和 Redis：

```bash
cd /srv/bookloop
docker compose up -d mysql redis
docker compose ps
```

7. MySQL 健康后导入：

```bash
docker compose exec -T mysql sh -c 'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" "$MYSQL_DATABASE"' < /path/to/mysql.sql
```

8. 将图片恢复到新建的空卷：

```bash
docker volume create bookloop_book_uploads
docker run --rm \
  -v bookloop_book_uploads:/data \
  -v /path/to/backup:/backup:ro \
  alpine:3.22 \
  tar -xzf /backup/uploads.tar.gz -C /data
```

9. 启动完整系统：

```bash
docker compose up -d --wait
docker compose ps
```

10. 验证用户、图书、图片、收藏和订单均存在，并完成一次登录和订单查询。

只有恢复演练成功，才能认为备份可用。生产原地恢复会覆盖现有数据，必须先停服、额外备份并单独审批，不要直接照搬演练命令覆盖生产卷。

## 15. 应用回滚

回滚前先判断数据库迁移是否向后兼容。MySQL DDL 可能隐式提交，不能假设代码回滚会自动回滚数据库。

如果本次发布没有不兼容迁移：

```bash
cd /srv/bookloop
git log --oneline -10
git switch --detach PREVIOUS_GOOD_COMMIT
docker compose up --build -d --wait
```

根据当前阶段验证回滚后的服务：

```bash
# 阶段 A
curl --fail http://SERVER_IP/readyz

# 阶段 B
curl --fail https://bookloop.example.com/readyz
```

恢复到最新 `main`：

```bash
git switch main
git pull --ff-only origin main
docker compose up --build -d --wait
```

如果存在不兼容迁移，停止发布。只能执行经过审查的向下迁移，或在新环境恢复发布前的 MySQL 与图片联合备份。不要在故障现场临时修改已应用的迁移文件。

## 16. 日常运维

健康与日志：

```bash
cd /srv/bookloop
docker compose ps
docker compose logs --since=30m backend nginx mysql redis
df -h
docker system df
```

根据当前阶段检查对外接口：

```bash
# 阶段 A
curl --fail http://SERVER_IP/healthz
curl --fail http://SERVER_IP/readyz

# 阶段 B
curl --fail https://bookloop.example.com/healthz
curl --fail https://bookloop.example.com/readyz
```

建议配置外部监控每分钟访问 `/healthz` 和 `/readyz`，并对以下事件告警：

- 对外 HTTP/HTTPS 不可访问；阶段 B 还需监控证书有效期。
- `/readyz` 非 200。
- 容器频繁重启。
- 磁盘剩余空间低于 20%。
- 最近一次备份失败或超过 24 小时未生成。
- MySQL 或上传图片卷不可读。

阶段 B 查看并演练证书续期；阶段 A 跳过：

```bash
sudo certbot certificates
sudo certbot renew --dry-run
```

## 17. 常见问题

### 页面返回 502

```bash
docker compose ps
docker compose logs --tail=200 backend nginx
curl --fail http://127.0.0.1:8088/readyz
sudo nginx -t
```

### `migrate` 失败

```bash
docker compose logs migrate mysql
docker compose run --rm migrate
```

不要修改已经应用的 SQL migration；新增迁移文件修复。

### 图片上传失败

检查浏览器响应中的 Request ID，然后查询：

```bash
docker compose logs backend nginx | grep REQUEST_ID
docker volume inspect bookloop_book_uploads
```

同时确认宿主机和容器 Nginx 的请求体上限均不小于后端的 `UPLOAD_MAX_BYTES`。

### 服务器重启后服务没有恢复

```bash
sudo systemctl status docker nginx
docker compose ps -a
docker compose up -d --wait
```

## 18. 当前 MVP 边界

- 支付为模拟支付，不接入真实支付渠道。
- 不包含物流、聊天、退款和资金结算。
- JWT 为短期 access token，客户端退出登录即丢弃。
- 图片保存在单机 Docker 卷，因此备份与恢复必须同时覆盖 MySQL 和图片卷。
- 当前管理员页面支持订单审计和图书封禁/恢复；用户列表、禁用/恢复用户仍需后续补齐。
- Phase 10 的 CI/CD、不可变镜像标签和部署审批尚未替代本文的手工发布流程。

完成第 11.1 节后，可以认为“公网 IP 部署和功能测试”已通过，但还不能将 Phase 9 标记为正式完成。准备好域名后，再完成第 9 节 HTTPS、第 11.2 节正式验收、至少一次第 14 节恢复演练和一次第 15 节回滚演练，才应将 Phase 9 标记为完成。
