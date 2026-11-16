# LACTSSL-Plugins

LACTSSL 官方插件仓库：DNS、证书部署与通知插件的源码、合并包构建工具、包格式与仓库索引。

主仓库 [LACTSSL](https://github.com/AXmishell/LACTSSL) 只保留插件宿主，通过 `go.mod` 依赖本仓库的 `protocol` 与 `catalog`，并在运行期从本仓库的插件索引按需下载按类型合并的插件包。

## 目录

```text
protocol/        插件协议 v1 的线格式（零第三方依赖）
sdk/helper/      helper 启动框架：内存预算、JSON、错误退出
sdk/httpapi/     自研 provider 共用的最小 HTTP 客户端
sdk/{sigv4,huawei,gcp,azure,edgegrid,dnsutil}/  自研签名/认证/域名工具
catalog/         官方服务商 / 部署目标元数据（data.json）
i18n/            插件侧精简文案
outbound/        SSRF 防护出站 HTTP 客户端
dns/             194 个 lego 服务商包装 + 注册表
  providers/<id>/   原有 lego 包装（保留）
  native/providers/<id>/  自研实现（heavy 10 家已迁移）
  groups/           合并包的 provider 分组
deploy/          7 个部署目标
  providers/{ftp,webhook,aliyuncdn,qiniu,panels}
notify/          23 个通知渠道
  providers/channels
cmd/
  dns-helper/    DNS 合并包入口（194 个服务商）
  deploy-helper/ 部署合并包入口（7 个目标）
  notify-helper/ 通知合并包入口（23 个渠道）
plugins.json     合并包清单（kind/id/version/tag/providers，6 个包）
testdata/        迁移前 bundle manifest 基线
scripts/pluginctl 生成、构建、打包、索引、签名工具
```

## 合并包构建

DNS 常用/重型两个包共用一个入口、通过构建标签选择服务商；部署与通知各一个入口：

```bash
go build -trimpath -tags "bundle_dns_core,nethttpomithttp2"  -o lactssl-dns-core  ./cmd/dns-helper
go build -trimpath -tags "bundle_dns_heavy,nethttpomithttp2" -o lactssl-dns-heavy ./cmd/dns-helper
go build -trimpath -tags "bundle_dns_heavy_native,nethttpomithttp2" -o lactssl-dns-heavy-native ./cmd/dns-helper
go build -trimpath -tags "bundle_dns_core_native,nethttpomithttp2" -o lactssl-dns-core-native ./cmd/dns-helper
go build -trimpath -tags nethttpomithttp2 -o lactssl-deploy-all ./cmd/deploy-helper
go build -trimpath -tags nethttpomithttp2 -o lactssl-notify-all ./cmd/notify-helper
```

`heavy-native` 是 10 个大型云厂商的自研实现，不链接 lego / AWS / Azure / GCP / 华为云等 SDK；它与 `heavy` 使用相同的 provider ID，因此二者只能安装一个（索引中以 `variant_of` / `conflicts_with` 标注）。

`core-native` 是与 `core` 相同的 184 个 provider 的增量变体：已迁移 provider 使用 `dns/native`，其余暂时继续使用原 lego 包装；每迁移一家，包体积继续下降。

`manifest` 输出该类全部条目；`pluginctl` 会校验 manifest 与 `plugins.json` 的 `providers` 集合完全一致。

## 打包与索引

```bash
# 生成 plugins.json（六个合并包）
make gen

# 构建 DNS 常用包的单个平台包
make plugin KIND=dns ID=core GOOS=linux GOARCH=amd64
# 输出 dist/lactssl-dns-core_1.0.0_linux_amd64.tar.gz 与 .meta.json

# 从全部 meta 生成索引（资产放在 plugin-assets-<kind> Release 中）
make index BASE_URL=https://github.com/AXmishell/LACTSSL-Plugins/releases/download BUCKET=plugin-assets
```

包格式：

```text
tar.gz
├── plugin.json
├── lactssl-<kind>-<id>[.exe]
└── LICENSE
```

索引包含每个合并包的版本、协议、最低宿主版本、providers 列表、各平台资产 URL、sha256、压缩大小与 `uncompressed_size`（解压后实际磁盘占用）。官方索引可用 Ed25519 签名，签名文件为 `index.json.sig`。

体积对比（linux/amd64，解压后）：

| 包 | 解压后 | manifest 峰值内存 |
|---|---:|---:|
| DNS 常用（184 个服务商） | 约 21 MiB | 约 22 MiB |
| DNS 常用·自研增量（184 个服务商） | **约 17.8 MiB** | 约 20 MiB |
| DNS 大型云厂商（10 个服务商） | 约 39 MiB | 约 26 MiB |
| DNS 大型云厂商·自研（10 个服务商） | **约 7.8 MiB** | 约 10 MiB |
| 部署（7 个目标） | 约 7 MiB | 约 10 MiB |
| 通知（23 个渠道） | 约 7 MiB | 约 10 MiB |
| 旧布局合计 | 约 73 MiB | — |
| 使用 core-native + heavy-native 的合计 | 约 40 MiB | — |

旧的单服务商打包约为 1.7 GiB（224 个包），合并后降到约 4%。DNS 拆成常用/重型两个包，是为了让绝大多数用户只安装约 21 MiB、运行内存约 22 MiB；两个包合计仍只比单个全量包多约 2 MiB。

`heavy-native` 的 10 家 provider 全部为仓库自研实现，链接图只剩 `miekg/dns`（RFC 2136 协议库）与 Go 标准库。`core-native` 目前已有 11 家迁移到自研实现（阿里云 DNS/ESA、Vultr、Linode、NS1、DNSimple、Exoscale、Scaleway、Azion、OVH、火山引擎），其余继续使用 lego。`scripts/check_native.sh` 会校验体积预算、已迁移 SDK 不回归、manifest 兼容性。

## 发布

`.github/workflows/release.yml`：

- 固定只维护 4 个 Release，不再按插件/版本各建一个：
  - `plugin-index`：只放 `index.json`（与 `index.json.sig`）。
  - `plugin-assets-<kind>`：放该类别合并包的 `tar.gz`，资产名自带版本与平台。
- 增量构建：对比已发布 `index.json` 与 `plugins.json` 的版本号，只构建变化的合并包（`workflow_dispatch` 可勾选 `force_all` 强制全量）。
- 每个合并包 job 构建 4 个平台：linux/amd64、linux/arm64、darwin/arm64、windows/amd64。
- 索引生成时合并上一次索引，未重建的合并包条目原样沿用；发布索引后删除被覆盖的旧版本资产，使每个 bucket 的资产数稳定在 `4 × 合并包数`（GitHub 单个 Release 上限 1000 个资产）。
- 所有插件 Release 都带 `--latest=false`，避免插件产物抢占仓库的 “Latest” 标记。
- 如配置 `PLUGIN_INDEX_SIGNING_KEY`（base64 Ed25519 私钥），同时发布 `index.json.sig`。

## 兼容性

- 协议保持 v1；旧版单服务商包与旧全量 helper 会由新合并包在安装时自动替换。
- `testdata/` 中的 manifest 基线用于验证合并包与旧 bundle 的字段一致。
- `plugins.json` 中每个合并包独立 SemVer；升级插件时修改对应版本号并重新构建。
- `heavy` 与 `heavy-native` 是同一 provider 集合的两个实现变体，provider ID、字段、别名完全一致；同一宿主只能启用一个，切换时账户与任务无需迁移。
- `heavy-native` 的 `rfc2136` 不支持 GSS-TSIG（会明确报错）；需要 GSS-TSIG 时继续使用原 `heavy` 包。

## 自研 SDK 化

方案与后续增量计划见 `docs/native-sdk-plan.md`，10 家 provider 的实现细节与限制见 `docs/native-providers.md`。当前已落地 P0 + P1：

```bash
make bundle-native     # 构建 lactssl-dns-heavy-native
make check-native      # 体积、依赖白名单、manifest 对齐检查
```
