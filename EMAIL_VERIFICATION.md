# 注册邮箱验证码

注册流程现在为：填写邮箱 → 获取 6 位验证码 → 填写用户名、密码和验证码 → 创建账号。
未验证邮箱不能创建新账号；登录流程仍使用用户名/邮箱和密码。

## API

`POST /api/v1/auth/email-codes`

```json
{"email":"reader@example.com"}
```

成功返回 `data.expires_in=300` 和 `data.resend_after=60`。响应、访问日志和 Redis 都不保存明文验证码。
邮件中包含验证码；不能通过接口响应获取它。

`POST /api/v1/auth/register`

```json
{
  "username":"reader",
  "email":"reader@example.com",
  "password":"your-own-password",
  "email_code":"123456"
}
```

`123456` 只是格式示例，接口没有固定验证码或开发绕过值。
验证码缺失或格式错误返回 `400 VALIDATION_ERROR`；错误、过期、超过尝试次数或已消费返回
`400 EMAIL_CODE_INVALID`。发送限流返回 `429 EMAIL_CODE_RATE_LIMITED`，同时返回
`Retry-After` 响应头和 `data.retry_after` 秒数。依赖故障返回 `503 SERVICE_UNAVAILABLE`。
已注册邮箱仍按已有业务约定返回 `409 EMAIL_ALREADY_EXISTS`。

## 执行流程与边界

Handler 解析输入和真实客户端 IP → Auth Service 规范化邮箱并检查账号唯一性
→ Registration Service 调用 Redis Repository 原子预占发送额度
→ `crypto/rand` 生成六位数字 → SMTP 通过 TLS 发送
→ Redis 保存带密钥的验证码摘要及过期时间。

注册时，Service 检查用户名、邮箱、密码和验证码格式，检查已有账号；
Redis Lua 原子核对并消费验证码，之后使用 bcrypt 哈希密码并在 MySQL 创建用户。
MySQL 唯一约束仍负责防止并发创建相同邮箱或用户名。

- 验证码有效期 5 分钟；允许前导零，必须输入完整 6 位。
- 同一邮箱发送间隔至少 60 秒，每小时最多 10 次；同一 IP 每小时最多 30 次。
- 每个验证码最多允许 5 次错误尝试，成功后立即删除。并发验证同一个码只能成功一次。
- 邮箱统一去空格并转小写；验证码与规范化邮箱绑定，不能跨邮箱使用。
- Redis key 使用邮箱/IP 的摘要，验证码使用 HMAC-SHA256 摘要。HMAC 复用 JWT 密钥，
  带独立用途前缀，与 JWT 签名隔离；六位验证码不能只存无密钥 SHA256，以免离线穷举。
- 邮件密码和验证码不写日志；SMTP 使用隐式 TLS 并校验证书，默认端口 465，
  不降级到明文连接。发送受请求取消和超时控制。
- 发送前先限流；SMTP 失败仍消耗发送额度，但不会覆盖此前有效的验证码。
- SMTP 成功而 Redis 写入失败时返回 503，不会宣称验证码已经可用。
- Redis 不可用时验证码注册拒绝继续；订单原有 Redis 降级规则保持独立。
- 验证码消费后若 MySQL 创建失败，需要重新获取验证码。两种存储之间不做分布式事务，
  不复活已消费的验证码。MySQL 始终保存最终的账号事实。

验证码是短期安全状态，复用 Redis 可以直接提供 TTL 和原子消费，不需要新建数据库表或基础设施。
发送流程先采用同步 SMTP，接口成功意味着邮件服务商已接收；真实投递、垃圾邮件判断由邮件服务商完成。
现有 RabbitMQ worker 继续处理订单通知，不参与注册邮件。
没有新增用户数据库字段；本次变更只约束新注册，不追溯改变旧账号登录行为。

## 阿里云配置

本项目使用阿里云邮件推送 DirectMail，当前发信地址为 `byteneat@notify.byteneat.top`。
在邮件推送控制台确认发信域名 `notify.byteneat.top` 已验证，发信类型选择触发邮件，
并为这个发信地址配置 SMTP 密码。SMTP 密码与阿里云账号密码不同。

杭州区配置示例（其他区域使用相应区域的官方 endpoint）：

```dotenv
SMTP_HOST=smtpdm.aliyun.com
SMTP_PORT=465
SMTP_USERNAME=byteneat@notify.byteneat.top
SMTP_FROM=byteneat@notify.byteneat.top
SMTP_PASSWORD='fill-in-your-smtp-password-locally'
```

实际密码只填写在被 Git 忽略的本地 env 文件或服务器受限 `.env` 中。
dotenv 密码含 `$` 或 `#` 等字符时使用单引号，避免变量插值或注释截断。
五项配置须完整；全部留空时服务可以启动，但获取注册验证码会返回 503，注册不会绕过验证。
Compose 仅向 backend 注入 SMTP 凭据，并让 backend 加入可出网网络，以访问外部 SMTP；
backend、MySQL、Redis、RabbitMQ 均不发布公网端口。

本机可在原有 Compose env 文件之后叠加邮件配置，例如隔离预览环境：

```bash
BOOKLOOP_BACKEND_IMAGE=bookloop-backend:email-verification \
BOOKLOOP_NGINX_IMAGE=bookloop-nginx:email-verification \
BOOKLOOP_HTTP_PORT=38088 \
docker compose --env-file .env.example --env-file .env.smtp.local -p bookloop-email-preview \
  up -d --no-build --wait mysql redis rabbitmq backend nginx

BOOKLOOP_BACKEND_IMAGE=bookloop-backend:email-verification \
BOOKLOOP_NGINX_IMAGE=bookloop-nginx:email-verification \
BOOKLOOP_HTTP_PORT=38088 \
docker compose --env-file .env.example --env-file .env.smtp.local -p bookloop-email-preview \
  up -d --no-build --no-deps worker
```

预览入口为 `http://127.0.0.1:38088`。镜像须先用相同 env/项目名执行 `docker compose build`。
`.env.example` 的数据库凭据只用于本机隔离测试，不用于生产。
SMTP 密码只在容器运行时注入，不作为构建参数或复制进镜像。

## 验证

```bash
cd backend
go fmt ./...
go vet ./...
go test ./...
# 指向独立的临时 Redis，不要使用生产 Redis：
BOOKLOOP_TEST_REDIS_ADDR=127.0.0.1:26380 go test ./internal/repository -run TestRegistrationCodesWithRedis -count=1 -v
# 需要 C 编译器：
CGO_ENABLED=1 go test -race ./...
cd ../frontend
npm test
npm run build
```

SMTP 单元测试使用本地 TLS 服务验证认证、MIME 内容和取消行为。Redis 集成测试覆盖真实过期、
发送配额、错误次数耗尽和并发消费。Service 与 API 测试覆盖未验证拒绝、邮件失败、Redis 故障、
规范化邮箱以及完整注册/登录路径。

`TestRegistrationWithLiveSMTP` 默认跳过，仅在显式设置 `BOOKLOOP_TEST_SMTP_TO`、
SMTP 配置和专用 Redis 地址后发送一封真实邮件。它使用内存用户仓储，验证真实邮件发送与
Redis 消费后可创建用户；不修改现有开发或生产用户。普通 CI 不设置收件地址，不发送外部邮件。

参考：[发信地址与 SMTP 密码](https://help.aliyun.com/zh/direct-mail/user-guide/setup-sender-addresses)、
[各区域 SMTP endpoint](https://help.aliyun.com/zh/direct-mail/smtp-endpoints)、
[发信域名 DNS 配置](https://help.aliyun.com/zh/direct-mail/user-guide/how-to-configure-sending-domain-names)。
