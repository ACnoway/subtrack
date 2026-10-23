# SubTrack

SubTrack 是一个自托管的订阅与数字资产管理平台，用于集中记录 VPS、域名、云服务、软件订阅和 API 服务的费用及到期信息，并通过 Email 和 Telegram 发送到期提醒。项目当前已完成第三阶段：续费和统计。

## 当前功能

- 单管理员登录，使用安全签名的 HttpOnly 会话 Cookie 和 CSRF 防护。
- 资产新增、编辑、永久删除、归档和恢复。
- 记录类别、服务商、账户备注、购买/到期/扣费日期、续费周期与价格、自动续费、续费链接、优先级、标签和备注。
- 按名称、服务商、账户和标签搜索，按类别及到期状态筛选。
- 展示正常、30 天内到期、今天到期、已过期和已归档状态。
- 在仪表盘查看到期概览、近期到期资产，以及按币种和类别折算的月度/年度预计支出。
- 按月查看到期日历，分别标记到期和下次扣费日期。
- 手动确认续费，按续费周期建议新日期并允许修改；月末日期按目标月份最后一天截断。
- 保存续费历史，并在同一数据库事务中更新资产到期日和下次扣费日。
- 导出或导入包含资产、归档状态及续费历史的 JSON 备份；导入为新增操作并整体事务提交。
- PostgreSQL 持久化和启动时自动执行的版本化 SQL 迁移。
- Email SMTP 通知、Telegram Bot 一次性链接绑定、通知测试及发送历史。
- 可配置提醒天数、重要资产额外提醒、发送时区和每日发送时间。
- 独立 Go Worker 提前生成持久化任务，按渠道去重并对失败发送进行指数退避重试；已发送任务不会重复发送。
- Docker Compose 分别运行 Web、Worker 和 PostgreSQL，数据库数据保存在命名卷中。

多用户支持和服务商自动同步属于后续阶段。完整规划见[开发大纲](docs/开发大纲.md)。

## 部署

需要 Docker Engine 和 Docker Compose v2。

```sh
cp .env.example .env
```

编辑 `.env` 中的管理员用户名、强密码、PostgreSQL 密码和会话密钥。可用以下命令生成会话密钥：

```sh
openssl rand -base64 48
```

请将输出填入 `SESSION_SECRET`。`DATABASE_URL` 中的数据库用户名和密码须与 `POSTGRES_USER`、`POSTGRES_PASSWORD` 一致；若密码含 URL 特殊字符，请对连接串中的用户名和密码进行百分号编码。`.env` 已被 Git 忽略，不要提交真实密钥。

要使用 Email 通知，请配置 `SMTP_HOST`、`SMTP_PORT`、`SMTP_FROM`，并按邮件服务要求设置 `SMTP_USERNAME`、`SMTP_PASSWORD` 和 `SMTP_ENCRYPTION`（`starttls`、`tls` 或 `none`）。要使用 Telegram，请通过 BotFather 创建机器人并配置 `TELEGRAM_BOT_TOKEN`；`TELEGRAM_BOT_USERNAME` 可选，留空时页面会从 Bot API 获取。通知邮箱、提醒时区和规则在网页的“提醒设置”中配置。

### 本地开发

`compose.dev.yaml` 会从当前源码构建应用镜像：

```sh
docker compose -f compose.dev.yaml up -d --build
```

### Release 部署

`compose.release.yaml` 是完整的独立部署文件，会从 Docker Hub 拉取 `acnofun/subtrack:latest`。如需固定到某个版本，编辑该文件中 `app.image` 的值，例如 `acnofun/subtrack:0.3.0`，然后运行：

```sh
docker compose -f compose.release.yaml up -d
```

默认打开 `http://localhost:8080`，使用 `.env` 配置的管理员账号登录。可通过 `http://localhost:8080/healthz` 检查应用和数据库连接，`http://localhost:8080/worker-healthz` 检查 Worker 心跳。部署到公网时应在 Caddy 或 Nginx 后启用 HTTPS，并将 `COOKIE_SECURE=true`。填写 `APP_PUBLIC_URL` 后，提醒消息还会提供打开管理平台的链接。

查看状态和日志：

```sh
docker compose -f compose.dev.yaml ps
docker compose -f compose.dev.yaml logs -f app
docker compose -f compose.dev.yaml logs -f worker
```

Release 部署时将命令中的 `compose.dev.yaml` 换成 `compose.release.yaml`。两个 Compose 文件都会启动独立 Worker。停止服务但保留数据：

```sh
docker compose -f compose.dev.yaml down
```

数据库保存在 `subtrack_data` 命名卷中。`docker compose down -v` 会删除该卷和数据库内容，请仅在确实要清空数据时使用。

## 自动发布与版本管理

推送 `v0.3.0` 形式的正式版本 tag 后，GitHub Actions 会检查 Go 代码，构建 amd64/arm64 镜像并推送 Docker Hub。普通 commit、PR 和预发布 tag 不触发镜像发布。配置凭据、版本规则、发布命令及预构建镜像部署方式见[版本发布文档](docs/版本发布.md)。

## 本地开发

需要 Go 1.26 或更新版本，以及一个 PostgreSQL 实例。设置 `DATABASE_URL`、`ADMIN_USERNAME`、`ADMIN_PASSWORD` 和至少 32 个字符的 `SESSION_SECRET` 后运行：

```sh
go run ./cmd/subtrack
```

应用启动时会检查数据库连接并应用尚未执行的 `internal/store/migrations/*.sql` 迁移。
另一个终端可用 `go run ./cmd/subtrack worker` 启动通知 Worker。

## 技术栈

- Go `net/http` 与 `html/template` 服务端渲染
- PostgreSQL 与 `pgx` `database/sql` 驱动
- Docker Compose

应用和后台 Worker 规划使用 Go；浏览器界面不需要 Node.js 运行时。

## 贡献

请阅读[贡献指南](CONTRIBUTING.md)，并在修改功能行为时同步更新文档。

## 许可证

本项目采用 [MIT License](LICENSE)。
