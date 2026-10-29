# CI/CD 与服务器上线

GitHub Actions 在 PR 和 main 提交上执行检查，main 检查通过后发布镜像。
生产部署必须手动执行 **Actions → CI and release → Run workflow → main → deploy=true**。
该操作重新验证选定提交，再部署同一批通过 Compose 检查的镜像。

流程：Go 格式/vet/单元测试/真实 MySQL 与 Redis 集成测试/race 检查 + 前端依赖审计/测试/类型检查/构建
→ Docker 构建 → 完整 Compose 启动和 HTTP 检查 → GHCR 发布
→ 手动生产发布与 Environment 规则 → SSH → 联合备份 → SQL 迁移 → 健康检查。

应用继续使用模块化单体与原有 Docker Compose。与手工 `git pull && docker compose build`
流程相比，服务器改为拉取 CI 已测试的镜像，避免服务器重新编译出另一份产物。
每个发布保存对应提交 SHA、Compose 文件和镜像 digest；不使用浮动 `latest` 标签发布应用。
当前 CI 构建 Linux AMD64 镜像；ARM64 服务器需要先调整构建平台和验证流程。

## 1. 服务器准备

需要 Ubuntu、Docker Engine、Docker Compose **2.24.4 或更高版本**、Bash、Git、curl、tar、
flock（util-linux）、sha256sum（coreutils）以及宿主机 Nginx。
SSH 部署用户须能使用 Docker。加入 docker 组等同于赋予很高的宿主机权限，请使用专用账号与密钥。
服务器需要能拉取 Docker Hub 基础服务镜像和 GHCR 应用镜像。

先在服务器安装 Docker/Nginx，并配置安全组，仅公开 SSH 和 80/443。
应用只监听 `127.0.0.1:8088`；MySQL、Redis、RabbitMQ、backend 均不公开端口。

首次创建目录，以下 `DEPLOY_USER` 替换为实际账号：

```bash
sudo install -d -m 750 -o DEPLOY_USER -g DEPLOY_USER /srv/bookloop
```

将仓库根目录 `.env.example` 复制到服务器 `/srv/bookloop/.env`，设为 `chmod 600`。
设置 `APP_ENV=production`，并将以下五项分别替换为独立随机值：

- `MYSQL_PASSWORD`
- `MYSQL_ROOT_PASSWORD`
- `REDIS_PASSWORD`
- `RABBITMQ_PASSWORD`
- `JWT_SECRET`（至少 32 字节）

可在服务器用 `openssl rand -hex 32` 分别生成，不要把生成值贴到聊天或提交到 Git。
保留 `BOOKLOOP_HTTP_PORT=8088`。生产 Compose 会强制回环地址绑定和 production 环境，
移除源码构建入口，并为容器设置日志轮转。凭据只保存在服务器，不经过 CI。

已有运行中的 `bookloop` 项目或数据卷时，先检查并备份。脚本拒绝自动接管没有
`current` 发布记录的既有容器；空服务器首次上线和现有数据迁移是不同操作。
不要为了让首次部署通过而删除容器或数据卷。旧迁移 `000007` 删除 `paid_at` 并改变订单状态约束，
包含旧订单的数据库必须先制定兼容迁移方案，不能直接清空生产订单。

### GHCR 读取权限

发布及部署均使用本次工作流的短期 `GITHUB_TOKEN`。部署作业只有 packages:read 权限，
通过 SSH 标准输入传递令牌，使用发布目录内的临时 Docker 配置登录 GHCR；退出时删除认证文件。
服务器不需要保存长期令牌，也不必公开 Package。不要将令牌放进制品或 Compose 配置。
Package 需要关联本仓库并允许本仓库 Actions 读取，工作流创建的新 Package 会自动关联。
人工发布新版本时才需要单独 `docker login ghcr.io`；回滚优先使用保留的本地镜像。

## 2. GitHub 配置

在仓库 **Settings → Environments** 创建 `production`，限制 Deployment branches 为 `main`。
可配置 Required reviewers 作为额外审批；单人维护时注意禁止自审可能让自己无法发布。
工作流本身只在手动触发并选中 deploy 时进入部署作业；Environment 审批规则需要在网页上设置，
YAML 中引用 environment 并不会自动创建审批保护。

在 `production` Environment 配置 Variables：

当前工作流已有本项目的默认值：`8.137.191.24`、`bookloop-deploy`、端口 `22`、
目录 `/srv/bookloop`、入口 `https://byteneat.top`。当前服务器无需重复填写 Variables；
迁移服务器或更改域名时，再用以下变量覆盖。

| 名称 | 示例/含义 |
|---|---|
| `DEPLOY_HOST` | 服务器公网 IP 或 SSH 域名 |
| `DEPLOY_USER` | 专用 SSH 用户 |
| `DEPLOY_PORT` | `22`，省略时使用 22 |
| `DEPLOY_ROOT` | `/srv/bookloop`，绝对路径，仅使用字母、数字、`/`、`_`、`-` |
| `DEPLOY_URL` | `https://bookloop.example.com`；仅测试时可用公网 IP 的 HTTP 地址 |

配置 Secrets：

| 名称 | 内容 |
|---|---|
| `DEPLOY_SSH_KEY` | 专用 CI SSH 私钥，对应公钥写入部署账号的 `authorized_keys` |
| `DEPLOY_KNOWN_HOSTS` | 已独立核验的服务器 SSH host key，非标准端口使用 `[host]:port` 格式 |

私钥通过 GitHub 的 Secrets 页面输入，不放入仓库。SSH 使用严格主机校验，不在工作流中
临时信任 `ssh-keyscan` 返回值。应通过云控制台或已有可信 SSH 通道核对指纹；
`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` 可以在服务器显示指纹。

Actions 的工作流权限须允许 `GITHUB_TOKEN` 发布本仓库关联的 Packages。
建议保护 main 分支，要求 `backend`、`frontend`、`images` 检查通过后合并。

## 3. Nginx 与 HTTPS

宿主机 Nginx 的站点配置转发到回环地址，域名需要替换：

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
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_connect_timeout 3s;
        proxy_read_timeout 30s;
    }
}
```

放入 `/etc/nginx/sites-available/bookloop`，创建对应 sites-enabled 链接，
用 `sudo nginx -t` 检查后 reload。不要覆盖服务器上其他站点。
DNS A 记录指向服务器后，可通过 Certbot Nginx 插件申请证书与启用 HTTP → HTTPS 跳转，
并运行 `sudo certbot renew --dry-run` 验证续期。没有域名时只能先完成 IP/HTTP 测试，
正式开放登录和真实用户前应完成 HTTPS。

## 4. 首次发布及后续更新

1. 将本地需要发布的业务代码和 CI/CD 配置审查后提交到 main；未提交修改不会进入镜像。
2. 等待 main 流水线检查和镜像发布成功。
3. 完成服务器 `.env`、Nginx 和 GitHub Environment 配置。
4. 在 Actions 手动运行 main 的工作流，选中 deploy，按已设置的 Environment 规则审批。
5. 检查作业结果，并访问公网首页、`/healthz`、`/readyz`。浏览器验证注册、登录、
   发布实物图/微信二维码、收藏、预约、取消和线下交付确认，以及买卖双方的通知。

发布目录：

```text
/srv/bookloop/
├── .env                    # 600，仅服务器保存
├── .deploy.lock            # 发布、备份、回滚共用锁
├── releases/
│   └── SHA-RUN_ID-ATTEMPT/  # Compose、脚本、release.env、schema.txt
├── current -> releases/…   # 最近成功发布
├── previous -> releases/…  # 上一个成功发布
└── backups/                # 每次更新前的 MySQL + 图片备份
```

所有发布使用固定 Compose project name `bookloop`，因此更换发布目录不会更换数据卷。
GitHub 的 concurrency 和服务器 flock 防止发布与备份同时操作数据库。
发布前先下载镜像，再停止 nginx、backend、worker，备份所有应用写入的数据，随后执行迁移。
这是一台服务器的短暂停机发布；并非零停机切换。
基础设施更新与应用发布分开：已有实例的 MySQL、Redis、RabbitMQ 使用当前发布的配置启动，
发布流程不负责升级它们。修改基础设施版本或配置前，需要单独安排备份、验证及维护窗口。

首次上线没有旧数据库可备份。更新时备份失败会尝试恢复旧应用，并阻止执行迁移；
迁移失败则保持业务停止，避免 MySQL DDL 部分提交后继续使用不兼容代码。
若无 schema 变更而新应用检查失败，会恢复旧镜像；schema 变更时保留现场，人工处理。
脚本同时保留失败退出状态，工作流不会把自动恢复当作发布成功。
内部健康检查成功但公网检查失败时，工作流会失败；应排查宿主机 Nginx、DNS、TLS、安全组。

## 5. 检查、备份与回滚

在服务器定义只用于当前终端的便捷函数：

```bash
cd /srv/bookloop
bookloop_compose() {
  docker compose -p bookloop --project-directory /srv/bookloop/current \
    --env-file /srv/bookloop/.env --env-file /srv/bookloop/current/release.env \
    -f /srv/bookloop/current/compose.yaml -f /srv/bookloop/current/compose.production.yaml "$@"
}
bookloop_compose ps -a
bookloop_compose logs --tail=100 backend worker nginx
curl --fail http://127.0.0.1:8088/readyz
```

手动执行联合备份，过程会短暂停止业务写入再恢复：

```bash
bash /srv/bookloop/current/deploy/release.sh backup /srv/bookloop
```

可以为部署账号设置每日低峰备份任务，例如 crontab：

```cron
0 3 * * * /bin/bash /srv/bookloop/current/deploy/release.sh backup /srv/bookloop 2>&1 | /usr/bin/logger -t bookloop-backup
```

备份包含 `mysql.sql`、`uploads.tar.gz`、`schema.txt`、`release.env` 和 `SHA256SUMS`。
用 `sha256sum -c SHA256SUMS` 检查完整性，并复制到服务器之外的受控位置。
每天至少保留一份，按磁盘容量制定保留策略；脚本不自动删除备份。
这些只覆盖核心数据库与上传图片，RabbitMQ 卷仍需保留；其待投递消息不包含在该联合备份内。
真实恢复演练须在隔离环境导入 SQL、还原图片、检查订单/图片/通知，再计划生产恢复。

回滚应用到上一个成功发布：

```bash
bash /srv/bookloop/current/deploy/release.sh rollback /srv/bookloop
```

脚本比对数据库 migration 版本与 checksum，仅允许 schema 一致时直接回滚。
若发生数据库结构变更，会拒绝盲目回滚。应审查 SQL 和旧代码兼容性，优先修复后向前发布；
必要时在停写状态下按恢复计划恢复同批次 SQL/图片。不要自动执行 `.down.sql`，也不要使用
`docker compose down -v` 或删除生产卷。保留 current、previous 对应目录和镜像，清理历史版本前核对引用。

## 6. 本地验证命令

```bash
cd backend
go fmt ./...
go vet ./...
go test ./...
# 仅连接可销毁的专用数据库；集成测试会重建测试表。
BOOKLOOP_TEST_MYSQL_DSN='root:TEST_PASSWORD@tcp(127.0.0.1:3306)/bookloop_test?parseTime=true&multiStatements=true' \
BOOKLOOP_TEST_REDIS_ADDR=127.0.0.1:6379 CGO_ENABLED=1 go test -race -count=1 ./...
cd ../frontend
npm ci
npm test
npm run build
cd ..
bash deploy/test-release.sh
docker compose --env-file .env.example -f compose.yaml -f compose.production.yaml config --quiet
```

Go race 检查需要 C 编译器。完整 Compose 验证须使用独立 project name 和空闲 HTTP 端口，
避免接触正在运行的开发/生产数据。

参考：[GitHub Environment 规则](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)、
[Compose up 与健康等待](https://docs.docker.com/reference/cli/docker/compose/up/)。
