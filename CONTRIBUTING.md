# 贡献指南

LACTSSL-Plugins 采用**模块化单体**：每个 provider 一个目录，聚合文件由生成器产出，多个贡献者可以并行开发而尽量不修改同一份文件。

面向使用者的插件协议说明见主仓库文档：[docs/plugin-development.md](https://github.com/AXmishell/LACTSSL/blob/main/docs/plugin-development.md)。

## 1. 开发环境

```bash
git clone https://github.com/AXmishell/LACTSSL-Plugins.git
cd LACTSSL-Plugins
go version   # 需要 go.mod 中声明的 Go 版本
make manifest
go test ./...
```

常用命令：

| 命令 | 作用 |
|---|---|
| `make manifest` / `make gen` | 生成全部聚合文件与 `plugins.json` |
| `make catalog` | 合并 `catalog/dns`、`catalog/deploy` 为 `catalog/data.json` |
| `make i18n` | 合并 `i18n/core`、`i18n/providers` 为 `i18n/locales` |
| `make deployimports` | 生成 `cmd/deploy-helper/imports_gen.go` |
| `make gen-v5` | 生成 lego v5 包装、分组与元数据 |
| `make bundle` | 构建全部合并包到 `bin/` |
| `make bundle-native` / `bundle-lego` / `bundle-lego-v5` | 构建单个 DNS 变体 |
| `go test ./...` | 全量单元测试 |
| `./scripts/check_native.sh` | native 包体积、依赖、manifest 一致性检查 |

## 2. 目录约定

```text
protocol/                    插件协议 v1 线格式
sdk/                         共享 HTTP、签名、DNS 工具
i18n/core/                   公共文案（按语言一个文件）
i18n/providers/dns/<id>/     单 provider 文案
i18n/providers/deploy/<id>/
catalog/dns/<id>.json        单 DNS provider 元数据
catalog/deploy/<id>.json     单部署目标元数据
catalog/migrations.json      历史部署 ID 与迁移配置
catalog/data.json            生成物：聚合元数据，禁止手改
dns/providers/<id>/          lego v4 包装（生成）
dns/native/providers/<id>/   native DNS 实现（手写）
dnsv5/                       lego v5 包装、分组、元数据（生成）
deploy/providers/<id>/       部署目标（手写）
notify/providers/channels/   每渠道一个 channel_<id>.go
cmd/                         helper 入口
scripts/                     生成器与构建工具
assets/icons/                图标
plugins.json                 生成物：合并包清单
```

**生成物禁止手改**。修改源文件后运行 `make manifest`，或让 PR 上的 `Update generated files` bot 回写。

## 3. 新增 DNS provider

### 3.1 Native（推荐用于重构/自研实现）

1. 创建 `dns/native/providers/<id>/provider.go`，实现：

```go
type plugin struct{}

func (plugin) ID() string        { return "<id>" }
func (plugin) Aliases() []string { return nil }
func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) { ... }

func init() {
    dns.Register(plugin{})
    // 只有实现 Verify 时才声明
    // dns.MarkVerifiable("<id>")
}
```

2. 实现 `Present` / `CleanUp`，必须：

- 用 `dnsutil.TXTValue(keyAuth)` 计算 TXT 值；
- 用 `dnsutil.ChallengeFQDN` / `ZoneName` / `RelativeName` 处理域名；
- 清理时只删除本次值，保留其他 TXT；
- 不存在时视为成功。

3. 创建 `catalog/dns/<id>.json`：

```json
{
  "value": "<id>",
  "label_key": "meta.dns.<id>.label",
  "desc_key": "meta.dns.<id>.desc",
  "fields": [
    {
      "key": "token",
      "label_key": "meta.field.token.label",
      "type": "password",
      "required": true,
      "env": "<LEGO_ENV_NAME>"
    }
  ]
}
```

4. 创建 `i18n/providers/dns/<id>/zh-CN.json` 与 `en-US.json`。
5. 添加 `provider_test.go`，使用 `httptest` 模拟厂商 API。
6. 运行 `make manifest` 和 `go test ./dns/native/providers/<id>`。
7. 运行 `./scripts/check_native.sh`。

`make manifest` 会自动把 `dns/groups/all_native/all.go` 指向你的 native 实现。

### 3.2 lego v4 包装

`dns/providers/<id>` 由 `scripts/gen_dns_plugins` 生成。通常只在升级 lego 或新增上游 provider 时改动：

```bash
go run scripts/gen_dns_plugins/main.go
make manifest
```

不要手改生成文件；需要调整别名或配置时改生成器或 catalog。

### 3.3 lego v5

```bash
make gen-v5
make manifest
```

生成器会读取 lego v5 源码，重写 `dnsv5/providers`、`dnsv5/legacy`、`dnsv5/groups`、`dnsv5/meta`。v5 新增 provider 的字段来自上游 `.toml`；如果 catalog 已有同名条目则沿用。

## 4. 新增部署目标

1. 创建 `deploy/providers/<id>/plugin.go`，实现 `deploy.Deployer`：

```go
type plugin struct{}

func (plugin) ID() string        { return "<id>" }
func (plugin) Aliases() []string { return nil }
func (plugin) Deploy(ctx context.Context, req deploy.Request) error { ... }

func init() { deploy.Register(plugin{}) }
```

2. 如需只读验证/预匹配/动态选项，实现 `deploy.Verifier` / `deploy.Planner` / `deploy.OptionLister`，并在 `init` 中调用 `deploy.MarkVerifiable` / `deploy.MarkPlannable`。
3. 创建 `catalog/deploy/<id>.json`，包含 `fields` 与 `deploy_fields`。
4. 创建 `i18n/providers/deploy/<id>/zh-CN.json` 与 `en-US.json`。
5. 运行 `make deployimports`（或 `make manifest`），生成 `cmd/deploy-helper/imports_gen.go`。
6. 添加 `httptest` 单测，覆盖成功、鉴权失败、只读验证不写入。

用户可控 URL 必须使用 `outbound.ValidateURL` / `outbound.NewClient`。`Verify` / `Plan` / `ListOptions` 不得写证书或修改目标系统。

## 5. 新增通知渠道

在 `notify/providers/channels/` 下新增 `channel_<id>.go`：

```go
package channels

func init() {
    registerChannel(newChannel("<id>", []notify.Field{
        text("url", "notifications.fieldServerUrl", true),
        password("token", "notifications.fieldToken", true),
    }, sendMyChannel))
}

func sendMyChannel(cfg map[string]any, msg notify.Message) error { ... }
```

- 字段构造器：`text`、`password`、`number`、`selectField` + `opt`、`textarea`；
- 发送逻辑：优先使用 `notifyJSON` / `notifyForm` / `notifyHTTP` 与 `outbound`；
- 文案 key 加到 `i18n/core/zh-CN.json` 与 `en-US.json`；
- 不需要修改共享列表，`init` 会自动注册。

## 6. 元数据与文案

- 单 provider 元数据放 `catalog/<kind>/<id>.json`；
- 单 provider 文案放 `i18n/providers/<kind>/<id>/<locale>.json`；
- 公共文案放 `i18n/core/<locale>.json`；
- `label_key` / `desc_key` / `placeholder_key` / `help_key` 指向 i18n key；
- 缺失翻译会回退到默认语言，再回退到 `label` / `value`；
- 新增语言只需增加 `i18n/core/<locale>.json`，生成器会自动合并。

## 7. 生成物与 bot

以下文件由生成器维护：

```text
catalog/data.json
i18n/locales/*.json
cmd/dns-helper/group_*.go
cmd/deploy-helper/imports_gen.go
dns/groups/**
dnsv5/**
plugins.json
```

PR 上会运行 `Update generated files`：

- 同仓库分支：bot 自动执行 `make manifest` 并把生成物提交回 PR；
- fork 分支：bot 无法回写，会提示贡献者本地运行 `make manifest` 并提交。

合并到 `main` 后，`Refresh generated files on main` 会再次刷新生成物，消除 merge queue 并行合并造成的版本/顺序竞争。

> 建议配置仓库 secret `BOT_TOKEN`（fine-grained PAT，`contents:write`）。配置后 bot 提交会触发新的 CI；未配置时使用 `GITHUB_TOKEN`，bot 提交不会触发 workflow，最终检查由 merge queue 重跑。

## 8. 版本与发布

- `all-lego` / `all-lego-v5` 版本跟随 `go.mod` 中的 lego 版本；
- `all-native`、`deploy/all`、`notify/all` 的补丁号由内容指纹自动递增；
- 公共输入（`protocol/`、`sdk/`、`i18n/`、`outbound/`、`catalog/`、`go.mod`）变化会触发所有相关包升版，避免“同版本不同内容”；
- 发布由 maintainer 通过 tag 或手动 workflow 触发，贡献者不需要改版本号。

## 9. CI 分层

| 层级 | 触发 | 内容 |
|---|---|---|
| lint | 每个 PR / merge queue | gofmt、go.mod/go.sum 检查 |
| generated | 每个 PR / merge queue | `make manifest` 并校验生成物 |
| test | Go 变更 | `go vet ./...`、`go test ./...` |
| bundle | DNS/deploy/notify 变更 | 构建 5 个 helper、manifest 数量/一致性、打包 smoke |
| native | DNS/SDK 变更 | `check_native.sh` 体积/依赖/manifest 检查 |
| nightly | 每天 | `go test -race`、全量构建、govulncheck |

分支保护只需要求 **CI gate** 一个检查；条件 job 被跳过时 gate 仍会通过。

## 10. Merge Queue

仓库设置建议：

1. `main` 开启 branch protection；
2. Require status checks：`CI gate`；
3. Require linear history；
4. 开启 Merge Queue，最大并发 5，等待超时 5 分钟；
5. 禁止直接 push `main`；
6. 启用 “Require branches to be up to date before merging”。

PR 流程：

```text
PR -> lint/generated/test/bundle/native -> CODEOWNER review
   -> merge queue -> CI gate -> 合并
   -> after-merge bot 刷新生成物
```

## 11. Review 与所有权

`.github/CODEOWNERS` 定义默认 owner。建议按团队替换：

- `@AXmishell/dns-native`：`dns/native/`
- `@AXmishell/lego`：`dns/providers/`、`dnsv5/`
- `@AXmishell/deploy`：`deploy/`
- `@AXmishell/notify`：`notify/`
- `@AXmishell/infra`：`.github/`、`Makefile`、`scripts/`、`go.mod`

审批要求：

- 单 provider / 渠道 / 部署目标：1 名 area owner；
- protocol / sdk / catalog schema / CI / release：2 名 core；
- 涉及凭据、证书、SSRF、签名：至少 1 名安全 owner。

## 12. 安全

- 不要在 PR、测试、日志中提交真实 token；
- 用户可控 URL 使用 `outbound`；
- 错误信息不要包含 secret；
- 私有漏洞请通过 `SECURITY.md` 中的渠道报告；
- 发布签名密钥只放在受保护的 GitHub Environment。

## 13. 提交前检查

```text
[ ] 只改自己的 provider 目录 + 对应 catalog/i18n
[ ] init 注册且 ID/alias 唯一
[ ] catalog 字段 key 与代码一致
[ ] 中英文文案都补齐
[ ] 单测使用 httptest，不访问真实网络
[ ] make manifest
[ ] gofmt / go vet / go test
[ ] check_native（native）
[ ] 生成物已提交或等待 bot 回写
```
