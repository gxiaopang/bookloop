<div align="center">

# 📚 BookLoop

### 让读过的书，继续被需要。

一个简洁、完整的校园二手书发布与交易平台

![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square&logo=go&logoColor=white)
![Vue](https://img.shields.io/badge/Vue-3-42B883?style=flat-square&logo=vuedotjs&logoColor=white)
![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?style=flat-square&logo=typescript&logoColor=white)
![MySQL](https://img.shields.io/badge/MySQL-8-4479A1?style=flat-square&logo=mysql&logoColor=white)
![Redis](https://img.shields.io/badge/Redis-7-DC382D?style=flat-square&logo=redis&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?style=flat-square&logo=docker&logoColor=white)

</div>

---

BookLoop 面向校园二手书交易场景。卖家上传实物图片和微信联系二维码后才能上架图书，买家可以浏览、收藏、扫码联系卖家，并预约线下交易。系统通过 MySQL 事务、行级锁和幂等键，保证同一本书不会被重复售出。

项目采用 Go + Vue 3 的模块化单体架构，适合作为完整的全栈项目运行，也适合学习 REST API、JWT 鉴权、事务并发控制、Redis 幂等、Transactional Outbox、RabbitMQ 消费幂等和 Docker Compose 部署。

> [!NOTE]
> 平台不提供支付功能。买卖双方通过微信协商线下交易，卖家确认交付后，由买家确认收书完成交易。

## ✨ 核心功能

| 模块 | 功能 |
|---|---|
| 用户 | 邮箱验证码注册、登录、JWT 身份认证、个人信息查询 |
| 书市 | 图书浏览、关键词搜索、成色筛选、价格排序、收藏最多排序、详情查看 |
| 发布 | 图书创建与编辑、实物图片上传与排序、微信联系二维码上传、上架与下架 |
| 收藏 | 添加收藏、取消收藏、收藏列表 |
| 订单 | 预约订单、买卖双方取消预约、卖家确认交付、买家确认收书 |
| 通知 | 订单状态站内通知、未读筛选、单条/全部已读 |
| 管理 | 订单查询、违规图书封禁与恢复 |
| 可靠性 | 并发购买保护、订单幂等、健康检查、请求追踪 |
| 异步消息 | MySQL Outbox、RabbitMQ publisher confirm、重试与死信队列 |
| 部署 | Docker Compose 一键启动、自动执行数据库迁移 |

## 🧰 技术栈

| 层级 | 技术 |
|---|---|
| 后端 | Go、Gin、GORM、JWT、bcrypt |
| 前端 | Vue 3、TypeScript、Vite、Element Plus、Pinia、Axios |
| 数据与消息 | MySQL 8、Redis 7、RabbitMQ 4 |
| 部署 | Docker、Docker Compose、Nginx |
| 测试 | Go testing、Vitest |

## 🏗️ 系统架构

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

MySQL Outbox → Go Worker → RabbitMQ → Notification Consumer → MySQL
```

后端遵循 `Router → Handler → Service → Repository → Database` 分层。MySQL 保存核心业务事实，Redis 只承担幂等等辅助能力；订单事务与 Outbox 原子提交，RabbitMQ 仅异步传递已提交事件。即使 Redis 或 RabbitMQ 暂时不可用，订单正确性仍由数据库事务与唯一约束保证。

## 🚀 快速启动

开始前请安装 Git、Docker Engine 和 Docker Compose 插件。

### 1. 获取代码

```bash
git clone https://github.com/gxiaopang/bookloop.git
cd bookloop
```

### 2. 创建本地配置

```bash
cp .env.example .env
```

`.env.example` 中的凭据仅供本地开发使用。部署到公网前，请为 MySQL、Redis 和 JWT 分别生成不同的随机密钥。

注册需要 SMTP 邮件配置。按 [邮箱验证码指南](EMAIL_VERIFICATION.md) 填写 `SMTP_HOST`、
`SMTP_PORT`、`SMTP_USERNAME`、`SMTP_PASSWORD`、`SMTP_FROM`；未配置时可以启动和浏览书市，
但获取验证码和新账号注册不会绕过邮箱验证。

### 3. 启动服务

```bash
docker compose up --build -d --wait mysql redis rabbitmq backend nginx
docker compose up -d --no-deps worker
docker compose ps
```

启动完成后访问：**<http://127.0.0.1:8088>**

### 4. 检查状态

```bash
curl --fail http://127.0.0.1:8088/healthz
curl --fail http://127.0.0.1:8088/readyz
docker compose logs migrate backend worker rabbitmq nginx
```

### 5. 停止服务

```bash
docker compose down
```

该命令会保留 MySQL 数据和上传图片。不要随意添加 `-v`，否则会同时删除数据卷。

## 📁 项目结构

```text
bookloop/
├── backend/
│   ├── cmd/                 # API、消息 worker 与数据库迁移入口
│   ├── internal/            # Handler、Service、Repository 等后端模块
│   └── migrations/          # SQL 数据库迁移
├── frontend/
│   └── src/                 # Vue 页面、组件、状态与 API 客户端
├── compose.yaml             # 完整服务编排
└── .env.example             # 环境变量模板
```

## 🔐 安全提示

- 不要提交 `.env`、私钥、数据库密码、JWT 密钥、证书或备份文件。
- 正式部署时不要将 MySQL、Redis 或 Go backend 端口暴露到公网。
- 公网环境应使用独立随机密钥，并在面向真实用户前配置 HTTPS。

## CI/CD 与上线

GitHub Actions 自动执行前后端检查、MySQL/Redis 集成测试与 Compose 冒烟检查，
main 通过后发布带唯一版本标签的镜像。生产部署在 Actions 中手动触发，
通过 SSH 拉取镜像摘要锁定的版本，完成数据库与图片备份、迁移和健康检查。
服务器准备、GitHub Secrets、HTTPS 和回滚步骤见 [部署指南](deploy/README.md)。

---

<div align="center">
  <sub>Built for books that deserve another chapter.</sub>
</div>

商品必须先上传实物图片，再上传微信联系二维码，才能上架。二维码单独保存，不计入 1～6 张商品图片；详情页提供“联系卖家”入口。已有 `000007` 环境追加迁移 `000008` 后启用双方确认，无需清空订单。具体接口、兼容规则和验证步骤见 [微信联系交易流程](CONTACT_TRADING.md)。
