# LACTSSL-Plugins

LACTSSL 官方插件仓库：DNS、证书部署与通知插件的源码、单插件构建工具、包格式与仓库索引。

主仓库 [LACTSSL](https://github.com/AXmishell/LACTSSL) 只保留插件宿主，通过 `go.mod` 依赖本仓库的 `protocol` 与 `catalog`，并在运行期从本仓库的插件索引按需下载单个插件。

## 目录

```text
protocol/        插件协议 v1 的线格式（零第三方依赖）
sdk/helper/      helper 启动框架：内存预算、JSON、错误退出
catalog/         官方服务商 / 部署目标元数据（data.json）
i18n/            插件侧精简文案
outbound/        SSRF 防护出站 HTTP 客户端
dns/             194 个 lego 服务商包装 + 注册表
  providers/<id>/
deploy/          7 个部署目标
  providers/{ftp,webhook,aliyuncdn,qiniu,panels}
notify/          23 个通知渠道
  providers/channels
cmd/
  dns-plugin/    单服务商入口 + select_<id>.go（build tag）
  deploy-plugin/ 单部署目标入口
  notify-plugin/ 单通知渠道入口
  *-helper/      旧版 bundle helper（兼容/测试，不再发布）
plugins.json     单插件清单（kind/id/version/tag）
testdata/        迁移前 bundle manifest 基线
scripts/pluginctl 生成、构建、打包、索引、签名工具
```

## 单插件构建

每个插件通过构建标签只注册一个条目：

```bash
go build -tags plugin_dns_cloudflare   -o lactssl-dns-cloudflare   ./cmd/dns-plugin
go build -tags plugin_deploy_ftp       -o lactssl-deploy-ftp       ./cmd/deploy-plugin
go build -tags plugin_notify_telegram  -o lactssl-notify-telegram  ./cmd/notify-plugin
```

`manifest` 会校验注册表恰好包含一个条目，否则直接失败。

## 打包与索引

```bash
# 生成 plugins.json 与 224 个 select_*.go
make gen

# 构建单个插件的单个平台包
make plugin KIND=dns ID=cloudflare GOOS=linux GOARCH=amd64
# 输出 dist/lactssl-dns-cloudflare_1.0.0_linux_amd64.tar.gz 与 .meta.json

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

索引包含每个插件的版本、协议、最低宿主版本、各平台资产 URL、sha256 与大小。官方索引可用 Ed25519 签名，签名文件为 `index.json.sig`。

## 发布

`.github/workflows/release.yml`：

- 固定只维护 4 个 Release，不再按插件/版本各建一个：
  - `plugin-index`：只放 `index.json`（与 `index.json.sig`）。
  - `plugin-assets-<kind>`：放该类别全部插件的 `tar.gz`，资产名自带版本与平台。
- 增量构建：对比已发布 `index.json` 与 `plugins.json` 的版本号，只构建变化的插件（`workflow_dispatch` 可勾选 `force_all` 强制全量）。
- 每个插件 job 构建 4 个平台：linux/amd64、linux/arm64、darwin/arm64、windows/amd64。
- 索引生成时合并上一次索引，未重建的插件条目原样沿用；发布索引后删除被覆盖的旧版本资产，使每个 bucket 的资产数稳定在 `4 × 插件数`（GitHub 单个 Release 上限 1000 个资产）。
- 所有插件 Release 都带 `--latest=false`，避免插件产物抢占仓库的 “Latest” 标记。
- 如配置 `PLUGIN_INDEX_SIGNING_KEY`（base64 Ed25519 私钥），同时发布 `index.json.sig`。

## 兼容性

- 协议保持 v1；旧版 bundle helper 仍保留源码用于回归测试，但不再作为发布产物。
- `testdata/` 中的 manifest 基线用于验证单插件与旧 bundle 的字段一致。
- `plugins.json` 中每个插件独立 SemVer；升级插件时修改对应版本号并重新构建。
