# 微信联系与线下交易

商品发布流程为：保存资料 → 上传 1～6 张实物图片 → 上传微信联系二维码 → 确认上架。平台支付功能已移除，买家通过商品详情页“联系卖家”查看二维码，协商线下交付；订单保留预约、取消、超时和买家确认完成。

## 接口与执行流程

`PUT /api/v1/books/{book_id}/wechat-qr-code` 使用 `multipart/form-data`，文件字段为 `image`，要求 JWT 身份认证。成功返回 `data.wechat_qr_code_url`；商品详情也返回该字段。二维码在详情页公开展示，不作为封面，也不占实物图片名额。

Handler 负责读取文件、限制请求大小和提取当前用户；Service 校验商品所有权和状态，调用现有图片存储验证 JPEG/PNG 内容及大小，再在事务中用 `SELECT FOR UPDATE` 锁定商品，重新校验所有者、可修改状态及实物图片数量。Repository 单独更新 `books.wechat_qr_code_url`。

上传仍需要至少一张实物图片；`reserved`、`sold`、`blocked` 状态不允许更改二维码。图片检查确认文件格式有效，不进行微信二维码识别或好友链接真实性验证，卖家需上传可用的个人联系二维码。

上架仍走独立动作接口。Service 在商品行锁内同时检查实物图片数量和联系方式，缺少二维码返回 `409 BOOK_CONTACT_REQUIRED`。这样前端禁用上架按钮之外，直接请求 API 也必须满足条件；并发上传、上架和订单创建通过同一商品行锁协调。

文件先写临时目录并校验，事务内提交文件路径和数据库元数据。数据库失败会补偿删除新文件；更换成功后才清理旧文件，失败时保留原联系方式。文件系统无法参与 MySQL 事务，清理失败会记录日志；进程异常仍可能留下孤立文件，沿用现有商品图片的存储约定。

不允许直接在商品 JSON 中传入二维码 URL，避免客户端绕过上传校验或指定任意地址。二维码地址单独存于商品表，复用现有图片存储，无需新增基础设施。

## 订单状态

`POST /api/v1/orders/{order_no}/pay` 已删除。订单由 `pending_handover`（待线下交付）直接进入 `completed`，买家确认时在同一事务内将商品改为 `sold`；仍支持取消和预约超时。重复完成不会重复发通知。

支付状态、`paid_at` 字段和支付事件消费已删除。预约有效期使用 `ORDER_RESERVATION_TIMEOUT` 配置，示例为 15 分钟。

## 升级与验证

本次允许清空开发数据，因此应先重置目标环境的数据库、上传图片、缓存和消息，再执行迁移并启动新版后端。`000007_use_offline_orders` 要求订单表中没有旧支付状态记录；不会自动转换旧订单。历史迁移文件保留校验和，新版最终结构由追加迁移建立。

空数据库初始化命令：

```bash
cd backend
go run ./cmd/migrate -dir ./migrations
```

迁移 `000006_require_wechat_contact` 添加商品二维码字段；`000007_use_offline_orders` 删除支付时间字段，并将订单状态约束和有效交易唯一索引改为线下交付流程。回滚 `000007` 前同样需要清空订单，再启动旧版本程序。

本机已按授权清空并重建 `bookloop-acceptance`（8088）和 `bookloop-verify`（18088）。对应启动配置保存在 Git 忽略的 `.env.reset.acceptance`、`.env.reset.verify`，文件权限为 600；登录密钥已更新，使用前需重新注册。以后在仓库根目录启动：

```bash
docker compose --env-file .env.reset.acceptance -p bookloop-acceptance up -d --wait
docker compose --env-file .env.reset.verify -p bookloop-verify up -d --wait
```

检查命令：

```bash
cd backend
go fmt ./...
go vet ./...
go test ./...
go test -race ./internal/service ./internal/router
# 使用独立测试数据库，仓储测试会重建业务表：
BOOKLOOP_TEST_MYSQL_DSN='<test-only DSN>' go test ./internal/repository -count=1 -v

cd ../frontend
npm run build
npm test
```

手动验收：创建商品后先传二维码应被拒绝；仅上传实物图不能上架；补充二维码后可上架并在详情预览；其他用户不能更改二维码；预约后确认线下交付即可完成，无支付入口。
