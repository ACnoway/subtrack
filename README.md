# SubTrack

SubTrack 是一个面向个人用户的订阅与数字资产管理平台，计划以 Web 应用和 Docker Compose 的方式自托管。它帮助集中管理 VPS、域名、云服务、软件订阅和 API 服务，并跟踪续费日期、费用与通知记录。

> **项目状态：规划与仓库初始化阶段。** 当前仓库包含产品开发大纲和项目说明；应用代码、数据库迁移及 Docker 部署配置尚未实现。

## 计划功能

- 统一管理订阅和数字资产，以及到期日期、续费周期、费用、自动续费状态和备注。
- 通过 Email 和 Telegram 发送可配置的到期提醒，并保留通知记录、支持失败重试。
- 记录续费历史，在续费后更新日期并重新安排提醒。
- 查看即将到期项目和订阅支出，并支持数据导入、导出与备份。
- 使用 Docker Compose 自托管，减少对商业 SaaS 服务的依赖。

详细需求、页面规划、数据模型、API 草案和验收标准见[开发大纲](docs/开发大纲.md)。

## 计划技术栈

| 部分 | 计划方案 |
| --- | --- |
| 主要开发语言 | Go：Web 服务、REST API 和后台 Worker |
| Web 页面 | Go `html/template` 服务端渲染、HTML、CSS，必要时辅以少量浏览器脚本 |
| 数据库 | PostgreSQL，通过 Go 数据库驱动访问并使用 SQL 迁移 |
| 通知 | SMTP、Go 邮件组件、Telegram Bot API |
| 后台任务 | 独立 Go Worker，任务状态保存在 PostgreSQL |
| 部署 | Docker Compose，可配合 Caddy 或 Nginx |

应用服务端与 Worker 使用 Go，不依赖 Next.js 或 Node.js 作为运行时。以上是当前规划，具体实现细节会在开发过程中补充。

## 仓库内容

```text
.
├── README.md
├── CONTRIBUTING.md
├── LICENSE
└── docs/
    └── 开发大纲.md
```

## 开发路线

1. 基础资产管理、认证、数据库和 Docker 部署。
2. Email 与 Telegram 通知、持久化提醒任务和重试。
3. 续费闭环、历史记录、统计和数据导入导出。
4. 域名信息检测、服务商 API、多用户及更多通知渠道。

## 开始使用

应用尚未实现，因此目前没有可运行的安装或启动命令。Go 模块、环境变量和 Docker Compose 配置会在对应功能落地时补充到本 README 和部署文档中。

## 安全说明

SubTrack 的设计原则是不保存服务器 root 密码或服务商账户密码。SMTP 凭据和 Telegram Bot Token 等必要密钥应通过受保护的环境变量或密钥管理方式配置，不能提交到 Git。面向公网部署时应启用 HTTPS，并定期备份和验证恢复 PostgreSQL 数据。

## 许可证

本项目采用 [MIT License](LICENSE)。

## 语言

项目文档目前以中文编写。欢迎通过 Issue 或 Pull Request 讨论需求、报告问题和提交改进；提交前请阅读[贡献指南](CONTRIBUTING.md)。
