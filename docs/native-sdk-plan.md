# 插件包 SDK 重构（去上游 SDK）分析与实施方案

- 日期：2026-11-16
- 范围：`LACTSSL-Plugins` 的 DNS / 部署 / 通知插件包
- 目标：把插件对上游 SDK（lego、AWS/Azure/GCP/阿里云/腾讯云等）的依赖，按服务商逐个替换为仓库内重构的最小 HTTP/协议实现；**保留原有插件包，不替换、不改名**；新增带标签的并行插件包，实现增量发布、可随时回退。
- 结论：方案可行，收益几乎全部在 DNS 包；部署/通知包已经接近纯 `net/http` 体积下限，不建议重写。

> **实施进展（2026-11-16）**
>
> - P0 已完成：`dns.Provider` 已去掉 lego 硬依赖；生成器支持 `sdk: native` 变体；`plugins.json` / 索引支持 `variant_of`、`conflicts_with`、`sdk`。
> - P1 已完成：`heavy-native` 的 10 家 provider 全部完成重构，实测 **7.80 MiB**（原 `heavy` 38.59 MiB），manifest 与原包逐字段一致，依赖闭包只剩 `miekg/dns` + Go 标准库（`golang.org/x/net`、`x/sys` 为其传递依赖）。
> - CI 已加入 `scripts/check_native.sh`：heavy 体积预算 10 MiB、core 预算 19 MiB、已迁移 SDK 黑名单、manifest 对比。
> - P2 第一批已完成：`core-native`（184 家）从 20.55 MiB 降到 **17.81 MiB**，已迁移 11 家：`alidns`、`aliesa`、`azion`、`dnsimple`、`exoscale`、`linode`、`ns1`、`ovh`、`scaleway`、`volcengine`、`vultr`。
> - 2026-11-17 调整：构建阶段不再分别发布 `core-native` 与 `heavy-native`，二者合并为单个 `all-native` 包（194 个 provider，实测约 **18.1 MiB**，构建标签 `bundle_dns_all_native`）。下文中关于两个 native 变体的命名与体积预算属于方案阶段记录，当前以 README 与 `docs/native-providers.md` 为准。
> - 未完成：`core-native` 长尾 provider；`rfc2136` 的 GSS-TSIG（native 变体明确报错，原包保留）。

---

## 0. 结论摘要

1. 当前体积（`linux/amd64`，`-s -w`，实测）：

   | 包 | 实测二进制 | 说明 |
   |---|---:|---|
   | `lactssl-dns-core` | 20.55 MiB | 184 个常用服务商 |
   | `lactssl-dns-heavy` | 38.59 MiB | 10 个大型云厂商 |
   | `lactssl-dns-all` | 57.79 MiB | 194 个服务商全量 |
   | `lactssl-deploy-all` | 6.84 MiB | 7 个部署目标 |
   | `lactssl-notify-all` | 6.58 MiB | 23 个通知渠道 |

2. DNS 包体积的 90% 以上来自上游 SDK 及其传递依赖。`heavy` 包只有 10 个服务商，却占 DNS 总体积约 65%；其中 `gcloud`、`yandexcloud` 两个服务商单独就带来约 27 MiB 的增量。

3. Go 链接器已经做了函数级死代码消除（DCE），**简单地把 SDK 源码复制进仓库不会明显缩小体积**。要真正缩小，只有两条路：
   - 按插件实际使用的 API，重写为最小 HTTP/签名实现（推荐）；
   - 只保留 SDK 中真正被调用的少数函数，并替换其反射/序列化框架（成本高，收益接近重写）。

4. 实测纯 `net/http` + TLS + JSON 的 Go 程序下限约 6.34 MiB；用 194 个空实现服务商做占位实验，二进制约 6.00 MiB。因此：
   - `heavy-native` 目标 **8–10 MiB**（当前 38.59 MiB，预计下降约 75%）；
   - `core-native` 目标 **9–12 MiB**（当前 20.55 MiB，预计下降约 45–55%）；
   - DNS 两类合计从 59.14 MiB 降到 **18–21 MiB**，下降约 65–70%。

5. 推荐采用“**同名功能、不同包名**”的并行方案：
   - 原包：`lactssl-dns-core`、`lactssl-dns-heavy`（完全不动）；
   - 新包：`lactssl-dns-core-native`、`lactssl-dns-heavy-native`；
   - 插件 ID：`core-native`、`heavy-native`；
   - 构建标签：`bundle_dns_core_native`、`bundle_dns_heavy_native`；
   - **包内 provider ID 保持不变**（`cloudflare`、`route53` 等），因此数据库中的账户、任务、凭据无需迁移，切换实现只是换一个 helper 文件。

6. 建议先做 `heavy-native`（10 个服务商，收益最大、范围可控），再做 `core-native`（184 个，按 SDK 体积排序分批）。`rfc2136` 的 GSS-TSIG 是唯一明显的高风险项，可最后处理或暂时保留 lego 实现。

---

## 1. 目标与约束

### 1.1 目标

- 新插件包不依赖上游服务商 SDK（lego 及各云厂商 SDK），只依赖 Go 标准库和本仓库内部包。
- 新包与旧包功能、协议、provider ID、字段 key、环境变量名、manifest 完全兼容。
- 新旧包可同时存在于仓库索引和 Release 中，用户按需选择；旧包不被替换、不被改名。
- 每个服务商的迁移是独立增量：可以单独合并、单独发布、单独回退。
- 每个阶段都能量化体积变化，并有自动化测试防止行为回归。

### 1.2 约束

- 协议保持 v1：`manifest` / `present` / `cleanup` 子命令、stdin JSON、退出码、env 变量不变。
- `catalog` 元数据是唯一来源：provider ID、别名、字段、`Env` 名不得变化。
- 原包构建路径、发布资产、索引条目保持不变。
- 不引入新的第三方运行时依赖；签名、认证、HTTP 客户端全部重构。
- 不复制上游 SDK 代码，除非保留许可证并明确标注；优先按公开 API 文档做干净实现。

---

## 2. 现状测量

### 2.1 依赖结构

- `dns/providers/<id>/plugin.go` 是生成出来的薄包装，统一 `import lego/providers/dns/<id>`，调用 `lego.NewDNSProvider()`。
- 194 个 DNS 服务商中，**99 个**会引入 lego 之外的第三方模块；其余 95 个主要使用 lego 自带的 HTTP 实现。
- 体积最大的依赖类别：
  - Google Cloud：`google.golang.org/api`、`grpc`、`protobuf`、`golang.org/x/oauth2`；
  - Yandex Cloud：`yandex-cloud/go-sdk`、`go-genproto`、`grpc`、`protobuf`；
  - AWS：`aws-sdk-go-v2`（route53、lightsail、STS、SSO、IMDS）；
  - Azure：`azidentity`、`armdns`、`armprivatedns`、`armresourcegraph`、MSAL；
  - 阿里云/腾讯云/华为云/火山引擎/百度云/京东云：各自 SDK 及序列化框架；
  - Sakura、Akamai、OCI、IBM、Infoblox、Exoscale、Linode 等：厂商 SDK。

### 2.2 典型固定成本

单独构建一个只包含 `dns` 包和一个服务商的探针程序（不含 bundle 标签），结果如下：

- 空探针（只 import `dns` 包）：约 **1.70 MiB**；
- 典型 lego 服务商：约 **2.0–2.1 MiB** 固定成本（lego 公共包、`miekg/dns`、backoff、jose 等）；
- 纯 `net/http` + TLS + JSON：约 **6.34 MiB**；
- 10 个空 DNS 服务商 + `net/http`：约 **6.00 MiB**；
- 194 个空 DNS 服务商 + `net/http`：约 **6.00 MiB**（服务商注册本身几乎不占体积）。

这说明：**重构 DNS 包的理论下限约 6 MiB，真正实现后预计 8–12 MiB**；当前 20–58 MiB 的差额几乎全是 SDK 与 lego 公共代码。

### 2.3 单个服务商的增量体积

> 方法：每次只 import 一个 provider，构建 `linux/amd64`、`-s -w`，减去空探针 1.70 MiB。
> 注意：这些数字包含共享的运行时和 `net/http`，**不能直接相加**；它们用于排序和判断优先级。

#### Heavy 包（10 个）

| 服务商 | 单 provider 总大小 | 相对空探针增量 | 主要上游依赖 | 难度 | 优先级 |
|---|---:|---:|---|---|---|
| `gcloud` | 15.67 MiB | 13.98 MiB | Google API、gRPC、protobuf、oauth2 | 高 | P1 |
| `yandexcloud` | 14.92 MiB | 13.23 MiB | Yandex SDK、genproto、gRPC | 中高 | P1 |
| `azuredns` | 5.86 MiB | 4.17 MiB | Azure ARM、azidentity、MSAL | 高 | P1 |
| `sakuracloud` | 5.79 MiB | 4.09 MiB | sacloud iaas/api-client | 中 | P1 |
| `huaweicloud` | 5.60 MiB | 3.90 MiB | 华为云 SDK、YAML、JSON 库 | 中 | P1 |
| `edgedns` | 5.56 MiB | 3.86 MiB | Akamai EdgeGrid、retryablehttp、testify | 高 | P1 |
| `lightsail` | 4.23 MiB | 2.54 MiB | AWS SDK v2 | 中 | P1 |
| `route53` | 4.23 MiB | 2.53 MiB | AWS SDK v2、SigV4 | 高 | P1 |
| `iij` | 3.86 MiB | 2.16 MiB | iij/doapi | 中 | P2 |
| `rfc2136` | 3.85 MiB | 2.16 MiB | miekg/dns、tsig、gokrb5 | 很高 | P3 |

#### Core 包中体积最大的 20 个

| 服务商 | 单 provider 总大小 | 相对空探针增量 | 主要上游依赖 | 难度 |
|---|---:|---:|---|---|
| `linode` | 7.25 MiB | 5.55 MiB | linodego、resty、oauth2 | 低 |
| `exoscale` | 6.02 MiB | 4.32 MiB | egoscale、viper、validator | 中 |
| `volcengine` | 5.34 MiB | 3.64 MiB | volc-sdk-golang | 中 |
| `alidns` | 5.17 MiB | 3.48 MiB | 阿里云 SDK、tea、darabonba | 中 |
| `baiducloud` | 5.16 MiB | 3.46 MiB | bce-sdk-go | 中 |
| `aliesa` | 5.13 MiB | 3.43 MiB | 阿里云 ESA SDK | 中 |
| `iijdpf` | 4.96 MiB | 3.27 MiB | iij-dpf、json-iterator | 中 |
| `ns1` | 4.75 MiB | 3.05 MiB | ns1-go | 低 |
| `ibmcloud` | 4.52 MiB | 2.82 MiB | softlayer-go、xmlrpc | 中高 |
| `inwx` | 4.42 MiB | 2.73 MiB | goinwx、xmlrpc、otp | 中 |
| `infoblox` | 4.20 MiB | 2.51 MiB | infoblox-go-client | 中 |
| `oraclecloud` | 4.15 MiB | 2.46 MiB | OCI SDK、pkcs8、gobreaker | 高 |
| `liquidweb` | 4.10 MiB | 2.40 MiB | liquidweb-go、CLI | 中 |
| `scaleway` | 4.07 MiB | 2.38 MiB | scaleway-sdk-go | 低 |
| `azure` | 3.95 MiB | 2.25 MiB | Azure legacy SDK、ADAL | 高 |
| `ucloud` | 3.94 MiB | 2.25 MiB | ucloud-sdk-go | 中 |
| `designate` | 3.93 MiB | 2.24 MiB | gophercloud | 中 |
| `azion` | 3.93 MiB | 2.24 MiB | azionapi-go-sdk | 中 |
| `ovh` | 3.93 MiB | 2.23 MiB | go-ovh | 中 |
| `namecheap` | 3.78 MiB | 2.08 MiB | x/text（lego 自带 HTTP） | 低 |

其余约 170 个服务商的单 provider 增量多在 2.03–2.10 MiB，基本就是 lego 公共成本；它们单个的额外代码很小，但数量多。

### 2.4 关键判断

- **Heavy 包是最高优先级**：10 个服务商覆盖了 DNS 体积的约 65%，而且 provider 集合固定、测试面可控。
- **Core 包不能只迁移几个**：只要还有一个 provider 使用 lego，lego 公共成本（约 2 MiB）就仍在；但迁移 `linode`、`exoscale`、`alidns`、`aliesa`、`volcengine`、`baiducloud` 等 SDK 大户，仍能先去掉 10 MiB 左右的第三方代码。
- **部署/通知包没有重写价值**：两者已经是 6.8/6.6 MiB，接近 `net/http` 下限；重写最多省几百 KiB，风险和收益不成比例。

---

## 3. 方案选择

### 3.1 三种实现路线

| 路线 | 做法 | 体积收益 | 风险/成本 | 结论 |
|---|---|---|---|---|
| A. 源码裁剪 | 把 lego/SDK 源码复制进仓库，只保留调用到的文件 | 很小（链接器已 DCE） | 许可证、升级、漏洞维护 | 不推荐 |
| B. 最小 HTTP 重写 | 按服务商公开 API 重写 zone 查询 + TXT 增删 | 最大 | 认证/签名/边界条件要逐家验证 | **推荐** |
| C. 共享认证层 + 轻量客户端 | 重构 SigV4/TC3/阿里云签名等，provider 只写端点映射 | 大 | 前期投入在共享层，后期 provider 很快 | **推荐，B 的落地方式** |

推荐组合：**C + B**。先做共享的 HTTP/签名/协议层，再按 provider 写薄实现。

### 3.2 每个 provider 真正需要实现的最小契约

DNS-01 只需要三件事：

1. 找到目标域名对应的 zone（配置指定或 SOA 查询）；
2. 读取 `_acme-challenge.<domain>` 的 TXT 记录集，追加本次 `keyAuth`，保留已有值，写入 TTL；
3. 清理时删除本次值；记录集为空则删除记录，非空则更新。

因此每个 provider 通常只需要 2–4 个 API：
- 列 zone / 查 zone ID；
- 查 TXT record set；
- 新增或更新 record set；
- 删除 record set（可选）。

不需要实现 SDK 里的分页、重试、所有资源类型、所有认证模式。认证模式以 `catalog/data.json` 中已有的字段为准，不额外扩展。

### 3.3 不建议的做法

- **给 provider ID 加后缀**（如 `cloudflare_native`）：会导致宿主数据库中的账户、任务、凭据需要迁移，UI 出现重复服务商，违背“无缝切换”。只应在确实需要新旧实现并存于同一宿主时使用。
- **直接替换原包**：用户无法回退，违反“保留原有插件包”的要求。
- **用 UPX 等二进制压缩**：只影响下载体积，不影响运行时内存/磁盘占用，且会增加杀软误报；不能替代依赖裁剪。

---

## 4. 命名与增量发布方案

### 4.1 包名与标签

| 项目 | 原包（保持不动） | 新包（重构实现） |
|---|---|---|
| 包文件名 | `lactssl-dns-heavy` | `lactssl-dns-heavy-native` |
| 插件 ID | `heavy` | `heavy-native` |
| 构建标签 | `bundle_dns_heavy` | `bundle_dns_heavy_native` |
| 包内 provider ID | `gcloud`、`route53`… | **完全相同** |
| `plugins.json` | 原条目不变 | 新增条目 |
| Release 资产 | 原资产不变 | 新增资产 |

Core 同理：`lactssl-dns-core-native` / `core-native` / `bundle_dns_core_native`。

标签建议统一用 `-native`，含义是“仓库重构、无上游 SDK”。如果团队更倾向 `-self`、`-nosdk`、`-std`，只需在生成器里改一个常量，不影响架构。

### 4.2 目录与构建标签

```text
dns/
  providers/                 # 原有 lego 包装，完全保留
  native/
    providers/
      cloudflare/            # 重构 cloudflare
      route53/               # 重构 route53
      ...
  groups/
    core/                    # 原有
    heavy/                   # 原有
    core_native/             # 新：core-native 的 provider 清单
    heavy_native/            # 新：heavy-native 的 provider 清单

cmd/dns-helper/
  group_core.go              # 原有 bundle_dns_core
  group_heavy.go             # 原有 bundle_dns_heavy
  group_core_native.go       # 新 bundle_dns_core_native
  group_heavy_native.go      # 新 bundle_dns_heavy_native
```

`dns/groups/*_native/all.go` 由生成器输出，允许两种来源混合：

```go
import (
    _ "github.com/AXmishell/LACTSSL-Plugins/dns/native/providers/cloudflare"
    _ "github.com/AXmishell/LACTSSL-Plugins/dns/native/providers/route53"
    // 尚未迁移的 provider 继续引用原包装
    _ "github.com/AXmishell/LACTSSL-Plugins/dns/providers/hetzner"
)
```

这样迁移是逐个替换 import，不需要一次性完成；每替换一个，对应 SDK 就从链接图里消失。

### 4.3 两种增量策略

#### 策略 A：完整同名集合，逐个替换（推荐）

- `heavy-native` 一开始就包含原 `heavy` 的全部 10 个 provider；`core-native` 一开始就包含原 `core` 的全部 184 个。
- 未迁移的 provider 暂时继续用原 lego 包装；每迁移一个，体积下降一截。
- 优点：功能始终完整，用户切换后不会缺 provider；manifest 可直接逐字段对比。
- 缺点：在最后一个 provider 迁移完成前，新包仍会链接 lego；但各 SDK 的移除收益已经逐步体现。

#### 策略 B：先做“纯重构子集”

- `heavy-native` 包含全部 10 个；`core-lite-native` 只包含 30–35 个默认启用、使用频率最高的 provider。
- 优点：第一个版本就完全不依赖 lego，体积最小、叙事最清晰。
- 缺点：用户如果需要长尾 provider，仍需安装原 `core`；两个包不能同时启用（provider ID 重叠）。

**建议**：`heavy` 用策略 A；`core` 先做一个 `core-native`（策略 A），如果希望尽早拿到“纯重构”包，再额外发布 `core-lite-native`（策略 B）。不要把 `core-lite-native` 当作最终形态。

### 4.4 新旧包共存与冲突

新包 provider ID 与原包相同，因此**同一个宿主不能同时启用两个包**，否则宿主注册表会报 `duplicate provider`（`internal/dnsplugin` 的行为是第一个注册成功，后续报错）。

推荐按以下顺序处理：

1. **短期（不改宿主）**：索引中新增 `variant_of` / `conflicts_with` 字段；文档明确“同一类别只能安装一个变体”；安装新包前先卸载旧包。宿主旧版本会忽略未知字段，不会因此崩溃。
2. **中期（宿主小改）**：宿主安装插件时比较 `providers` 集合，检测重叠并提示“替换/停用旧包”；插件页把 `core` 与 `core-native` 显示为同一组变体。
3. **备选（零宿主改动）**：单独发布 `index-native.json`，只包含重构变体（以及部署/通知原包）。用户通过切换仓库 URL 选择 native 通道。缺点是维护两份索引，但完全避免冲突。

不建议用 provider ID 后缀来规避冲突，原因见 3.3。

### 4.5 `plugins.json` 变更示例

```json
{
  "kind": "dns",
  "id": "heavy-native",
  "version": "0.1.0",
  "tag": "bundle_dns_heavy_native",
  "name": "DNS 插件包（大型云厂商·重构）",
  "description": "10 个大型云厂商的重构实现，不依赖上游 SDK。",
  "bundle": true,
  "variant_of": "heavy",
  "conflicts_with": ["heavy"],
  "sdk": "native",
  "providers": ["gcloud", "yandexcloud", "..."]
}
```

原条目可以补 `"sdk": "lego"`，也可以不改。`variant_of`、`conflicts_with`、`sdk` 都是可选字段；`scripts/pluginctl` 需要同步扩展结构体，否则 `manifest` 重新生成时会丢失。

---

## 5. 技术架构

### 5.1 去掉 `dns` 包对 lego 的硬依赖

当前 `dns.Provider.New` 返回 `github.com/go-acme/lego/v4/challenge.Provider`，导致所有 native provider 也必须 import lego 才能实现签名。

改为仓库内最小接口：

```go
// dns/provider.go
type Provider interface {
    ID() string
    Aliases() []string
    New(cfg map[string]string) (ChallengeProvider, error)
}

type ChallengeProvider interface {
    Present(domain, token, keyAuth string) error
    CleanUp(domain, token, keyAuth string) error
}
```

- 原 lego 包装的 `New` 返回类型改成 `dns.ChallengeProvider`；lego 的 provider 结构体天然满足该接口，不需要改实现。
- `scripts/gen_dns_plugins` 的模板同步改成返回 `dns.ChallengeProvider`，并重新生成 194 个包装；这样包装文件连 `lego/challenge` 都不再 import。
- `cmd/dns-helper` 的 `run` 参数改成 `dns.ChallengeProvider`。
- `dns/registry.go` 不再 import lego。
- 这样 native provider 可以完全不接触 lego；未迁移的 provider 继续 import lego 包装即可。

### 5.2 共享基础包（全部只依赖标准库）

```text
sdk/
  httpapi/        # 统一 Client：超时、重试、退避、JSON/XML、错误归一化、脱敏
  dnsutil/        # FQDN 规范化、zone 切分、TXT 值编码、TTL
  dnsmsg/         # 可选：最小 DNS 查询/UPDATE 报文 + TSIG
  sigv4/          # AWS SigV4（route53、lightsail）
  alibaba/        # 阿里云 RPC 签名（alidns、aliesa）
  tencent/        # 腾讯云 TC3（dnspod、tencentcloud、edgeone）
  volcengine/     # 火山引擎签名
  huawei/         # 华为云 AK/SK 签名
  azure/          # AAD client credentials + ARM 请求
  gcp/            # service account JWT -> OAuth2 token
  edgegrid/       # Akamai EdgeGrid
  ovh/            # OVH 签名
```

实现原则：

- 只用 `net/http`、`crypto/*`、`encoding/*`、`net/url`、`time` 等标准库。
- 每个签名包提供**固定的测试向量**，用官方文档/SDK 的示例请求做 golden test。
- 统一错误：`provider <id>: <action>: <status>: <bounded body>`，不打印 Authorization、token、secret。
- 统一超时：单请求 15–30s；整体 `present/cleanup` 由宿主 60s 超时兜底。
- 统一重试：只对 429/5xx/网络错误做有限次退避；写操作要保证幂等或可安全重试。

### 5.3 重构 provider 的目录结构

```text
dns/native/providers/cloudflare/
  provider.go      # New/ID/Aliases/Present/CleanUp
  api.go           # 端点、请求/响应结构
  provider_test.go # httptest mock + 行为测试
```

`provider.go` 不读取环境变量，直接使用 `New(cfg map[string]string)` 传入的配置；`catalog.EnvForProvider` 仍由 helper 统一设置环境变量，保持原行为。这样：
- manifest 字段和宿主表单完全不变；
- 原有环境变量兼容路径不变；
- native provider 不需要依赖 lego 的 `env` 包。

### 5.4 生成器改造

`scripts/gen_dns_plugins` 目前只生成 `dns/providers/<id>` 和 `dns/all`。需要扩展：

1. 读取 `plugins.json` 中 `sdk == "native"` 的条目；
2. 为每个 native 变体生成 `dns/groups/<id>/all.go` 和 `cmd/dns-helper/group_<id>.go`；
3. 根据 `dns/native/providers/` 下实际存在的包，决定 import native 还是原包装；
4. 校验 `plugins.json` 的 `providers` 与 manifest 输出完全一致（沿用现有 `pluginctl` 校验）；
5. 生成时保持排序稳定，避免无意义 diff。

### 5.5 认证实现要点

| 认证 | 关键点 | 复用范围 |
|---|---|---|
| AWS SigV4 | canonical request、HMAC-SHA256、`x-amz-security-token`、route53 XML / lightsail JSON | route53、lightsail、vinyldns（可选） |
| 阿里云 RPC | HMAC-SHA1、参数排序/编码、`Signature`、`Timestamp`、`Version` | alidns、aliesa |
| 腾讯云 TC3 | HMAC-SHA256、`X-TC-*` 头、地域/版本 | dnspod、tencentcloud、edgeone |
| 火山引擎 | HMAC-SHA256、`X-Date`、`Authorization` | volcengine |
| 华为云 | AK/SK、`SDK-HMAC-SHA256`、`X-Sdk-Date` | huaweicloud |
| Azure | AAD `client_credentials`、`resource=https://management.azure.com/`、ARM REST、私有 DNS | azuredns、azure（legacy 需单独处理） |
| GCP | service account JSON -> JWT RS256 -> token endpoint -> DNS REST API | gcloud |
| Akamai EdgeGrid | `EG1-HMAC-SHA256`、nonce、timestamp、content hash | edgedns |
| OVH | `$1$` SHA1 签名（method+url+body+timestamp） | ovh |
| RFC2136 | DNS UPDATE 报文、TSIG HMAC（SHA1/224/256/384/512）、GSS-TSIG | rfc2136 |

`gcloud`、`azuredns`、`edgedns`、`route53`、`rfc2136` 是难度最高的五个，建议放在各自批次最后，并配真实环境冒烟测试。

---

## 6. 分阶段实施

> 每个阶段结束都必须满足：`go build ./...`、`go test ./...`、原 bundle manifest 不变、native manifest 与对应原 manifest 逐字段一致（忽略 `version`/`name`）。

### P0：基线与脚手架（3–5 人日）

1. 固化当前体积基线：
   - 新增 `scripts/size-report`，输出各 bundle 的 stripped/uncompressed 大小；
   - 把本文的测量结果写入 CI artifact，后续每次 PR 对比。
2. 增加 `dns.Provider` 本地接口，去掉 `dns/registry.go` 对 lego 的 import。
3. 新建 `dns/native/providers/`、`dns/groups/*_native/` 目录和生成器骨架。
4. 扩展 `plugins.json` 结构体：`variant_of`、`conflicts_with`、`sdk`。
5. 增加依赖守卫脚本：
   - `bundle_dns_*_native` 构建的 `go list -deps` 不得出现 `github.com/go-acme/lego` 和已知厂商 SDK；
   - 未迁移阶段允许白名单例外，最终逐步清空。
6. 增加 native manifest 对比测试。

**验收**：原 4 个 bundle（core/heavy/deploy/notify）体积、manifest、测试完全不变；native 空 bundle 可以构建。

### P1：`heavy-native`（15–25 人日）

按以下顺序迁移 10 个服务商：

1. `yandexcloud`：IAM token + REST，改动最小，先验证框架；
2. `sakuracloud`：Basic auth + JSON；
3. `iij`：doapi JSON-RPC；
4. `lightsail`：SigV4 + JSON；
5. `route53`：SigV4 + XML + change 轮询；
6. `huaweicloud`：AK/SK + REST；
7. `azuredns`：AAD + ARM + 私有 DNS；
8. `edgedns`：EdgeGrid；
9. `gcloud`：service account JWT + DNS REST；
10. `rfc2136`：TSIG；GSS-TSIG 可延后或暂时保留原实现。

**体积目标**：`lactssl-dns-heavy-native` ≤ 10 MiB（当前 38.59 MiB）。

**验收**：
- 10 个 provider 的 manifest 与原 `heavy` 完全一致；
- 每个 provider 有 `httptest` mock 测试：创建 TXT、保留已有值、删除本次值、空记录集删除、404 幂等；
- `route53`/`azuredns`/`gcloud` 至少各做一次真实 API 冒烟（可用测试账号，CI 中跳过）；
- 依赖守卫通过（除 `rfc2136` 的 `miekg/dns` 例外，如果决定保留）。

#### 新增一个 native 变体的操作步骤

1. 在 `dns/native/providers/<id>/` 实现 `plugin` + `provider`，并在 `init()` 中 `dns.Register`。
2. 在 `plugins.json` 增加条目：`"id": "<id>-native"`、`"tag": "bundle_<kind>_<id>_native"`、`"sdk": "native"`、`"variant_of": "<id>"`、`"conflicts_with": ["<id>"]`，`providers` 与原包一致。
3. 执行 `make gen`：生成器会为已存在的 `dns/native/providers/<id>` 使用 native import，其余 provider 继续使用原 lego 包装。
4. 执行 `make check-native` 或对应的体积/依赖/manifest 检查。
5. 原包条目、版本、资产完全不动；新包独立发版。

`core-native` 的第一步只需要增加上述条目；后续每迁移一个 core provider，生成器自动切换 import，包体积随之下降。

### P2：`core-native` 第一批（15–25 人日）——已落地 11 家

优先迁移 core 中 SDK 体积大、使用频率高的服务商：

- 第一批：`linode`、`exoscale`、`scaleway`、`ns1`、`vultr`、`namecheap`、`ovh`、`dnsimple`、`hetzner`、`gandi`；
- 第二批：`alidns`、`aliesa`、`tencentcloud`、`dnspod`、`edgeone`、`volcengine`、`baiducloud`、`jdcloud`；
- 第三批：`infoblox`、`designate`、`oraclecloud`、`ibmcloud`、`inwx`、`liquidweb`、`ucloud`、`azure`（legacy）。

**体积目标**：`core-native` 先降到 ≤ 16 MiB；如果完成全部 184 个，目标 ≤ 12 MiB。

**验收**：同上；另外增加“同一 provider 新旧实现的请求语义对比”测试（mock 下游，比较创建/删除的最终记录状态，不要求请求字节完全一致）。

### P3：`core-native` 长尾（30–60 人日，可选）

剩余约 150 个服务商，绝大多数是简单 REST。建议：

- 抽象一个表驱动 `restProvider`：
  - base URL、认证方式、zone 列表路径、record set 路径、JSON/XML 字段映射；
  - 用配置表描述，不为每家写一套重复代码。
- 按 API 家族分组（Bearer、Basic、API-Key header、query signature），同类共享实现。
- 每批 20–30 个，独立 PR、独立测试。

完成后 `core-native` 不再包含 lego，达到“完全不依赖上游 SDK”。

### P4：宿主/索引/发布接线（5–8 人日）

1. `plugins.json` 增加 native 条目；`make gen` 生成新 group 文件。
2. `Makefile` 增加 `bundle-native` 目标。
3. CI 增加：
   - native bundle 构建（4 平台）；
   - 体积预算检查；
   - 依赖守卫；
   - manifest 对比；
   - mock 测试。
4. Release workflow 构建并发布新资产，原资产不变。
5. 索引合并新旧条目；如采用“独立索引”方案，生成 `index-native.json`。
6. 文档：安装/切换/回退说明；明确“同一类别只能启用一个变体”。

---

## 7. 测试与质量门禁

| 检查 | 内容 | 失败后果 |
|---|---|---|
| manifest 对比 | native bundle 与对应原 bundle 的 provider ID、别名、字段、`Env`、名称/描述逐字段一致 | 阻止合并 |
| 协议兼容 | 用原请求 JSON 调用 native `present/cleanup`，退出码与行为一致 | 阻止合并 |
| provider 行为 | mock 下游：创建、追加、删除、空集删除、重复删除、已有 CNAME 冲突 | 阻止合并 |
| 签名 golden | 每个签名包用官方示例做固定向量 | 阻止合并 |
| 真实 API 冒烟 | 可选、按凭据启用；不阻塞普通 CI | 记录结果 |
| 依赖守卫 | native 构建不得出现 lego / 厂商 SDK | 阻止合并 |
| 体积预算 | `heavy-native ≤ 10 MiB`、`core-native ≤ 12 MiB`、`all-native ≤ 13 MiB` | 阻止合并 |
| 交叉编译 | linux/amd64、linux/arm64、darwin/arm64、windows/amd64 | 阻止合并 |
| 安全 | 不记录密钥；错误体截断；`go vet`、`govulncheck` | 阻止合并 |

---

## 8. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| 认证/签名实现错误 | 用户签发失败，且错误难定位 | 官方测试向量 + mock + 真实账号冒烟；按 provider 灰度 |
| API 版本/端点漂移 | 某家服务商不可用 | 固定 API 版本；fixture 测试；保留原包可立即回退 |
| GSS-TSIG（rfc2136） | Kerberos 环境无法工作 | 最后处理；先支持 TSIG；GSS 暂留 lego 或单独说明 |
| 私有 Zone / zone 发现差异 | 找不到 zone 或写错 zone | 对 route53/azure/gcloud 做私有 zone 专项测试；保留 `hosted_zone_id` 等字段语义 |
| 新旧包重复 provider ID | 宿主注册冲突 | 安装层互斥/变体分组；短期文档约束；备选独立索引 |
| 长尾 provider 维护成本 | 184 家手写实现不可持续 | 表驱动 `restProvider`；只对高频 provider 重构，长尾继续用 lego |
| 安全审计面 | 重构后签名代码漏洞 | 复用经过审计的共享签名包；fuzz；限制错误输出 |
| 许可证 | 复制 SDK 代码引发合规问题 | 干净实现；如复制则保留 MIT/Apache 声明 |
| 体积反弹 | 后续新增依赖 | CI 体积预算 + 依赖守卫 |

**回退策略**：因为原包完全不动，回退只需卸载 native 包、重新安装原包；provider ID 不变，账户/任务/凭据无需迁移。这是本方案相对“直接替换”的最大优势。

---

## 9. 工作量估算

| 阶段 | 内容 | 预估 |
|---|---|---:|
| P0 | 基线、接口、生成器、CI 骨架 | 3–5 人日 |
| P1 | heavy-native 10 个服务商 | 15–25 人日 |
| P2 | core-native 第一批（20–30 个高频/大 SDK） | 15–25 人日 |
| P3 | core-native 长尾（约 150 个） | 30–60 人日 |
| P4 | 宿主/索引/发布/文档 | 5–8 人日 |
| **MVP（P0–P2 + P4）** | heavy + 高频 core，已可交付 | **约 38–63 人日** |
| **完整去 SDK** | 全部 194 个 | **约 68–123 人日** |

如果只做 `heavy-native`，约 **20–30 人日**即可拿到 DNS 体积下降约 75% 的最大单笔收益。

---

## 10. 需要确认的决策点

1. **新包后缀**：`-native`、`-self`、`-nosdk` 还是 `-std`？建议 `-native`。
2. **Core 策略**：先做完整 184 个（策略 A），还是先做 30–35 个的 `core-lite-native`（策略 B）？
3. **rfc2136**：是否接受首版不支持 GSS-TSIG？如果不接受，是否允许该 provider 暂时继续链接 lego？
4. **宿主改动**：是否愿意为“变体分组/安装互斥”改宿主？如果不愿意，是否接受独立 `index-native.json`？
5. **依赖白名单**：是否允许保留 `miekg/dns`（约 0.3 MiB）用于 zone 查询和 RFC2136，还是要求完全标准库？
6. **真实 API 测试**：是否能提供各云厂商测试账号/子账号，用于发布前冒烟？

---

## 11. 附录：测量方法

```bash
# 当前 bundle 体积
CGO_ENABLED=0 go build -trimpath -tags "bundle_dns_core,nethttpomithttp2" \
  -ldflags "-s -w" -o /tmp/dns-core ./cmd/dns-helper
CGO_ENABLED=0 go build -trimpath -tags "bundle_dns_heavy,nethttpomithttp2" \
  -ldflags "-s -w" -o /tmp/dns-heavy ./cmd/dns-helper
CGO_ENABLED=0 go build -trimpath -tags "nethttpomithttp2" \
  -ldflags "-s -w" -o /tmp/dns-all ./cmd/dns-helper

# 单 provider 增量：临时 main 只 import 一个 provider，构建后减空探针
# 空探针：只 import github.com/AXmishell/LACTSSL-Plugins/dns
# 结果见第 2.3 节表格

# 纯 net/http 下限：一个只做 TLS GET + JSON 解码的程序
# 实测 6.34 MiB

# 依赖清单
go list -deps -tags bundle_dns_heavy,nethttpomithttp2 \
  -f '{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}' ./cmd/dns-helper | sort -u
```

> 注：单 provider 增量包含共享运行时和 `net/http`，不能简单相加；它用于判断“先迁移谁”和估算单家成本。最终体积以完整 bundle 构建为准。
