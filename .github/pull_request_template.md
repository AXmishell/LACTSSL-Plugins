## 变更内容

<!-- 一句话说明这个 PR 做了什么，以及为什么。 -->

## 类型

- [ ] 新增 DNS provider
- [ ] 新增部署目标
- [ ] 新增通知渠道
- [ ] 修复 provider 行为
- [ ] 协议 / SDK / 构建 / CI
- [ ] 文档

## 影响范围

- Provider / 目标 / 渠道 ID：
- 是否涉及凭据、证书、SSRF、签名等安全逻辑：
- 是否改变 manifest 字段或默认值：
- 是否需要版本重置或 `allow_downgrade`：

## 验证

- [ ] `make manifest`
- [ ] `gofmt -w .`
- [ ] `go vet ./...`
- [ ] `go test ./...`
- [ ] `./scripts/check_native.sh`（涉及 native DNS 时）
- [ ] 本地运行 `manifest` 并确认 provider 数量/字段
- [ ] 新增/修改的 i18n key 已同时更新 `zh-CN` 与 `en-US`

## 生成物

- [ ] 已提交 `catalog/data.json`、`i18n/locales`、`plugins.json` 等生成物
- [ ] 或已确认由 `Update generated files` bot 回写

## 备注

<!-- 截图、API 文档、测试账号说明、已知限制等。不要贴真实 token。 -->
