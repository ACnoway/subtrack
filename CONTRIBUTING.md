# 贡献指南

感谢你关注 SubTrack。提交代码前，请先阅读 [README](README.md) 和[开发大纲](docs/开发大纲.md)，并在 Issue 中说明问题或拟议的改动。

## 提交建议

- 项目服务端、REST API 和后台 Worker 使用 Go；网页以 Go 模板服务端渲染为主。
- 一个 Pull Request 尽量聚焦一项功能或修复，并说明背景、改动和验证方式。
- 新增功能应与开发大纲中的阶段和安全原则保持一致；若需要调整产品方向，请先在 Issue 中讨论。
- 不要提交 `.env`、访问令牌、SMTP 密码、Bot Token、真实用户数据或其他凭据。
- 更新功能行为时，请同步维护相关文档。
- 正式版本使用 `vMAJOR.MINOR.PATCH` tag 发布，流程和 Docker Hub 配置见[版本发布文档](docs/版本发布.md)。

## 开发与验证

项目使用 Go 1.26+ 和 PostgreSQL。设置所需环境变量并准备数据库后，可运行：

```sh
gofmt -w ./cmd ./internal
go test ./...
go vet ./...
docker compose up --build
```

启动过程中会自动应用版本化 SQL 迁移。`.env`、数据库连接串及其他真实凭据不得提交。
