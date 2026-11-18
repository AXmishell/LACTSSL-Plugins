# LACTSSL-Plugins

LACTSSL 官方插件仓库，为 [LACTSSL](https://github.com/AXmishell/LACTSSL) 证书管理工具提供 DNS 验证、证书部署与通知推送能力，同时维护插件协议、服务商元数据、合并包格式与签名索引。

LACTSSL 主仓库只保留插件宿主：宿主内实现插件协议 v1 的 manifest 解码，在运行期从插件索引下载对应类型的合并包，并以子进程方式调用插件。两个仓库通过签名索引、协议版本与 `min_host` 兼容，构建期不共享 Go 模块。

## 插件类型

| 类型 | 数量 | 内容 |
|---|---:|---|
| DNS | 194 个服务商 | ACME DNS-01 的 TXT 记录创建与清理，覆盖主流云厂商、国际域名商与自建 DNS |
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
  providers/     194 个 lego 服务商包装与注册表
  native/        仓库内重构的 21 个 DNS 服务商实现
  groups/        合并包的 provider 分组
deploy/          证书部署目标实现与注册表
notify/          通知渠道实现与注册表
cmd/             dns-helper、deploy-helper、notify-helper 三个合并包入口
assets/icons/    服务商与部署目标图标
plugins.json     合并包清单（kind、id、版本、构建标签、providers）
```

## 插件协议

插件以一次性子进程运行，主程序通过环境变量传入协议版本与常驻内存预算，双方以 JSON 交换数据：

- 子命令按类型划分：`manifest` 通用，DNS 使用 `present` / `cleanup`，部署使用 `deploy`，通知使用 `send`。
- `manifest` 输出插件自描述：版本、协议、服务商或渠道 ID、显示名称、别名与配置字段。
- 请求与响应经 stdin/stdout 传递，凭据、任务参数与证书材料使用同一套结构。
- 字段支持明文/密文、必填、默认值、条件显示与多语言文案；部署目标区分认证字段（`fields`）与任务字段（`deploy_fields`）。

协议保持 v1，新能力通过可选字段与特性标记扩展，旧版宿主会忽略无法识别的字段。

## 合并插件包

同一类型的所有服务商合并为一个可执行文件，共享一份 Go 运行时，避免为每个服务商单独打包：

- DNS 发布一个 `all-native` 包，覆盖全部 194 个服务商；其中 21 家使用 `dns/native` 的仓库实现，其余继续使用 lego 包装。
- 部署与通知各发布一个包，分别包含全部规范部署目标与通知渠道。
- 包内包含 `plugin.json`、平台可执行文件、可选的 `icons/` 与 `LICENSE`。
- 索引声明版本、协议、最低宿主版本、服务商列表、各平台资产、sha256 与解压后大小，并内联图标供插件市场展示；官方索引可使用 Ed25519 签名。

合并包在安装时自动替换同类的旧单服务商包；DNS 服务商的 ID、字段与凭据无需迁移。

## 版本管理

`plugins.json` 中的版本由 `make manifest` 自动维护，不需要手工修改补丁号：

- 主/次版本在 `scripts/pluginctl/main.go` 的 `dnsBaseVer`、`deployBaseVer`、`notifyBaseVer` 中调整；修改后该包从 `<主>.<次>.0` 重新计数。
- 补丁号由各合并包输入文件的内容指纹决定：相关源码、共享 SDK、元数据、图标或构建脚本变化时自动 +1，内容未变化则保持不变。
- DNS、部署、通知三个包在非 tag 构建时独立计数；`sdk/`、`catalog/` 等共享目录变化会同时升级三个包。
- 修改代码后运行 `make manifest`，并把更新后的 `plugins.json` 一起提交；CI 与发布流程会校验是否遗漏。

发布时以 git tag 为准：`v1.3.4` 或 `plugins-v1.3.4` 会让发布流程先以 `manifest -version 1.3.4` 生成临时清单，三个合并包的资产名、索引、包内 `plugin.json` 与二进制 manifest 全部使用 `1.3.4`，并全量重建。仓库里的 `plugins.json` 保持按内容指纹维护的内部版本，不被 CI 改写，仅用于非 tag 的手动构建。

`plugins.json` 里的 `source_hash` 只用于版本计算，不会写入插件索引。

## 兼容性约定

- DNS 服务商 ID、字段 key、别名与旧插件保持逐项一致。
- 部署目标对外使用规范 ID，历史 ID 通过别名与迁移映射落到规范实现，并将旧配置合并进新目标。
- 已迁移的 DNS 服务商不再链接 lego 包装或上游云 SDK，签名与协议逻辑由本仓库的 `sdk/` 实现。
- `all-native` 的 `rfc2136` 不支持 GSS-TSIG，需要该能力时可使用不带合并标签的本地全量构建。

## 文档

- [SDK 重构方案](docs/native-sdk-plan.md)
- [Native DNS 服务商实现说明](docs/native-providers.md)
- [迁移映射](docs/migration-plan.md)

## 许可证

[MIT](LICENSE)
