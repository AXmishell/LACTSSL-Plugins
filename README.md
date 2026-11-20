# LACTSSL-Plugins

LACTSSL 官方插件仓库，为 [LACTSSL](https://github.com/AXmishell/LACTSSL) 证书管理工具提供 DNS 验证、证书部署与通知推送能力，同时维护插件协议、服务商元数据、合并包格式与签名索引。

LACTSSL 主仓库只保留插件宿主：宿主内实现插件协议 v1 的 manifest 解码，在运行期从插件索引下载对应类型的合并包，并以子进程方式调用插件。两个仓库通过签名索引、协议版本与 `min_host` 兼容，构建期不共享 Go 模块。

## 插件类型

| 类型 | 数量 | 内容 |
|---|---:|---|
| DNS | 194 个（v4）/ 228 个（v5） | ACME DNS-01 的 TXT 记录创建与清理，覆盖主流云厂商、国际域名商与自建 DNS |
| 部署 | 25 个规范目标 | 面板与网站、云 CDN、对象存储、WAF、邮件与 Webhook 等证书下发场景 |
| 通知 | 23 个渠道 | Telegram、钉钉、企业微信、飞书、Bark、Gotify、ntfy、PushDeer、Server 酱等 |

部署目标包含宝塔、1Panel、Nginx Proxy Manager、AcePanel、CDNfly、Lucky、Proxmox、群晖、Plesk 等自建服务，以及阿里云、腾讯云、华为云、火山引擎、百度智能云、UCloud、又拍云、多吉云、雨云等云服务；其中部分目标从 certd 与 ALLinSSL 移植并按本仓库协议重构。历史 ID（如 `aliyun_cdn`、`tencent_cos`、`qiniu_kodo`）通过别名映射到规范目标。

## 项目结构

```text
protocol/        插件协议 v1 的线格式（零第三方依赖，插件侧实现）
catalog/         插件内部的服务商与部署目标元数据（data.json，不对外提供 Go API）
i18n/            插件侧精简文案与语言选择
sdk/             插件共用的实现：HTTP、签名、DNS 工具、helper 框架等
outbound/        带 SSRF 防护的出站 HTTP 客户端
dns/
  providers/     194 个 lego v4 服务商包装与注册表
  native/        仓库内重构的 21 个 DNS 服务商实现
  groups/        合并包的 provider 分组
dnsv5/
  providers/     228 个 lego v5 服务商包装
  legacy/        v5 已移除 ID 的兼容占位
  meta/          v5 全量 provider 的补充元数据
deploy/          证书部署目标实现与注册表
notify/          通知渠道实现与注册表
cmd/             dns-helper、dns-helper-v5、deploy-helper、notify-helper 入口
assets/icons/    服务商与部署目标图标
plugins.json     合并包清单（kind、id、版本、构建标签、providers）
```

## 插件协议

插件以一次性子进程运行，主程序通过环境变量传入协议版本与常驻内存预算，双方以 JSON 交换数据：

- 子命令按类型划分：`manifest` 通用，DNS 使用 `present` / `cleanup`，部署使用 `deploy` / `options`，通知使用 `send`。
- `manifest` 输出插件自描述：版本、协议、服务商或渠道 ID、显示名称、别名与配置字段。
- 请求与响应经 stdin/stdout 传递，凭据、任务参数与证书材料使用同一套结构。
- 字段支持明文/密文、必填、默认值、条件显示与多语言文案；部署目标区分认证字段（`fields`）与任务字段（`deploy_fields`）。
- 部署字段可声明 `options_source` 与 `multiple`，由宿主调用 `options` 从远端读取可选项，例如宝塔面板的网站与 Docker 网站列表。

协议保持 v1，新能力通过可选字段与特性标记扩展，旧版宿主会忽略无法识别的字段。

## 合并插件包

同一类型的所有服务商合并为一个可执行文件，共享一份 Go 运行时，避免为每个服务商单独打包：

- DNS 发布三个变体：`all-lego` 覆盖 194 个服务商，全部使用 lego v4 官方实现；`all-native` 中 21 家使用 `dns/native` 的仓库实现，其余继续使用 lego v4 包装；`all-lego-v5` 使用 lego v5.6.0，覆盖 228 个上游 provider，并额外保留 7 个 v5 已移除 ID 的兼容占位；linux/amd64 实测约 37 MiB（压缩包约 13 MiB）。
- `all-lego` 与 `all-native` 共享同一批 194 个 ID；`all-lego-v5` 向前兼容这 194 个 ID，并新增 41 个 v5 provider。三个包不能同时启用，`conflicts_with` 已声明互斥关系，`all-native` 和 `all-lego-v5` 通过 `variant_of` 指向 `all-lego`。
- 部署与通知各发布一个包，分别包含全部规范部署目标与通知渠道。
- 包内包含 `plugin.json`、平台可执行文件、可选的 `icons/` 与 `LICENSE`。
- 索引声明版本、协议、最低宿主版本、服务商列表、各平台资产、sha256 与解压后大小，并内联图标供插件市场展示；官方索引可使用 Ed25519 签名。

合并包在安装时自动替换同类的旧单服务商包；DNS 服务商的 ID、字段与凭据无需迁移。

## 版本管理

`plugins.json` 中的版本由 `make manifest` 自动维护，不需要手工修改补丁号：

- 主/次版本在 `scripts/pluginctl/main.go` 的 `dnsBaseVer`、`deployBaseVer`、`notifyBaseVer` 中调整；修改后该包从 `<主>.<次>.0` 重新计数。
- 补丁号由各合并包**自身**输入文件的内容指纹决定：只有该包的 `cmd/<kind>-helper/`、`<kind>/`、`assets/icons/<kind>/`，以及公共的 `go.mod`/`go.sum`、`LICENSE`、`assets/icons/icons.json` 变化时才 +1；内容未变化则保持不变。
- DNS native、部署、通知三个包版本互相独立：`protocol/`、`sdk/`、`i18n/`、`outbound/`、`catalog/` 等共享源码目录的改动不会让三个包同时升版。
- `all-lego` / `all-lego-v5` 例外：版本号分别直接跟随 `go.mod` 中 `github.com/go-acme/lego/v4` 与 `/v5` 的版本（`v4.35.2` → `4.35.2`，`v5.6.0` → `5.6.0`），不参与补丁递增，也不会被发布 tag 覆盖；升级 lego 后重新运行 `make manifest` 即可。仅包装层变化而 lego 未升级时版本不变，非 tag 发布需使用 `force_all` 或打 tag 重建。
- 修改代码后运行 `make manifest`（历史名称 `make gen`，两者等价），并把更新后的 `plugins.json`、`cmd/dns-helper`、`cmd/dns-helper-v5`、`dns/groups`、`dnsv5` 一起提交；CI 会校验是否遗漏，失败时直接打印差异。

发布时以 git tag 为准：`v1.3.4` 或 `plugins-v1.3.4` 会让发布流程先以 `manifest -version 1.3.4` 生成临时清单，DNS native、部署与通知包的资产名、索引、包内 `plugin.json` 与二进制 manifest 使用 `1.3.4`，并全量重建；`all-lego` / `all-lego-v5` 仍使用各自 go.mod 中的 lego 版本。仓库里的 `plugins.json` 保持按内容指纹维护的内部版本，不被 CI 改写，仅用于非 tag 的手动构建。

`plugins.json` 里的 `source_hash` 只用于版本计算，不会写入插件索引。

需要显式重置版本（例如把内部版本从 `1.3.x` 降到 `1.0.0`）时，先修改 `dnsBaseVer`/`deployBaseVer`/`notifyBaseVer` 并清除对应条目的 `source_hash`，重新生成 `plugins.json`；手动触发 Release 时勾选 `allow_downgrade`，否则索引合并会保留更高的旧版本。tag 发布仍会覆盖内部版本，两者不要混用。

## 兼容性约定

- DNS 服务商 ID、字段 key、别名与旧插件保持逐项一致。
- 部署目标对外使用规范 ID，历史 ID 通过别名与迁移映射落到规范实现，并将旧配置合并进新目标。
- 已迁移的 DNS 服务商不再链接 lego 包装或上游云 SDK，签名与协议逻辑由本仓库的 `sdk/` 实现。
- `all-native` 的 `rfc2136` 不支持 GSS-TSIG，需要该能力时请安装 `all-lego`。
- `all-lego-v5` 沿用旧 ID：v5 的 `dnsupdate` 以 `rfc2136` 发布，`webnamesru` 以 `webnames` 发布；`dnspod`、`azure`、`brandit`、`cloudxns`、`googledomains`、`iij`、`iwantmyname` 在 v5 已移除，包内保留同名占位并在调用时返回迁移提示。需要继续使用这些实现时请留在 `all-lego` / `all-native`。

## 文档

- [SDK 重构方案](docs/native-sdk-plan.md)
- [Native DNS 服务商实现说明](docs/native-providers.md)
- [lego v5 独立 Helper](docs/lego-v5.md)
- [迁移映射](docs/migration-plan.md)

## 许可证

[MIT](LICENSE)
