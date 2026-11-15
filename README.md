# LACTSSL-Plugins

LACTSSL 官方插件实现仓库，包含 DNS、证书部署与通知三类 helper，以及它们使用的协议与元数据。

主仓库 [LACTSSL](https://github.com/AXmishell/LACTSSL) 只保留插件宿主（发现、校验、执行、启停、上传 API/UI），通过 `go.mod` 固定本仓库版本并直接构建 `cmd/*-helper`。

## 目录

```text
protocol/        插件协议 v1 的线格式（零第三方依赖）
sdk/helper/      helper 启动框架：内存预算、JSON、错误退出
catalog/         官方服务商 / 部署目标元数据（data.json）
i18n/            插件侧精简文案
outbound/        SSRF 防护出站 HTTP 客户端
dns/             194 个 lego 服务商包装 + 注册表
deploy/          FTP/Webhook/阿里云 CDN/七牛/面板
notify/          23 个通知渠道
cmd/             dns-helper / deploy-helper / notify-helper
testdata/        迁移前 manifest 基线
```

## 构建

```bash
make build        # 三个 helper 输出到 bin/
make test         # 单测（含 manifest 基线对比）
make check        # gofmt + vet + test
make gen          # 重新生成 194 个 DNS 包装包与 all 清单
```

交叉编译与发布由 `.github/workflows/release.yml` 负责，产物：

- `lactssl-dns-helper-extra`
- `lactssl-deploy-helper`
- `lactssl-notify-helper`
- `checksums.txt`

## 协议

helper 协议保持 v1：

```text
dns-helper    manifest | present | cleanup
deploy-helper manifest | deploy
notify-helper manifest | send
```

- 退出码 `0` 成功，非 `0` 失败；stderr 进入主程序日志。
- 内存预算通过 `LACTSSL_PLUGIN_MEMORY_LIMIT`（字节）传入。
- 主程序只传递最小环境变量集合，并周期性检查 `VmRSS`。
- 请求 JSON 与 manifest 字段定义见 `protocol/`。

`testdata/` 中的三份 manifest 是迁移前主仓库 helper 的输出基线，`cmd/*-helper` 的测试会逐字段对比，确保本次拆分没有改变任何服务商 ID、别名、字段或 `Env` 映射。

## 版本

- SemVer tag，例如 `v0.1.0`。
- 主仓库固定版本；升级插件版本时单独提交依赖变更。
- 协议 v1 内只做向后兼容的可选字段扩展。
