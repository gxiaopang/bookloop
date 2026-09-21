# BookLoop

BookLoop 是一个面向校园场景的二手书交易平台。卖家可以发布带实物图片的图书，买家可以浏览、收藏、下单并完成模拟支付；系统通过 MySQL 事务、行级锁和幂等键保证同一本书不会被重复售出。

项目采用 Go + Vue 3 的模块化单体架构，既可以作为完整的全栈项目运行，也适合学习 REST API、JWT 鉴权、事务并发控制、Redis 幂等和 Docker Compose 部署。

> 当前支付流程为模拟支付，不接入真实支付渠道。

## 功能

- 用户注册、登录、JWT 身份认证和个人信息查询
- 图书发布、编辑、实物图片上传与排序、上架和下架
- 图书列表、关键词搜索、成色筛选、价格排序和详情查看
- 收藏和取消收藏
- 创建订单、模拟支付、取消订单和确认完成
- MySQL 事务与 `SELECT FOR UPDATE` 防止并发重复购买
- Redis `Idempotency-Key` 协调订单创建幂等，MySQL 唯一约束最终兜底
- 管理员查看订单、封禁和恢复图书
- 健康检查、结构化日志、请求 ID 和优雅关闭
- Docker Compose 一键启动前端、后端、MySQL、Redis 和数据迁移

## 技术栈

| 层级 | 技术 |
|---|---|
| 后端 | Go、Gin、GORM、JWT、bcrypt |
| 前端 | Vue 3、TypeScript、Vite、Element Plus、Pinia、Axios |
| 数据 | MySQL 8、Redis 7 |
| 部署 | Docker、Docker Compose、Nginx |
| 测试 | Go testing、Vitest |

## 架构

```text
Browser
   │
   ▼
Nginx ───────────────► Vue SPA / uploaded images
   │ /api/v1
   ▼
Gin Router → Middleware → Handler → Service → Repository
                                              ├── MySQL（事实源）
                                              └── Redis（幂等辅助）
```

后端遵循 `Router → Handler → Service → Repository → Database` 分层。MySQL 保存核心业务事实，Redis 仅用于幂等等辅助能力；即使 Redis 不可用，订单正确性仍由数据库事务和约束保证。

## 快速启动

### 环境要求

- Git
- Docker Engine 及 Docker Compose 插件

### 1. 获取代码

```bash
git clone git@github.com:gxiaopang/bookloop.git
cd bookloop
```

没有配置 GitHub SSH Key 时，也可以使用 HTTPS：

```bash
git clone https://github.com/gxiaopang/bookloop.git
cd bookloop
```

### 2. 创建本地配置

```bash
cp .env.example .env
```

`.env.example` 中的凭据只用于本地开发。部署到公网前，必须为 MySQL、Redis 和 JWT 分别生成不同的随机密钥，并确保 `.env` 不会提交到 Git。

### 3. 启动

```bash
docker compose up --build -d --wait
docker compose ps
```

浏览器访问：<http://127.0.0.1:8088>

检查服务状态：

```bash
curl --fail http://127.0.0.1:8088/healthz
curl --fail http://127.0.0.1:8088/readyz
docker compose logs migrate backend nginx
```

### 4. 停止

```bash
docker compose down
```

该命令会保留 MySQL 数据和上传图片。不要随意添加 `-v`，否则会删除对应的数据卷。

## 完整功能验收

建议注册两个普通用户，按以下顺序验证核心闭环：

```text
卖家注册并登录
  → 发布图书并上传图片
  → 上架图书
  → 买家注册并登录
  → 收藏图书
  → 创建订单
  → 模拟支付
  → 确认完成
```

验收时还应确认：

- 登录成功后会离开登录页，并显示“我的发布”等登录后入口。
- 买家不能购买自己发布的图书。
- 同一本书被并发购买时只有一个订单能够成功。
- 容器重启后，用户、订单和图片仍然存在。

## 常用开发检查

后端：

```bash
cd backend
go fmt ./...
go vet ./...
go test ./...
```

前端：

```bash
cd frontend
npm ci
npm run typecheck
npm test
npm run build
```

Compose 配置：

```bash
docker compose --env-file .env.example config --quiet
```

## 项目结构

```text
bookloop/
├── backend/
│   ├── cmd/                 # API 服务与数据库迁移入口
│   ├── internal/            # Handler、Service、Repository 等后端模块
│   └── migrations/          # 可审查、可复现的 SQL 迁移
├── frontend/
│   └── src/                 # Vue 页面、组件、状态和 API 客户端
├── docs/
│   ├── PLAN.md              # 系统设计、业务规则和开发阶段
│   ├── DOCKER.md            # Compose 运行说明
│   └── DEPLOYMENT.md        # 公网 IP 测试与域名 HTTPS 上线教程
├── compose.yaml
└── .env.example
```

## 文档

- [系统设计与开发计划](docs/PLAN.md)
- [Docker Compose 使用说明](docs/DOCKER.md)
- [公网 IP 测试与正式上线教程](docs/DEPLOYMENT.md)
- [数据库迁移说明](backend/migrations/README.md)
- [前端开发说明](frontend/README.md)

## 安全说明

- 不要提交 `.env`、私钥、数据库密码、JWT 密钥、证书或备份文件。
- 正式部署时不要将 MySQL、Redis 或 Go backend 端口暴露到公网。
- 公网 IP 的纯 HTTP 部署只适合短期个人验收；面向真实用户时应配置域名和 HTTPS。
- 上传图片、用户权限和订单状态始终以后端校验为准，不能信任客户端传入的所有权信息。

## 当前范围

当前版本覆盖校园二手书交易 MVP，不包含真实支付、退款、物流、聊天、评价、第三方登录和复杂推荐系统。后续阶段可在现有模块化单体结构上继续完善 CI/CD、管理员用户治理和生产监控。
