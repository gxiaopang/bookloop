# BookLoop 系统设计与开发计划

> 文档状态：Phase 0 设计基线
>
> 适用范围：BookLoop MVP（模块化单体）
>
> API 前缀：`/api/v1`

## 1. 项目范围与 MVP 定义

BookLoop 是面向校园场景的二手书发布、收藏和交易平台。MVP 的目标不是覆盖完整电商链路，而是验证以下闭环：用户注册登录，卖家发布一本实体书，买家浏览并收藏商品，买家创建订单并完成模拟支付，双方查看订单，平台保证同一本书不会被成功出售两次。

### 1.1 MVP 范围

- 用户：注册、登录、查看个人信息；管理员可禁用违规用户。
- 图书商品：公开列表与详情；按标题关键词和成色筛选；按最新、价格升序或价格降序排序；卖家发布、编辑、上架和下架自己的商品。
- 商品图片：每件商品维护 1～6 张实物图片，支持上传、删除和排序；图片保存在本地持久化目录并由 Nginx 提供静态访问。
- 收藏：登录用户添加、取消、查看收藏。
- 订单：创建订单、模拟支付、取消待支付订单、确认完成；买家和卖家分别查看相关订单。
- 管理：基于用户角色的最小权限控制；管理员可查询订单、禁用用户、封禁或恢复违规商品。
- 安全：bcrypt 密码哈希、JWT 鉴权、输入校验、所有权和角色校验。
- 一致性：MySQL 事务和行级锁保证并发购买正确；MySQL 是核心数据的唯一事实源。
- 幂等：订单创建使用 `Idempotency-Key`，Redis 提供快速并发协调与结果重放，MySQL 唯一约束提供最终兜底。
- 部署：Docker Compose 编排 Nginx、Go 后端、MySQL、Redis，并用持久卷保存上传图片；Nginx 提供静态资源、图片访问、反向代理和 TLS 入口。

### 1.2 MVP 不包含

- 真实支付、退款、资金结算和发票。
- 物流、配送追踪、站内聊天、评价、举报工单。
- 外部对象存储服务和图片处理流水线；MVP 由 Go 后端接收图片并保存到本地 `uploads` 目录。
- 个性化推荐、全文检索、复杂统计报表。
- 商品分类及相关字典系统；第一版只做标题搜索、成色筛选、排序和分页。
- 第三方登录、短信/邮件验证码、复杂风控。
- 微服务、消息队列、分布式事务、分布式锁、Kubernetes、Kafka、Elasticsearch。

### 1.3 核心业务不变量

1. 一本书在任意时刻最多只能存在一个状态为 `pending_payment`、`paid` 或 `completed` 的有效交易链。
2. `cancelled` 或 `expired` 订单释放该书的有效交易占位，之后才允许创建新的交易链。
3. 买家不能购买自己发布的书；买家身份来自 JWT，不接受客户端传入。
4. 卖家只能编辑、上架或下架自己的书；管理员的治理操作必须走独立权限检查。
5. 图书和订单状态只能按本文定义的状态机转换，客户端不能直接指定任意状态。
6. 相同用户使用相同 `Idempotency-Key` 和相同请求体重试创建订单，必须得到同一业务结果；相同 Key 配不同请求体必须拒绝。
7. `available` 商品至少有 1 张、最多有 6 张图片；列表封面始终由 `sort_order` 最小的图片动态确定。
8. 只有商品所有者能上传、删除和排序图片；`blocked`、`sold` 商品的图片不可修改。

## 2. 系统总体架构

采用模块化单体。后端在一个 Go 进程中按 User、Book、Favorite、Order、Admin 领域组织代码，共享一个 MySQL 数据库和一个 Redis 实例。领域之间通过 Service 协作，不拆分网络服务。

```text
Browser
  │ HTTPS
  ▼
Nginx
  ├── /                 → Vue 静态文件（SPA fallback）
  ├── /uploads/*        → uploads 持久卷（只读静态文件）
  └── /api/v1/*         → Go/Gin API
                              │
                         Router/Middleware
                              │
                           Handler
                              │
                           Service
                              │
                         Repository
                          ├── MySQL 8（事实源）
                          └── Redis（幂等辅助）

Go Backend ──写入──> uploads 持久卷 <──只读── Nginx
```

设计原则：

- 同一代码库、同一部署单元、清晰模块边界，优先可读性和可测试性。
- 同步 REST 请求完成主要业务，不引入消息队列或事件总线。
- MySQL 决定订单和图书的真实状态；Redis 丢失时系统仍保持数据正确，只会降低幂等重放效率。
- 图片元数据由 MySQL 管理，图片文件由本地存储适配器写入 `uploads`。存储接口只抽象保存、删除和生成公开路径，未来可替换远端存储而不改变 Handler/Service 契约。
- DTO 与持久化 Model 分离，避免数据库字段直接成为稳定的外部 API 契约。

## 3. 后端目录结构

计划在 Phase 1 起逐步建立以下结构；Phase 0 不创建这些目录或代码：

```text
backend/
├── cmd/
│   └── server/
│       └── main.go              # 组装依赖、启动与优雅关闭
├── internal/
│   ├── config/                  # 环境变量读取和启动校验
│   ├── handler/                 # HTTP 入参、响应和状态码
│   ├── service/                 # 业务规则、权限、事务编排
│   ├── repository/              # GORM 查询、持久化、事务辅助
│   ├── model/                   # 数据库持久化模型
│   ├── dto/                     # 请求与响应结构
│   ├── middleware/              # JWT、角色、日志、恢复、Request ID
│   ├── router/                  # 路由注册与中间件组合
│   └── storage/                 # 本地图片存储适配器及可替换的最小接口
├── pkg/                         # 仅放可独立复用的小组件
├── migrations/                  # 可审查、可复现的 SQL 迁移
├── tests/                       # 跨模块和集成测试
├── Dockerfile
├── go.mod
└── go.sum
```

初期不建立泛用 `common`、`base`、`domain event` 等抽象目录。只有出现明确复用需求时才提取到 `pkg`。

## 4. 前端目录结构

计划在 Phase 7 建立：

```text
frontend/
├── src/
│   ├── api/                     # Axios 实例及按领域拆分的 API
│   ├── assets/                  # 静态样式与图片
│   ├── components/              # 可复用展示组件
│   ├── layouts/                 # 公共页面布局
│   ├── router/                  # Vue Router 与路由守卫
│   ├── stores/                  # Pinia：auth 等跨页面状态
│   ├── types/                   # API DTO 的 TypeScript 类型
│   ├── utils/                   # 小型纯函数工具
│   ├── views/                   # 登录、图书、收藏、订单、管理页面
│   ├── App.vue
│   └── main.ts
├── public/
├── index.html
├── package.json
├── tsconfig.json
└── vite.config.ts
```

前端不复制后端业务规则作为安全边界。按钮禁用和路由守卫只改善体验，最终权限与状态转换仍由后端校验。

## 5. Handler → Service → Repository 分层设计

### 5.1 Router 与 Middleware

- 注册 `/api/v1` 路由组，组合公开、登录用户和管理员路由。
- Middleware 完成 Request ID、访问日志、panic 恢复和 JWT 验证。鉴权 Middleware 从已验证的 `sub` 取得 `user_id`，再查询当前 `users` 记录并确认 `status = 'active'`。
- Middleware 写入请求上下文的用户状态和角色必须来自本次数据库查询；管理员授权使用数据库当前 `role`，不能把 JWT 中可能过期的角色声明作为最终依据。

### 5.2 Handler

- 解析 path、query、header 和 JSON body。
- 做格式级校验，例如必填、长度、数值范围和分页上限。
- 从认证上下文读取当前用户，不信任请求体中的 `buyer_id` 或 `seller_id`。
- 调用 Service，将领域错误映射为统一 HTTP 响应。
- 不直接调用 GORM、不开事务、不包含所有权判断。

### 5.3 Service

- 执行业务校验、所有权/角色检查和状态转换。
- 定义事务边界，编排一个或多个 Repository 操作。
- 订单 Service 负责“锁图书 → 校验 → 建订单 → 改图书状态”的原子流程。
- 接受/返回 DTO 或领域所需数据，不处理 Gin 的 `Context` 和 HTTP 响应。
- 对可预期失败返回可识别的领域错误，不用 `panic` 表示普通业务错误。

### 5.4 Repository

- 封装 GORM 查询、行锁、创建、更新和受影响行数检查。
- 接收 `context.Context`，数据库操作使用 `WithContext`。
- 不决定 HTTP 状态码，不做买家/卖家业务决策。
- 普通仓储保持具体、小而明确。Phase 5 仅为跨仓储原子操作增加一个小型事务辅助；Service 决定事务内容，GORM 事务细节留在 Repository/基础设施边界，不扩展成通用框架。

### 5.5 Model 与 DTO

- Model 对应数据库列、索引和关联。
- Request DTO 只暴露允许客户端修改的字段，例如发布图书请求不包含 `seller_id` 和 `status`。
- Response DTO 排除 `password_hash`、内部幂等指纹等敏感/内部字段，并保持金额为固定两位小数的 JSON 字符串，避免浮点误差。图书详情返回按 `sort_order` 排序的全部图片；列表从关联图片中动态取第一张，不在 `books` 冗余图片字段。

## 6. MySQL 数据库设计

### 6.1 通用约定

- MySQL 8，InnoDB，字符集 `utf8mb4`，时区统一为 UTC；API 输出 RFC 3339 时间。
- 主键使用 `BIGINT UNSIGNED` 自增；对外订单使用不可枚举的 `order_no`。
- 金额使用 `DECIMAL(10,2)`，Go 代码不得用二进制浮点参与金额计算。
- 状态字段使用短字符串并配合 `CHECK` 约束；迁移脚本显式创建索引、外键和约束。
- 默认物理保留核心业务记录。MVP 不提供删除用户或已产生交易的图书，避免软删除与唯一索引的额外复杂度。
- 外键用于基本引用完整性，关键业务正确性仍由事务和 Service 保证。

### 6.2 `users`

| 字段 | 类型 | 约束/含义 |
|---|---|---|
| `id` | BIGINT UNSIGNED | PK，自增 |
| `username` | VARCHAR(32) | 非空，唯一；展示名/登录名 |
| `email` | VARCHAR(254) | 非空，规范化为小写后唯一 |
| `password_hash` | VARCHAR(255) | 非空，只保存 bcrypt 哈希 |
| `role` | VARCHAR(16) | 非空，`user` / `admin`，默认 `user` |
| `status` | VARCHAR(16) | 非空，`active` / `disabled`，默认 `active` |
| `created_at` | DATETIME(3) | 非空 |
| `updated_at` | DATETIME(3) | 非空 |

约束与索引：

- `UNIQUE(username)`、`UNIQUE(email)`。
- `CHECK(role IN ('user','admin'))`。
- `CHECK(status IN ('active','disabled'))`。

### 6.3 `books`

| 字段 | 类型 | 约束/含义 |
|---|---|---|
| `id` | BIGINT UNSIGNED | PK，自增 |
| `seller_id` | BIGINT UNSIGNED | 非空，FK → `users.id`，发布者 |
| `title` | VARCHAR(200) | 非空 |
| `description` | TEXT | 可空 |
| `price` | DECIMAL(10,2) | 非空，大于 0 |
| `condition` | VARCHAR(16) | 非空，`new` / `like_new` / `good` / `fair` |
| `status` | VARCHAR(20) | 非空，见图书状态机 |
| `created_at` | DATETIME(3) | 非空 |
| `updated_at` | DATETIME(3) | 非空 |

约束与索引：

- FK `seller_id` 使用 `RESTRICT`，有商品的用户不能物理删除。
- `CHECK(price > 0)`。
- `CHECK(condition IN ('new','like_new','good','fair'))`。
- `CHECK(status IN ('available','reserved','sold','off_shelf','blocked'))`。
- `INDEX(status, created_at, id)` 支持公开商品按最新发布稳定分页。
- `INDEX(status, price, id)` 支持公开商品按价格稳定排序。
- `INDEX(status, condition, created_at, id)` 支持成色筛选后的最新排序。
- `INDEX(status, condition, price, id)` 支持成色筛选后的价格排序。
- `INDEX(seller_id, status, created_at)` 支持“我的发布”。
- 标题关键词第一版使用受限长度的 `LIKE '%keyword%'`。数据规模较小时保持简单，不增加全文检索或额外检索系统。

`books` 最终只包含上表九个字段，不保存任何图片地址或额外书目属性。新建商品默认 `off_shelf`；上传至少一张图片后，所有者才能执行上架。

### 6.4 `book_images`

| 字段 | 类型 | 约束/含义 |
|---|---|---|
| `id` | BIGINT UNSIGNED | PK，自增 |
| `book_id` | BIGINT UNSIGNED | 非空，FK → `books.id` |
| `image_url` | VARCHAR(2048) | 非空，应用生成的相对公开路径 |
| `sort_order` | SMALLINT UNSIGNED | 非空，正整数；数值最小者为列表封面 |
| `created_at` | DATETIME(3) | 非空 |

约束与索引：

- `UNIQUE(book_id, sort_order)` 保证一本书内图片顺序唯一，并同时支持按书有序查询。
- `CHECK(sort_order > 0)`。
- FK `book_id` 使用 `CASCADE`；若允许清理尚未交易的下架商品，其图片元数据一并删除。
- 每本书最多 6 张图片属于跨行规则：图片 Service 在锁定对应 `books` 行的事务中计数并校验，避免两个并发上传都越过上限。
- `available` 商品必须至少有 1 张图片。上架时在同一事务内锁定商品并检查图片数；删除 `available` 商品的最后一张图片必须拒绝，卖家需先下架。
- 列表查询为每本书选取 `sort_order` 最小的关联记录作为封面；详情查询返回按 `sort_order ASC, id ASC` 排列的全部图片。
- 排序接口接收该商品完整的图片 ID 顺序，Service 校验所有 ID 都属于当前商品且不重复，再在事务中先写入不冲突的临时正数区间、后写入最终 1～N，避免逐行更新触发唯一键冲突。

图片文件与元数据的一致性规则：

- 后端只接受规定的图片 MIME/扩展名和大小，解码验证实际内容，使用服务端生成的随机文件名，禁止使用客户端路径。
- 上传先写同一文件系统中的临时文件并完成内容校验；随后锁定 `books` 行，校验所有者、状态和图片数量，原子重命名到最终路径并创建 `book_images` 记录。数据库回滚时补偿删除新文件，进程在重命名后崩溃产生的孤儿文件由清理任务处理。
- 删除先完成权限与状态校验并提交元数据变更，再删除文件。文件删除失败时记录告警并由安全的孤儿文件清理任务重试，不能回滚已经成功的数据库事务。
- `image_url` 保存相对于站点的公开路径；业务代码通过最小存储接口操作文件，不拼接宿主机绝对路径。

### 6.5 `favorites`

| 字段 | 类型 | 约束/含义 |
|---|---|---|
| `user_id` | BIGINT UNSIGNED | PK 一部分，FK → `users.id` |
| `book_id` | BIGINT UNSIGNED | PK 一部分，FK → `books.id` |
| `created_at` | DATETIME(3) | 非空 |

约束与索引：

- `PRIMARY KEY(user_id, book_id)` 同时保证一个用户不会重复收藏同一本书。
- `INDEX(book_id)` 支持按图书统计/清理收藏。
- 用户或未产生交易的图书物理删除时可 `CASCADE` 收藏；MVP 正常业务仍不物理删除核心记录。

### 6.6 `orders`

| 字段 | 类型 | 约束/含义 |
|---|---|---|
| `id` | BIGINT UNSIGNED | PK，自增 |
| `order_no` | CHAR(26) | 非空、唯一，对外订单号（例如 ULID） |
| `book_id` | BIGINT UNSIGNED | 非空，FK → `books.id` |
| `buyer_id` | BIGINT UNSIGNED | 非空，FK → `users.id`；从 JWT 获取 |
| `seller_id` | BIGINT UNSIGNED | 非空，FK → `users.id`；从锁定的图书获取 |
| `book_title` | VARCHAR(200) | 非空，创建时快照 |
| `book_price` | DECIMAL(10,2) | 非空，创建时快照/成交价 |
| `status` | VARCHAR(24) | 非空，见订单状态机 |
| `idempotency_key` | VARCHAR(64) | 非空，客户端生成的幂等键 |
| `request_fingerprint` | CHAR(64) | 非空，规范化请求的 SHA-256 |
| `expires_at` | DATETIME(3) | 非空，待支付过期时间 |
| `paid_at` | DATETIME(3) | 可空 |
| `completed_at` | DATETIME(3) | 可空 |
| `cancelled_at` | DATETIME(3) | 可空；取消或过期时间 |
| `cancel_reason` | VARCHAR(255) | 可空 |
| `active_book_id` | BIGINT UNSIGNED | 生成列，有效交易链中等于 `book_id`，否则为 `NULL` |
| `created_at` | DATETIME(3) | 非空 |
| `updated_at` | DATETIME(3) | 非空 |

`active_book_id` 设计为：

```sql
GENERATED ALWAYS AS (
  CASE
    WHEN status IN ('pending_payment', 'paid', 'completed') THEN book_id
    WHEN status IN ('cancelled', 'expired') THEN NULL
  END
) STORED
```

约束与索引：

- `UNIQUE(order_no)`。
- `UNIQUE(buyer_id, idempotency_key)`：Redis 不可用或结果丢失时，数据库仍能识别重复创建。
- `UNIQUE(active_book_id)`：MySQL 允许多个 `NULL`，但同一本书最多只能存在一个 `pending_payment`、`paid` 或 `completed` 订单，作为“一本书最多存在一个有效交易链”的数据库最终约束；取消或过期后生成列变为 `NULL`，才允许后续交易链。
- `CHECK(buyer_id <> seller_id)`：Service 校验之外的数据库兜底。
- `CHECK(book_price > 0)`。
- `CHECK(status IN ('pending_payment','paid','completed','cancelled','expired'))`。
- `INDEX(buyer_id, created_at, id)`、`INDEX(seller_id, created_at, id)` 支持订单列表。
- `INDEX(book_id, status)` 支持按书校验相关订单。
- `INDEX(status, expires_at)` 支持扫描过期待支付订单。
- 用户与图书外键均使用 `RESTRICT`，交易记录不可因删除主体而消失。

## 7. 表关系

```text
users 1 ─────< books       （一个卖家发布多本书）
books 1 ─────< book_images （一本商品书有多张实物图片）
users 1 ─────< favorites >───── 1 books
users 1 ─────< orders       （buyer_id，买家订单）
users 1 ─────< orders       （seller_id，卖家订单）
books 1 ─────< orders       （取消/过期后可再次创建订单）
```

核心业务表固定为 `users`、`books`、`book_images`、`favorites`、`orders`，不增加辅助字典业务表。Admin 不建立独立表，使用 `users.role = 'admin'`。订单保留书名和价格快照，因此后续图书信息变化不会篡改历史成交信息。

## 8. 图书状态设计

| 当前状态 | 允许转为 | 触发者/条件 |
|---|---|---|
| 新建 | `off_shelf` | 卖家创建商品资料；尚不公开售卖 |
| `available` | `off_shelf` | 所有者主动下架 |
| `off_shelf` | `available` | 所有者重新上架；用户有效且图片数为 1～6 |
| `available` / `off_shelf` | `blocked` | 管理员治理 |
| `blocked` | `off_shelf` | 仅管理员恢复；恢复后由卖家决定是否重新上架 |
| `available` | `reserved` | 创建订单事务锁定图书并成功建单 |
| `reserved` | `available` | 对应待支付订单取消或过期 |
| `reserved` | `sold` | 对应订单模拟支付成功 |
| `sold` | 无 | MVP 终态，不支持退款后重售 |

约束：

- 卖家只能触发 `available ↔ off_shelf`，不能直接设置 `reserved`、`sold` 或 `blocked`。
- 管理员只能把 `available`/`off_shelf` 变为 `blocked`，或把 `blocked` 恢复为 `off_shelf`；不能治理性改写订单占用或成交状态。
- `reserved` 与 `sold` 只能由 Order Service 在订单事务中写入，不提供任何允许客户端直接指定商品状态的通用接口。
- 上下架、block/unblock 与图片修改都需锁定商品行或使用带当前状态条件的更新，并检查受影响行数，和创建订单竞争时不会越过状态机。
- `reserved`、`sold` 状态下禁止卖家编辑价格和核心描述，避免订单快照与交易预期不一致。
- 图片上传、删除和排序只允许商品所有者操作。`blocked`、`sold` 必须拒绝图片修改；`reserved` 期间也暂时冻结图片，避免待支付期间改变买家看到的实物信息。
- 取消/过期释放图书、支付标记售出必须与订单状态变化处于同一 MySQL 事务。
- 管理员不能把 `reserved` 或 `sold` 图书强制改为 `available`；需要售后能力时另行设计，不在 MVP 内。

## 9. Order 状态机

```text
                    simulate pay
pending_payment ───────────────────> paid ── confirm receipt ──> completed
       │
       ├── buyer cancel ───────────> cancelled
       └── timeout ────────────────> expired
```

| 转换 | 权限与前置条件 | 同事务中的图书变化 |
|---|---|---|
| 创建 → `pending_payment` | 登录买家；书为 `available`；买家不是卖家 | `available` → `reserved` |
| `pending_payment` → `paid` | 仅买家；未过期；模拟支付成功 | `reserved` → `sold` |
| `pending_payment` → `cancelled` | 仅买家；尚未支付 | `reserved` → `available` |
| `pending_payment` → `expired` | 系统发现 `expires_at <= now` | `reserved` → `available` |
| `paid` → `completed` | 买家确认已完成线下交付 | 保持 `sold` |

其他转换全部返回 `409 ORDER_STATE_CONFLICT`。MVP 不支持已支付取消或退款；`cancelled`、`expired`、`completed` 均为终态。过期处理采用数据库时间判断：访问订单或购买同一本书时可做事务内惰性回收，另配单进程周期扫描提高及时性；扫描任务不是正确性的唯一依赖。

## 10. REST API 设计

### 10.1 通用约定

- 基础路径：`/api/v1`。
- JSON；时间为 UTC RFC 3339；金额响应为字符串，例如 `"35.00"`。
- 鉴权头：`Authorization: Bearer <token>`。
- 订单创建必须带 `Idempotency-Key: <unique-key>`，建议 UUID/ULID，最大 64 字符。
- 列表统一使用 `page`、`page_size`（默认 20，最大 100）；响应包含分页元数据。大数据量时可保持响应契约并改为游标实现。
- 成功响应：`{"code":"OK","message":"success","data":...,"request_id":"..."}`。
- 失败响应：`{"code":"BOOK_NOT_AVAILABLE","message":"...","details":...,"request_id":"..."}`；生产环境不返回堆栈或 SQL。

### 10.2 Auth 与 User

| 方法 | 路径 | 鉴权 | 用途 |
|---|---|---|---|
| POST | `/auth/register` | 否 | 注册普通用户 |
| POST | `/auth/login` | 否 | 验证凭据并签发 JWT |
| GET | `/users/me` | 是 | 当前用户资料 |

### 10.3 Book

| 方法 | 路径 | 鉴权 | 用途 |
|---|---|---|---|
| GET | `/books` | 否 | 可售图书列表；仅支持标题关键词、成色、指定排序和分页 |
| GET | `/books/{book_id}` | 否 | 图书详情、可购买状态及按顺序排列的全部图片 |
| POST | `/books` | 是 | 创建 `off_shelf` 商品资料；`seller_id` 来自 JWT |
| PATCH | `/books/{book_id}` | 所有者 | 编辑允许字段 |
| POST | `/books/{book_id}/images` | 所有者 | `multipart/form-data` 上传图片，总数不得超过 6 |
| DELETE | `/books/{book_id}/images/{image_id}` | 所有者 | 删除图片；可售商品不能删除最后一张 |
| PUT | `/books/{book_id}/images/order` | 所有者 | 使用完整图片 ID 数组调整顺序 |
| POST | `/books/{book_id}/off-shelf` | 所有者 | `available` → `off_shelf` |
| POST | `/books/{book_id}/on-shelf` | 所有者 | `off_shelf` → `available`，必须已有 1～6 张图片 |
| GET | `/users/me/books` | 是 | 当前用户发布列表 |

查询参数严格限定为：

```http
GET /api/v1/books?keyword=高等数学
GET /api/v1/books?condition=good
GET /api/v1/books?sort=newest
GET /api/v1/books?sort=price_asc
GET /api/v1/books?sort=price_desc
GET /api/v1/books?page=1&page_size=20
```

- `keyword` 只匹配 `title`，并进行去首尾空白、长度限制和参数化查询。
- `condition` 必须是数据库允许的成色值。
- `sort` 默认 `newest`；价格排序以 `id` 作为稳定的第二排序键，最新排序使用 `created_at DESC, id DESC`。
- 公开列表必须同时满足 `books.status = 'available'` 和关联卖家 `users.status = 'active'`，并通过关联查询动态选择 `sort_order` 最小的图片作为列表封面；详情返回所有图片。
- 不支持上述范围之外的商品检索维度。

### 10.4 Favorite

| 方法 | 路径 | 鉴权 | 用途 |
|---|---|---|---|
| GET | `/favorites` | 是 | 当前用户收藏列表 |
| PUT | `/favorites/{book_id}` | 是 | 幂等添加收藏 |
| DELETE | `/favorites/{book_id}` | 是 | 幂等取消收藏 |

### 10.5 Order

| 方法 | 路径 | 鉴权 | 用途 |
|---|---|---|---|
| POST | `/orders` | 是 + 幂等键 | 以 `book_id` 创建待支付订单 |
| GET | `/orders?as=buyer\|seller` | 是 | 当前用户的买入/卖出订单 |
| GET | `/orders/{order_no}` | 参与者/管理员 | 订单详情 |
| POST | `/orders/{order_no}/pay` | 买家 | 模拟支付 |
| POST | `/orders/{order_no}/cancel` | 买家 | 取消待支付订单 |
| POST | `/orders/{order_no}/complete` | 买家 | 确认完成 |

动作端点用于表达受控状态转换，避免开放通用的“任意修改 status”接口。支付、取消和完成接口也应按当前状态实现重复调用语义：已经达到目标状态可返回当前资源，冲突状态返回 409。

### 10.6 Admin

| 方法 | 路径 | 鉴权 | 用途 |
|---|---|---|---|
| GET | `/admin/users` | 管理员 | 用户列表 |
| POST | `/admin/users/{user_id}/disable` | 管理员 | 禁用用户 |
| POST | `/admin/users/{user_id}/enable` | 管理员 | 恢复用户 |
| POST | `/admin/books/{book_id}/block` | 管理员 | 将可售/下架商品置为 `blocked` |
| POST | `/admin/books/{book_id}/unblock` | 管理员 | 将 `blocked` 商品恢复为 `off_shelf` |
| GET | `/admin/orders` | 管理员 | 订单审计查询 |

管理员治理使用独立 block/unblock 动作，不能复用卖家的上下架权限。管理员将用户设为 `disabled` 后，该用户不能继续访问受保护接口；其 `available` 商品因公开查询同时校验卖家状态而立即停止展示，创建订单也必须再次确认卖家仍为 `active`。禁用操作不批量修改这些商品的 `status`；用户恢复为 `active` 后，仍符合条件的 `available` 商品自动恢复展示。已有待支付订单由既有订单状态机处理，不做隐式跨域批量变更。

## 11. JWT 鉴权流程

1. 注册时规范化邮箱，检查唯一性，使用 bcrypt 哈希密码，只保存 `password_hash`。
2. 登录时按用户名或邮箱查询用户；即使用户不存在也避免返回可用于枚举账号的差异化信息。检查用户为 `active` 后用 bcrypt 比较密码。
3. 后端使用强随机 `JWT_SECRET` 签发短期访问令牌。JWT 主要用于证明身份，`sub = user_id`；Claims 最少包括 `sub`、`iat`、`exp`、`jti`、`iss`，MVP 不依赖令牌中的角色信息做最终授权。
4. 客户端通过 `Authorization: Bearer <token>` 发送令牌。生产环境只允许 HTTPS；前端优先以内存保存令牌，若为简化刷新体验使用持久存储，必须明确 XSS 风险。
5. JWT Middleware 校验签名算法、签名、签发方和过期时间，从 `sub` 解析用户 ID，然后为每个受保护请求查询当前 `users` 记录；用户不存在或 `status != 'active'` 时拒绝请求。
6. Middleware 将数据库当前用户 ID、状态和角色写入请求上下文；敏感操作的 Service 仍查询必要的资源状态并校验所有权，JWT 身份不能替代数据库资源检查。
7. 管理员接口的最终授权条件是数据库当前 `role = 'admin'`。即使 JWT 带有旧的 role claim，也不得据此放行；普通用户访问返回 403。

MVP 仅使用短期 access token，不设计 refresh token 表，也不为每次鉴权所需的当前用户查询增加 Redis 用户缓存。登出由客户端丢弃令牌完成；Phase 6 可在明确需要立即撤销时用 Redis 按 `jti` 建黑名单，TTL 不超过令牌剩余有效期。JWT 密钥轮换、refresh token 与全设备会话管理属于后续增强。

## 12. 并发购买：MySQL Transaction + SELECT FOR UPDATE

订单创建的正确性以 MySQL 为准，核心事务伪流程如下：

```text
BEGIN
  1. 按 (buyer_id, idempotency_key) 查询已有订单；存在则校验指纹并返回它。
  2. SELECT * FROM books WHERE id = ? FOR UPDATE；不存在则 ROLLBACK。
  3. 再查一次幂等订单，覆盖等待图书锁期间另一个同 Key 请求已经提交的情况。
  4. 若书为 reserved，只在其 pending_payment 订单已超时时原子过期并释放；否则拒绝。
  5. 若书最终非 available、或买家等于 seller_id，则返回领域错误并 ROLLBACK。
  6. SELECT status FROM users WHERE id = seller_id FOR SHARE；seller.status != 'active' 则 ROLLBACK。
  7. 从锁定行读取 seller_id、title、price，创建 pending_payment 订单快照。
  8. UPDATE books SET status = 'reserved' WHERE id = ? AND status = 'available'
     并断言 affected_rows = 1。
COMMIT
```

关键点：

- `FOR UPDATE` 必须在显式事务内，并通过主键命中现有行；InnoDB 对该书记录加排他锁。
- 两个买家并发购买同一本书时，第一个事务锁住该行并改为 `reserved`。第二个事务等待，拿到锁后会读到 `reserved`，因此失败；只产生一个有效订单。
- 创建 `pending_payment` 订单时生成列 `active_book_id = book_id`，`UNIQUE(active_book_id)` 会在数据库层拒绝第二条有效交易链；状态转为 `paid`/`completed` 时占位保持不变，只有 `cancelled`/`expired` 才变为 `NULL` 并释放占位。
- 同 Key 请求即使在 Redis 降级时并发，等待图书锁的一方也会在锁后复查并重放已提交订单；若请求目标不同而绕过同一图书锁，则复合唯一键会让其中一个事务回滚，随后查询并按指纹返回原结果或幂等冲突。
- 订单的 `seller_id` 和成交价从锁定后的数据库行复制，绝不相信客户端。建单事务必须用锁定读取得卖家当前数据库状态并确认 `seller.status = 'active'`，使其与管理员禁用操作形成明确的先后顺序。
- 图书状态更新仍带 `status='available'` 条件并检查受影响行数，作为防御性校验。
- 事务内不调用 Redis、外部 HTTP 或执行慢操作，尽快释放行锁；设置合理数据库超时并处理死锁重试（有限次数、仅重试可安全重放的完整事务）。
- 所有涉及订单和图书的事务统一先锁图书、再锁订单/写订单，避免不同锁顺序导致死锁。
- 模拟支付事务锁定图书和订单，验证订单为 `pending_payment`、未过期且图书为 `reserved`，再把订单改为 `paid`、图书改为 `sold`。状态变化不会释放 `active_book_id` 占位。
- 取消/过期事务同样锁定相关记录，同时把订单置终态并把图书恢复为 `available`，不存在“订单已取消但书仍锁定”的中间提交状态。

不使用 Redis 分布式锁：单库事务已经能覆盖核心并发约束，引入分布式锁会增加锁续期、双写一致性和故障恢复复杂度。

## 13. Redis + Idempotency-Key 订单幂等

Redis 负责快速识别并发重试，MySQL 唯一约束负责最终正确性。

### 13.1 Key 和记录

- Redis key：`bookloop:idem:order:create:{buyer_id}:{idempotency_key}`。
- 请求指纹：对规范化后的 `book_id`、用户 ID、API 版本做 SHA-256；JSON 字段顺序不能影响结果。
- `processing` 记录：包含随机 owner token、fingerprint，短 TTL（建议 30 秒）。
- `completed` 记录：包含 fingerprint、HTTP 状态码和最小必要响应（至少 `order_no`），TTL 建议 24 小时。
- MySQL 同时保存 `idempotency_key` 和 `request_fingerprint`，并有 `UNIQUE(buyer_id, idempotency_key)`。

### 13.2 请求流程

1. Handler 验证 Key 格式，Service 计算 fingerprint。
2. 用 Redis `SET key processing-payload NX EX 30` 原子占位。
3. 若已有 `completed`：指纹相同则重放保存的结果；不同则返回 `409 IDEMPOTENCY_KEY_REUSED`。
4. 若已有 `processing`：指纹不同返回 409；相同表示请求仍在处理，返回 `409 REQUEST_IN_PROGRESS` 和 `Retry-After`，客户端稍后以同一 Key 重试。
5. 获得处理权后进入 MySQL 事务。事务开始先按 `(buyer_id, idempotency_key)` 查订单：已有且指纹相同则读取并重放，指纹不同则冲突；否则执行锁书和建单流程。
6. MySQL 提交成功后，用 Lua 比较 owner token，只有占位仍属于当前请求时才把 Redis 记录替换为 `completed` 并延长 TTL，然后响应客户端。
7. 业务失败或事务回滚后，用“比较 owner token 后删除”的 Lua 操作释放自己的 `processing` 记录，不能误删后来请求获得的锁。

### 13.3 故障语义

- Redis 不可用：订单创建可降级走 MySQL 唯一约束和事务，正确性不受影响；记录告警并可能返回较慢。是否允许降级由配置控制，生产默认允许。
- MySQL 已提交、Redis 结果写入失败：首次请求仍返回成功；重试时 MySQL 唯一键查询到原订单并重建 Redis 结果。
- 服务在占位后崩溃：短 TTL 自动释放；若数据库已提交，下一次重试从 MySQL 找回结果。
- 客户端超时：必须用同一 Key 重试，不能生成新 Key。新 Key 会被视为新意图，但图书状态/行锁仍阻止重复有效购买。
- Redis 中不保存订单事实状态，过期或清空不会导致已提交订单消失。

## 14. Docker Compose 架构

计划包含四个运行服务：

```text
Browser ──> nginx（唯一公网入口） ──> backend（内部网络）
              │                         ├──> mysql:8（持久卷）
              │                         ├──> redis（幂等辅助）
              │                         └──> book_uploads（读写）
              └── /uploads/* ──────────────> book_uploads（只读）
```

- `nginx`：多阶段构建前端静态文件，暴露 80/443；反向代理 `/api/` 到 backend；以只读方式挂载图片卷并服务 `/uploads/`。
- `backend`：多阶段构建最小 Go 运行镜像，使用非 root 用户，健康检查 `/healthz` 和 `/readyz`；以读写方式挂载同一图片卷到配置的 `uploads` 目录。
- `mysql`：命名卷保存数据，健康检查完成后 backend 才就绪；生产不映射公网端口。
- `redis`：用于幂等辅助；生产不映射公网端口，配置密码、内存上限和合适的淘汰策略，不能因淘汰影响核心正确性。
- `book_uploads` 命名卷独立于容器生命周期，容器重建不能丢失图片；备份和恢复必须同时覆盖 MySQL 与该卷。
- Compose 使用独立内部网络、服务名 DNS、健康检查和 `restart` 策略。不使用 `depends_on` 代替应用自身的连接重试。
- 数据库迁移由明确的一次性迁移命令/服务执行，避免多个 backend 副本启动时竞态迁移。

## 15. 开发环境与生产环境区别

| 项目 | 开发环境 | 生产环境 |
|---|---|---|
| 前端 | Vite dev server，热更新，代理 `/api` | 构建为静态文件，由 Nginx 服务 |
| 后端 | 本地运行，debug 日志，可连接 Compose 依赖 | 容器运行，info/warn JSON 日志，优雅关闭 |
| 图片文件 | 项目外或被忽略的本地 `uploads` 目录 | Docker 持久卷，backend 读写、Nginx 只读 |
| MySQL/Redis 端口 | 仅绑定 `127.0.0.1` 便于调试 | 不对公网/宿主机开放，仅 Compose 内网 |
| 配置 | 本地 `.env`（不提交） | 平台/服务器注入环境变量或受限 env 文件 |
| JWT 密钥 | 本地随机开发值 | 高熵独立密钥，严格文件权限，支持轮换计划 |
| CORS | 允许明确的本地开发源 | 同源优先或精确生产域名，禁止 `*` 搭配凭证 |
| TLS | 可使用 HTTP localhost | 强制 HTTPS，HTTP 重定向 HTTPS |
| 数据 | 可重建的本地数据与测试图片 | MySQL 和图片卷联合备份、恢复演练 |
| 错误信息 | 可记录较详细诊断 | 客户端隐藏内部细节，服务端结构化记录 |

环境差异通过配置表达，不在业务代码中维护两套逻辑。

## 16. Nginx 部署方案

- Nginx 是唯一公网入口，生产监听 80/443；80 重定向到 443。
- `/` 提供 Vue 静态文件，未知非 API 路径回退到 `/index.html` 以支持前端路由。
- `/uploads/` 从只读图片卷提供静态文件，关闭目录列表，禁止执行内容嗅探或脚本，并只允许后端生成的安全路径。
- `/api/` 反向代理到 `backend:8080`，传递 `Host`、`X-Real-IP`、`X-Forwarded-For`、`X-Forwarded-Proto` 和 `X-Request-ID`。
- 后端只信任明确的 Nginx 代理来源，避免客户端伪造转发头。
- 设置请求体大小、连接/读取超时和基础限流；图片上传同时受 Nginx 与后端文件大小限制。登录与订单创建可使用更严格速率，但不能把限流当作业务一致性方案。
- 静态带 hash 资源设置长期缓存，`index.html` 使用短缓存或不缓存；API 响应默认不由 Nginx 缓存。
- 上传图片使用不可预测且不会覆盖的随机文件名，可设置长期缓存；图片更新通过新增 URL 而不是覆盖同名文件完成。
- TLS 证书和私钥只挂载给 Nginx，不进入镜像和 Git。续期方式按云环境在 Phase 9 落实。
- 日志输出到 stdout/stderr 交由容器日志收集，API 请求沿用/生成 Request ID。

## 17. 环境变量管理

建议变量：

```text
APP_ENV
HTTP_ADDR
LOG_LEVEL
DB_HOST
DB_PORT
DB_NAME
DB_USER
DB_PASSWORD
DB_MAX_OPEN_CONNS
DB_MAX_IDLE_CONNS
REDIS_ADDR
REDIS_PASSWORD
REDIS_DB
JWT_SECRET
JWT_ISSUER
JWT_EXPIRES_IN
BCRYPT_COST
ORDER_PAYMENT_TIMEOUT
IDEMPOTENCY_PROCESSING_TTL
IDEMPOTENCY_RESULT_TTL
CORS_ALLOWED_ORIGINS
UPLOAD_DIR
UPLOAD_PUBLIC_PREFIX
UPLOAD_MAX_BYTES
```

规则：

- 提交 `.env.example`，只含变量名和安全示例；`.env`、证书、密钥、真实密码和本地 `uploads` 内容加入 `.gitignore`。
- 启动时集中读取并校验配置；缺少必需变量、TTL 非法或生产密钥强度不足时立即报错退出。
- 不给生产秘密提供可用的硬编码默认值，不在日志中打印完整数据库 DSN、JWT、密码或 Authorization Header。
- 开发环境可由 Compose `env_file` 读取本地文件；生产由受限权限的服务器配置或部署平台注入。
- 修改配置项时同步更新 `.env.example` 和部署说明；不同环境使用各自数据库、Redis 和 JWT 密钥。

## 18. 错误处理和日志方案

### 18.1 错误分类

- 校验错误：400，例如 `VALIDATION_ERROR`。
- 未认证：401，例如 `UNAUTHORIZED`、`TOKEN_EXPIRED`。
- 无权限：403，例如 `FORBIDDEN`、`BOOK_NOT_OWNER`。
- 资源不存在：404，例如 `BOOK_NOT_FOUND`、`ORDER_NOT_FOUND`。
- 状态/唯一性冲突：409，例如 `BOOK_NOT_AVAILABLE`、`BOOK_IMAGE_LIMIT_REACHED`、`BOOK_IMAGE_REQUIRED`、`ORDER_STATE_CONFLICT`、`IDEMPOTENCY_KEY_REUSED`。
- 语义不可处理：422，用于格式正确但不满足明确业务输入规则的情况；避免与 409 混用。
- 限流：429；依赖暂时不可用：503；未知内部错误：500。

Service 返回可用 `errors.Is/As` 判断的领域错误，Handler 统一映射，Repository 将“无记录、唯一键冲突、死锁/超时”等数据库错误转换成有限的基础错误，避免泄漏 SQL 实现。

### 18.2 日志

- 使用 Go 标准库结构化日志能力输出 JSON，字段至少包括 timestamp、level、message、request_id、method、path、status、duration、user_id（存在时）和 error_code。
- 订单关键转换记录 order_no、book_id、actor_id、from_status、to_status；不记录密码、JWT、Authorization、完整请求体和个人敏感信息。
- 访问日志由 Middleware 在请求结束时统一记录。panic 由 Recovery 捕获，记录内部错误并返回通用 500。
- 预期业务冲突使用 info/warn，未知错误使用 error；同一错误避免在 Repository、Service、Handler 重复记录三次。
- Request ID 优先接受可信 Nginx 传入值，否则后端生成，并写入响应，方便串联 Nginx 与应用日志。

## 19. 测试策略

### 19.1 测试层次

- Service 单元测试：状态转换、所有权、图片数量/排序、禁止自购、禁用用户、金额和输入边界；仅在确有测试替身需求的边界定义小接口。
- Handler 测试：使用 `httptest` 验证参数、鉴权上下文、状态码和统一响应格式，不连接真实数据库。
- Repository/迁移集成测试：使用真实 MySQL 8 验证索引、外键、生成列、事务和 GORM 查询；SQLite 不能替代行锁测试。
- 图片存储测试：使用临时目录验证类型/大小限制、随机文件名、路径安全、写入补偿、删除失败处理和存储接口替换边界。
- Redis 集成测试：验证 NX 占位、TTL、同 Key 不同指纹、owner token 安全释放和结果重放。
- 端到端测试：注册 → 登录 → 发布 → 收藏 → 创建订单 → 模拟支付 → 完成的核心路径。
- 前端验证：TypeScript 类型检查、生产构建、关键页面与权限路径的最小测试；Phase 7 再按实际需求选择最小测试工具，不在 Phase 0 引入依赖。

### 19.2 必测场景

1. 买家购买自己的书返回 403，数据库不产生订单，图书仍为 `available`。
2. 非所有者编辑、上下架、上传、删除或排序图片均返回 403；管理员 block/unblock 权限独立验证。
3. 无图片不能上架；上传第一张后可以上架；`available` 商品删除最后一张图片被拒绝。
4. 单请求和并发上传都不能让同一本书超过 6 张图片；重复/外部图片 ID 的排序请求被拒绝。
5. 调整顺序后列表使用最小 `sort_order` 图片，详情按顺序返回全部图片；`blocked`、`sold`、`reserved` 的图片修改被拒绝。
6. 标题搜索只匹配标题，成色筛选正确，三种排序在价格/时间相同时仍有稳定的 ID 次序，分页无重复遗漏。
7. 图片类型伪装、超限文件和路径穿越输入被拒绝；数据库写入失败会清理新文件，容器重建后持久卷图片仍可访问。
8. N 个独立数据库连接并发购买同一本书，恰好一个创建成功，一个待支付订单有效，图书为 `reserved`；`UNIQUE(active_book_id)` 无法插入第二条有效交易链。
9. 订单处于 `pending_payment`、`paid`、`completed` 时 `active_book_id` 始终占用该书；支付后图书为 `sold`，任何新订单均失败。
10. 取消或过期后 `active_book_id` 变为 `NULL`，订单与图书释放原子完成，随后允许新买家下单。
11. 非法订单状态转换均返回 409，订单和图书状态不变。
12. 相同用户、相同 Key、相同请求只创建一条订单并返回同一 order_no。
13. 相同 Key、不同请求返回幂等冲突；不同用户可以使用相同 Key。
14. MySQL 已提交但 Redis 结果缺失时，重试仍从 MySQL 返回原订单。
15. Redis 不可用时核心数据仍正确，按配置降级并产生可观测日志。
16. 有效 JWT 对应用户被禁用后，后续所有受保护请求均失败；用户恢复 `active` 后，在令牌仍有效的前提下可重新通过身份检查。
17. JWT 中即使存在旧的管理员角色信息，只要数据库当前 `role` 不是 `admin` 就不能访问管理员接口；最终授权始终使用数据库当前角色。
18. 禁用卖家的 `available` 商品不出现在公开列表且不能创建新订单，商品状态保持不变；恢复卖家后，符合条件的商品自动重新展示。

### 19.3 验证命令原则

后端每个阶段至少运行：

```bash
go fmt ./...
go vet ./...
go test ./...
```

涉及 MySQL/Redis 的阶段还需运行带真实依赖的集成测试；前端阶段运行类型检查和生产构建。测试失败必须报告，不能用跳过测试宣称完成。

## 20. Phase 0～Phase 10 开发计划与验收标准

每个 Phase 只实现本阶段范围，完成后总结文件、设计决策、实际执行的验证命令及结果，并等待确认。

### Phase 0：系统设计

工作：确定 MVP、架构、数据模型、API、状态机、一致性、幂等、部署、安全和分阶段计划。

验收标准：

- `docs/PLAN.md` 覆盖本设计中的 24 项要求，无业务代码变更。
- 数据库模型固定为五张核心表，图书字段保持精简，图片使用独立一对多表和本地持久化存储。
- 一本书只售一次、禁止自购、所有权、状态合法性、并发和幂等均有数据库与服务层方案。
- 技术选型保持模块化单体且与 `AGENTS.md` 一致。

### Phase 1：Go 后端基础设施

工作：初始化 Go 模块；配置、Gin 路由、统一响应/错误、日志、Request ID、Recovery、MySQL/Redis 客户端、健康检查、依赖组装和优雅关闭；建立迁移执行约定。

验收标准：

- 服务可由环境变量启动，配置错误会明确失败。
- `/healthz` 可反映进程存活，`/readyz` 可反映关键依赖就绪状态；MySQL 是必需依赖，Redis 按已配置的降级策略判定并单独暴露状态。
- 收到终止信号时停止接收新请求、限时等待在途请求并关闭连接。
- `go fmt ./...`、`go vet ./...`、`go test ./...` 全部通过。

### Phase 2：User 与 Authentication

工作：用户迁移、Repository/Service/Handler、注册、登录、bcrypt、JWT Middleware、`/users/me`、角色与禁用状态基础检查。

验收标准：

- 密码从不明文存储或返回；重复用户名/邮箱得到稳定错误。
- 合法登录获得 JWT；缺失、篡改、过期令牌被拒绝。
- 每个受保护请求都按 JWT `sub` 查询数据库当前用户；禁用用户不能访问，管理员权限以数据库当前 `role` 为准，并覆盖令牌角色过期场景。
- MVP 鉴权不增加 Redis 用户缓存，相关测试验证数据库状态变更立即生效。
- 单元、Handler 和 MySQL 集成测试通过。

### Phase 3：Book Marketplace

工作：`books` 与 `book_images` 迁移；精简商品资料；标题搜索、成色筛选、三种排序和分页；本地图片存储、上传/删除/排序；上架/下架、“我的发布”、所有权校验和管理员 block/unblock。

验收标准：

- `seller_id` 只来自 JWT；只有所有者能修改商品和图片，`blocked`/`sold` 图片不可修改。
- `books` 只包含本文定义的九个字段，`book_images` 只包含本文定义的五个字段；关系和约束由真实迁移验证。
- 商品有 1～6 张图片才能上架；列表取最小顺序图片，详情返回全部图片，调整顺序结果正确。
- 公开列表只包含 `books.status = 'available'` 且 `seller.status = 'active'` 的商品；标题关键词、成色、最新、价格升降序和分页正确且不接受额外检索维度。
- 禁用卖家不会批量改变商品状态；恢复卖家后，其符合条件的 `available` 商品自动重新展示。
- 状态转换遵守图书状态机，卖家、管理员和订单业务的转换权限彼此分离。
- 图片校验、路径安全、失败补偿、索引和查询经测试，后端检查全通过。

### Phase 4：Favorites

工作：收藏迁移、添加、取消和列表。

验收标准：

- 重复添加不产生重复行；重复取消保持成功的幂等语义。
- 用户只能读取和操作自己的收藏。
- 下架、封禁和已售商品的收藏展示行为明确且测试覆盖。
- Repository、Service、Handler 测试和后端检查通过。

### Phase 5：Orders

工作：订单迁移、创建、买家/卖家列表、详情、模拟支付、取消、过期、完成；实现 MySQL 事务、`SELECT FOR UPDATE`、状态机和 MySQL 幂等唯一键。

验收标准：

- 禁止自购，成交价与卖家均取自锁定的图书行，创建订单时再次确认卖家数据库当前状态为 `active`。
- 并发测试中同一本书恰好一个有效交易链；`UNIQUE(active_book_id)` 覆盖待支付、已支付和已完成状态，取消/过期后才释放占位。
- 创建、支付、取消/过期操作中的订单与图书状态原子一致。
- 非参与者不可看订单，所有非法转换被拒绝。
- 真实 MySQL 并发/集成测试及全部 Go 检查通过。

### Phase 6：Redis Enhancements

工作：按本文方案加入订单创建的 Redis 幂等协调、结果重放、故障降级与指标/日志；仅在明确需要时增加 JWT `jti` 黑名单。

验收标准：

- 同 Key 同请求并发/重试只产生一条订单并可重放同一结果。
- 同 Key 不同请求稳定返回冲突；owner token 防止误删占位。
- 覆盖占位后崩溃、提交后缓存失败、TTL 到期和 Redis 不可用场景。
- Redis 清空不破坏 MySQL 数据正确性；测试和 Go 检查通过。

### Phase 7：Vue Frontend

工作：Vue 3 + TypeScript + Vite + Element Plus + Pinia + Axios；实现登录、商品列表/详情/发布编辑、1～6 张实物图片上传与排序、收藏、买卖订单、模拟支付与基础管理页面。

验收标准：

- 核心交易闭环可通过浏览器完成，加载、空态和错误态清晰。
- 发布流程会先创建下架商品、上传至少一张图片再上架；列表封面和详情图片顺序与后端一致。
- Axios 全局 interceptor 可以统一附加 JWT 和通用 Request ID 关联信息，但不得为每个请求自动生成 `Idempotency-Key`。
- `createOrder` 业务在一次新的购买意图开始时生成一次 Key，并显式附加到创建订单请求；同一购买意图发生网络超时或请求重试时必须继续复用原 Key，只有新的购买意图才生成新 Key。
- 路由守卫与角色 UI 正确，但后端仍是权限事实源。
- TypeScript 类型检查和生产构建通过，关键流程完成验证。

### Phase 8：Dockerization

工作：后端/前端多阶段镜像、Compose、Nginx 配置、健康检查、网络、MySQL 与 `book_uploads` 持久卷、迁移命令和 `.env.example`。

验收标准：

- 一条明确命令可从干净环境启动完整系统并完成迁移。
- 容器健康检查通过；重建容器后 MySQL 数据和上传图片均保留，Nginx 可继续读取图片。
- 只有 Nginx 对外暴露，MySQL/Redis 不公开；镜像中无源代码秘密。
- backend 对图片卷为读写权限，Nginx 仅只读；本地上传内容不进入 Git 或镜像构建上下文。
- Compose 环境完成核心端到端交易验证。

### Phase 9：Cloud Deployment

工作：云服务器拉取部署、DNS/TLS、Nginx 生产配置、防火墙、MySQL 与图片卷联合备份、恢复演练、日志轮转和发布/回滚手册。

验收标准：

- HTTPS 域名可访问，HTTP 自动跳转 HTTPS，API 与 SPA 路由正确。
- 数据库和 Redis 端口未公网开放，秘密不在仓库和镜像中。
- 完成一次数据库与图片文件一致恢复验证，以及一次可执行的版本回滚演练。
- 线上健康检查、核心闭环和基本安全配置验证通过。

### Phase 10：CI/CD

工作：提交/PR 检查、构建镜像、制品标记、部署审批、数据库迁移与回滚流程。

验收标准：

- CI 自动执行 Go 格式检查、vet、测试，以及前端类型检查/构建。
- 只有全部检查通过的提交可生成带不可变版本标记的镜像。
- 生产部署需要明确审批，使用受保护秘密，失败可回滚上一镜像。
- 迁移步骤可审查，破坏性变更有向后兼容/回滚说明。

## 21. 适合写入简历的技术点

应写可解释、可验证的成果，不堆砌名词；只有对应 Phase 完成并有测试证据后才写入简历。

- 使用 Go、Gin、GORM 构建 Handler → Service → Repository 的模块化单体 REST API，清晰分离 HTTP、业务和数据访问职责。
- 基于 MySQL InnoDB 事务与 `SELECT FOR UPDATE` 设计二手书抢购流程，并用条件更新、统一锁顺序和 `UNIQUE(active_book_id)` 生成列约束保证一本书最多存在一个有效交易链。
- 设计图书与订单状态机，在事务内原子维护 `order.status` 与 `book.status`，覆盖取消、超时、支付和非法转换测试。
- 使用 Redis `SET NX EX`、请求指纹、owner token 和结果重放实现订单创建幂等，并以 MySQL 复合唯一键处理缓存失效和提交后崩溃场景。
- 使用 bcrypt 和 JWT 证明请求身份，并在每个受保护请求中读取数据库当前用户状态与角色完成授权，使禁用和角色变更立即生效。
- 设计可替换的图片存储边界，以 Go 后端安全上传、本地持久卷和 Nginx 静态服务实现 1～6 张实物图片管理与有序封面选择。
- 为高风险业务编写真实 MySQL 并发集成测试，验证多请求竞争时只有一个订单成功。
- 使用 Docker Compose 编排 Nginx、Go、MySQL、Redis，完成健康检查、优雅关闭、内部网络、持久化和 HTTPS 生产部署。
- 建立结构化日志、Request ID、统一错误码和 CI/CD 质量门禁，提高问题定位和交付可靠性。

## 22. 关键设计结论

- 核心业务表固定为 `users`、`books`、`book_images`、`favorites`、`orders`；Admin 是用户角色，不单独建表。
- `books` 仅保存商品交易必需信息，图片由 `book_images` 一对多维护；不建立额外书目或交易字典业务表。
- 每件可售商品必须有 1～6 张图片，最小 `sort_order` 图片作为列表封面；本地文件由持久卷保存并由 Nginx 提供静态访问。
- MySQL 是唯一事实源；Redis 只优化订单幂等协调和结果重放。
- “一个有效交易链”由行锁与图书状态控制主路径，再由 `UNIQUE(active_book_id)` 覆盖 `pending_payment`、`paid`、`completed`，取消/过期后释放数据库占位。
- JWT 主要证明 `sub = user_id`；所有受保护请求查询数据库当前用户，要求 `status = 'active'`，管理员权限使用数据库当前 `role`，不增加 Redis 用户缓存。
- 公开商品必须同时满足图书可售和卖家有效；禁用卖家不改变图书状态，但其商品立即隐藏且不能创建新订单，恢复后自动按原状态重新展示。
- “不能自购”和“商品所有权”同时在 Service 校验，并用可信 JWT 身份、数据库当前用户状态、订单 `CHECK` 等方式纵深防御。
- Book 和 Order 都有封闭状态机，所有跨表转换都在一个事务内完成。
- 当前设计保持单体、五张核心表和少量必要基础设施，不引入真实支付、外部图片存储服务或分布式系统组件。
