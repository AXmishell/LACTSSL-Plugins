# Native Provider 实现说明

`lactssl-dns-all-native` 把原 `heavy-native` 与 `core-native` 合并为一个全量重构包：覆盖 `core` + `heavy` 的 194 个 provider，provider ID、字段、别名与 manifest 完全一致。已迁移 provider 的实现位于本仓库，不再链接对应的 lego 包装与上游云 SDK。

## 1. 已实现 provider

| Provider | 认证 | API | 备注 |
|---|---|---|---|
| `gcloud` | service account JSON -> JWT RS256 -> OAuth2 Bearer | Cloud DNS REST v1 | 支持 public/private managed zone |
| `yandexcloud` | IAM token Bearer | Yandex Cloud DNS REST | 按 folder 列 zone |
| `azuredns` | AAD client credentials | Azure ARM REST | 支持 public / private DNS、public/china/usgovernment |
| `sakuracloud` | Basic Auth（token/secret） | Sakura Cloud CommonServiceItem API | 按 zone 名匹配 |
| `huaweicloud` | AK/SK SDK-HMAC-SHA256 | 华为云 DNS v2 REST | 按 zone 名匹配 |
| `edgedns` | EdgeGrid EG1-HMAC-SHA256 | Akamai Config DNS v2 | 支持 `.edgerc`、环境变量、account switch key |
| `route53` | AWS SigV4 | Route 53 REST XML | 支持 hosted zone ID、private zone、change 等待 |
| `lightsail` | AWS SigV4 | Lightsail JSON 1.1 | 使用 `dns_zone` 指定域名 |
| `iij` | IIJ HmacSHA256 | IIJ DO API | 支持 zone/record/commit 流程 |
| `rfc2136` | TSIG HMAC（MD5/SHA1/224/256/384/512） | DNS UPDATE | **不支持 GSS-TSIG**，见下 |

## 2. Core 已迁移 provider

`lactssl-dns-all-native` 中，常用部分的 184 个 provider ID 与 `lactssl-dns-core` 完全一致；已迁移的实现位于 `dns/native/providers/`，其余继续引用原 lego 包装。

| Provider | 认证 | API |
|---|---|---|
| `alidns` | 阿里云 RPC HMAC-SHA1 | Alibaba Cloud DNS `2015-01-09` |
| `aliesa` | 阿里云 RPC HMAC-SHA1 | ESA `2024-09-10` |
| `azion` | `Authorization: Token` | Azion Intelligent DNS v3 |
| `dnsimple` | Bearer + `/whoami` account | DNSimple v2 |
| `exoscale` | EXO2-HMAC-SHA256 | Exoscale DNS v2 |
| `linode` | Bearer | Linode API v4 |
| `ns1` | `X-NSONE-Key` | NS1 v1 |
| `ovh` | OVH `$1$` 签名或 Bearer access token | OVH `/1.0/domain/zone` |
| `scaleway` | `X-Auth-Token` | Scaleway Domain v2beta1 |
| `volcengine` | HMAC-SHA256 | Volcano Engine DNS `2018-08-01` |
| `vultr` | Bearer | Vultr API v2 |

合并后的 `all-native` 实测约 18.1 MiB；常用部分在迁移这 11 家后从 20.55 MiB 降到 17.81 MiB。194 个 provider 的 manifest 与原 `core` / `heavy` 逐字段一致。

## 3. 共享包

| 包 | 作用 |
|---|---|
| `sdk/httpapi` | 请求构造、超时、响应体上限、JSON/XML 解码、404/429 判断 |
| `sdk/dnsutil` | `_acme-challenge` FQDN、zone 后缀匹配、相对记录名、TXT 引号处理 |
| `sdk/sigv4` | AWS SigV4；测试向量与 AWS SDK 逐字节一致 |
| `sdk/huawei` | 华为云 SDK-HMAC-SHA256 |
| `sdk/gcp` | service account JWT 与 token 缓存 |
| `sdk/azure` | AAD client credentials 与云环境端点 |
| `sdk/edgegrid` | Akamai EdgeGrid 签名 |
| `sdk/alibaba` | 阿里云 RPC HMAC-SHA1 签名 |
| `sdk/volcengine` | 火山引擎 HMAC-SHA256 签名 |
| `sdk/exoscale` | Exoscale EXO2-HMAC-SHA256 签名 |
| `sdk/ovh` | OVH `$1$` 签名与 endpoint 映射 |

## 4. 行为约定

- `Present`：确保 `_acme-challenge.<domain>` 的 TXT 记录集包含本次 `keyAuth`；保留其他 TXT 值。
- `CleanUp`：只删除本次 `keyAuth`；记录集为空时删除整条记录；不存在时视为成功。
- TTL 使用各 provider 的原默认值（route53 10s、huaweicloud/iij 300s、其余 60s），与 lego 行为保持一致。
- 错误信息包含 provider 与动作，不输出 token/secret；响应体由 `httpapi` 截断。
- 所有请求使用 `net/http`，默认超时 30s；GCP/Azure token 请求使用 60s 上下文。

## 5. 已知限制

- `rfc2136` 的 GSS-TSIG 依赖 Kerberos/GSSAPI，native 变体不实现；检测到 `tsig_algorithm=gss` 时会直接报错。需要 GSS-TSIG 时使用原 `core` / `heavy` 包。
- `route53` / `lightsail` 目前支持静态 AK/SK 或环境变量，未实现 shared credentials 文件、STS AssumeRole、EC2 IMDS；原 `heavy` 包保留这些能力。
- `gcloud` 未实现 metadata server 默认凭据；需要显式 service account JSON 或 `GOOGLE_APPLICATION_CREDENTIALS`。
- `azuredns` 未实现 managed identity / workload identity，仅 client secret。
- `edgedns` 未实现请求限速（Akamai 的 request limit）；如遇 429，宿主会按失败重试。
- `alidns` / `aliesa` 仅支持静态 AccessKey/Secret，未实现 ECS RAM Role。
- `ovh` native 版支持 Application Key 签名与 Access Token；未实现 OAuth2 client credentials。
- `volcengine` native 版未实现临时凭证/SessionToken。
- `scaleway` 使用 `X-Auth-Token`（secret key），未使用 access key。

这些限制都可以在后续增量中补齐，不影响原 `core` / `heavy` 包。

## 6. 验证

```bash
go test ./dns/native/... ./sdk/...
./scripts/check_native.sh
```

`check_native.sh` 会：
1. 构建 `all-native` 并检查 ≤ 20 MiB；
2. 检查已迁移 provider 不再链接对应 lego 包装，且已迁移 SDK 未回归；
3. 对比全量 194 个 provider 的 manifest（忽略 version/name）。
