# LACTSSL-Plugins

LACTSSL 官方插件仓库：DNS、证书部署与通知插件的源码、合并包构建工具、包格式与仓库索引。

主仓库 [LACTSSL](https://github.com/AXmishell/LACTSSL) 只保留插件宿主，通过 `go.mod` 依赖本仓库的 `protocol` 与 `catalog`，并在运行期从本仓库的插件索引按需下载按类型合并的插件包。

## 目录

```text
protocol/        插件协议 v1 的线格式（零第三方依赖）
sdk/helper/      helper 启动框架：内存预算、JSON、错误退出
sdk/httpapi/     重构 provider 共用的最小 HTTP 客户端
sdk/{sigv4,huawei,gcp,azure,edgegrid,dnsutil}/  重构签名/认证/域名工具
catalog/         官方服务商 / 部署目标元数据（data.json）
i18n/            插件侧精简文案
outbound/        SSRF 防护出站 HTTP 客户端
dns/             194 个 lego 服务商包装 + 注册表
  providers/<id>/   原有 lego 包装（保留）
  native/providers/<id>/  重构实现（heavy 10 家已迁移）
  groups/           合并包的 provider 分组
deploy/          37 个部署目标
  providers/{ftp,webhook,qiniu,aliyuncdn,aliyunoss,aliyuncloud,tencent,huawei,volcengine,ucloud,upyun,doge,baidu,btwaf,lecdn,rainyun,smtp,npm,acepanel,cdnfly,lucky,proxmox,synology,plesk,panels}
notify/          23 个通知渠道
  providers/channels
cmd/
  dns-helper/    DNS 合并包入口（194 个服务商）
  deploy-helper/ 部署合并包入口（37 个目标）
  notify-helper/ 通知合并包入口（23 个渠道）
plugins.json     合并包清单（kind/id/version/tag/providers，6 个包）
assets/icons/    服务商图标（按 kind 存放，icons.json 声明别名）
testdata/        迁移前 bundle manifest 基线
scripts/pluginctl 生成、构建、打包、索引、签名工具
```

## 合并包构建

DNS 常用/重型两个包共用一个入口、通过构建标签选择服务商；重构实现把两者合并为一个 `all-native` 包；部署与通知各一个入口：

```bash
go build -trimpath -tags "bundle_dns_core,nethttpomithttp2"  -o lactssl-dns-core  ./cmd/dns-helper
go build -trimpath -tags "bundle_dns_heavy,nethttpomithttp2" -o lactssl-dns-heavy ./cmd/dns-helper
go build -trimpath -tags "bundle_dns_all_native,nethttpomithttp2" -o lactssl-dns-all-native ./cmd/dns-helper
go build -trimpath -tags nethttpomithttp2 -o lactssl-deploy-all ./cmd/deploy-helper
go build -trimpath -tags nethttpomithttp2 -o lactssl-notify-all ./cmd/notify-helper
```

`all-native` 覆盖原 `core` 与 `heavy` 的全部 194 个 provider，把原来两个重构包合并为一次构建：已迁移 provider 使用 `dns/native`，其余暂时继续使用原 lego 包装；每迁移一家，包体积继续下降。它与 `core`、`heavy` 使用相同的 provider ID，因此与二者互斥（索引中以 `conflicts_with` 标注）。

`all-native` 在清单中声明 `hide_icon: true`：插件市场不展示服务商 logo，避免把某一家的图标当作整个全量包的门面；`heavy` 仍使用首个 provider 的图标。

`manifest` 输出该类全部条目；`pluginctl` 会校验 manifest 与 `plugins.json` 的 `providers` 集合完全一致。

## 打包与索引

```bash
# 生成 plugins.json（五个合并包）
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
├── icons/                      # 可选：该包覆盖的 provider 图标
│   └── <provider>.svg|png|webp
└── LICENSE
```

图标源文件在 `assets/icons/<kind>/`，`assets/icons/icons.json` 声明历史 ID 的复用关系（如 `baota_site → baota`）。`pluginctl build` 会按包的 `providers` 收集图标，写入 `plugin.json.icons` 与 `icons/`；`pluginctl index` 把图标内联到索引根级 `icons`，供主程序在插件安装前展示市场条目。单张图标解码后不超过 64 KB，只允许 svg / png / webp。

索引包含每个合并包的版本、协议、最低宿主版本、providers 列表、各平台资产 URL、sha256、压缩大小与 `uncompressed_size`（解压后实际磁盘占用）。官方索引可用 Ed25519 签名，签名文件为 `index.json.sig`。

体积对比（linux/amd64，解压后）：

| 包 | 解压后 | manifest 峰值内存 |
|---|---:|---:|
| DNS 常用（184 个服务商） | 约 21 MiB | 约 22 MiB |
| DNS 大型云厂商（10 个服务商） | 约 39 MiB | 约 26 MiB |
| DNS 全量·重构（194 个服务商） | **约 18.1 MiB** | 约 20 MiB |
| 部署（37 个目标） | 约 7.8 MiB | 约 11 MiB |
| 通知（23 个渠道） | 约 7 MiB | 约 10 MiB |
| 旧布局合计 | 约 73 MiB | — |
| 仅安装 DNS 全量·重构 | **约 18.1 MiB** | — |

旧的单服务商打包约为 1.7 GiB（224 个包），合并后降到约 4%。DNS 拆成常用/重型两个包，是为了让绝大多数用户只安装约 21 MiB、运行内存约 22 MiB；选择重构实现时只需安装一个 `all-native` 包，不再需要同时安装常用和大型云厂商两个变体。

`all-native` 的 10 家大型云厂商全部为仓库重构实现，常用包中目前也已有 11 家迁移到重构实现（阿里云 DNS/ESA、Vultr、Linode、NS1、DNSimple、Exoscale、Scaleway、Azion、OVH、火山引擎），其余继续使用 lego。`scripts/check_native.sh` 会校验体积预算、已迁移 provider 不再链接 lego 包装或上游 SDK、以及 194 个 provider 的 manifest 兼容性。

## 从 certd 移植的部署目标

本仓库从 [certd](https://github.com/certd/certd) 的部署插件中移植并重构了 17 个目标，统一使用 LACTSSL 的 `deploy` 协议与 `fields` / `deploy_fields` 模型。

| LACTSSL ID | certd 插件 | 说明 |
|---|---|---|
| `smtp` | 邮件发送证书 | 证书打包为 ZIP 附件，支持 TLS / STARTTLS |
| `nginx_proxy_manager` | Nginx Proxy Manager-部署到主机 | 自定义证书上传与代理主机绑定，支持 TOTP |
| `acepanel` | AcePanel-部署到网站 | 上传证书并批量部署网站 |
| `acepanel_panel` | AcePanel-面板证书 | 替换面板 HTTPS 证书 |
| `cdnfly` | cdnfly-部署证书到cdnfly | 支持 API Key 与账号密码登录 |
| `lucky` | lucky-更新Lucky证书 | 通过 OpenToken 更新已有证书 |
| `doge` | 多吉云-部署到多吉云CDN | 上传证书并绑定 CDN 域名 |
| `tencent_cdn` | 腾讯云-部署到CDN-v2 | 通过 SSL 证书服务部署到 CDN |
| `tencent_cos` | 腾讯云-部署证书到COS | 通过 SSL 证书服务部署到 COS 自定义域名 |
| `huawei_cdn` | 华为云-部署证书至CDN | 更新 CDN 域名 HTTPS 配置 |
| `volcengine_cdn` | 火山引擎-部署证书至CDN | 上传证书并更新加速域名 |
| `aliyun_oss` | 阿里云-部署证书至OSS | 为 OSS 自定义域名绑定证书 |
| `ucloud_cdn` | UCloud-部署到CDN | 上传 USSL 证书并更新 CDN HTTPS 配置 |
| `upyun_cdn` | 又拍云-部署证书到CDN/USS | 上传证书并更新域名 HTTPS 策略 |
| `proxmox` | Proxmox-上传证书到Proxmox | 为 PVE 节点上传自定义证书 |
| `synology` | 群晖-部署证书到群晖面板 | 通过 DSM Web API 更新证书，支持 TOTP / 设备 ID |
| `plesk` | Plesk-部署Plesk网站证书 | 通过 Plesk HTTP API 登录并上传一体化证书 |

重构约定：

- 认证与连接信息放在 `fields`，随凭据加密保存；域名、站点、节点等任务参数放在 `deploy_fields`。
- 站点/主机/证书等选择项统一支持「ID 或域名，逗号分隔」，留空时按证书域名自动匹配，不依赖主程序的下拉组件。
- 腾讯云 TC3、华为云 SDK-HMAC、火山引擎 HMAC、阿里云 OSS V1、UCloud SHA1 签名均为仓库内重构实现，不引入上游云 SDK。
- certd 中基于 SSH 的目标（OpenWrt、QNAP、飞牛 NAS、IIS 等）与「复制到本机」没有重复移植：LACTSSL 内置的 `ssh` 与 `local` 目标已覆盖同类场景；IIS 需要 PFX 编码与 PowerShell 远程执行，暂不在插件侧提供。群晖、Plesk 等有稳定 HTTP API 的目标已改为 API 实现，不依赖 SSH。
- Kubernetes、AWS CloudFront、Fastly 等依赖大型 SDK 或仅上传不绑定的目标暂未纳入部署包，后续可按同一模式增量补充。

## 从 ALLinSSL 移植的部署目标

同时从 [ALLinSSL](https://github.com/allinssl/allinssl) 的 `backend/internal/cert/deploy` 移植并重构了 13 个目标。ALLinSSL 使用 Go 实现，字段与接口逻辑与本仓库更接近；本次保留其 API 语义，但统一改为 LACTSSL 的 `fields` / `deploy_fields` 与错误处理：

| LACTSSL ID | ALLinSSL 目标 | 说明 |
|---|---|---|
| `tencent_waf` | `tencentcloud-waf` | 腾讯云 SSL 证书服务部署到 WAF |
| `tencent_teo` | `tencentcloud-teo` | 腾讯云 SSL 证书服务部署到 EdgeOne |
| `aliyun_dcdn` | `aliyun-dcdn` | 阿里云 DCDN 域名证书 |
| `aliyun_esa` | `aliyun-esa` | 阿里云 ESA 站点证书 |
| `aliyun_waf` | `aliyun-waf` | 阿里云 WAF 域名证书，复用 CAS 上传与 WAF Listen/Redirect |
| `baidu_cdn` | `baidu-cdn` | 百度智能云 CDN 证书上传与绑定 |
| `volcengine_dcdn` | `volcengine-dcdn` | 火山引擎 DCDN 证书上传与绑定 |
| `lecdn` | `lecdn` | LeCDN 登录、证书上传、站点域名绑定 |
| `rainyun` | `rainyun-sslcenter` | 雨云 SSL 证书中心 |
| `btwaf` | `btwaf-site` | 宝塔 WAF 站点证书 |
| `safeline_panel` | `safeline-panel` | 雷池控制台证书 |
| `safeline_portal` | `safeline-portal` | 雷池认证中心证书 |
| `baota_docker` | `btpanel-dockersite` | 宝塔 Docker 网站证书 |

`btpanel-singlesite` 由现有 `baota` 网站部署逻辑直接覆盖。ALLinSSL 的 `btpanel`、`1panel`、`aliyun-cdn`、`aliyun-oss`、`qiniu-*`、`huaweicloud-cdn`、`volcengine-cdn`、`doge-cdn`、`webhook`、`ssh`、`localhost` 与本仓库已有目标重复，未重复注册。

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
- `all-native` 覆盖 `core` 与 `heavy` 的全部 provider，provider ID、字段、别名与原包完全一致；同一宿主只能启用一个 DNS 变体，切换时账户与任务无需迁移。
- `all-native` 的 `rfc2136` 不支持 GSS-TSIG（会明确报错）；需要 GSS-TSIG 时继续使用原 `core` / `heavy` 包。

## SDK 重构

方案与后续增量计划见 `docs/native-sdk-plan.md`，10 家 provider 的实现细节与限制见 `docs/native-providers.md`。当前已落地 P0 + P1：

```bash
make bundle-native     # 构建 lactssl-dns-all-native
make check-native      # 体积、依赖白名单、manifest 对齐检查
```
