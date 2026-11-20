# lego v5 独立 Helper

本仓库在保留现有 `all-lego` / `all-native`（lego v4）的同时，新增独立的
`all-lego-v5` 插件包。它使用 `github.com/go-acme/lego/v5`，首版不包含
`dns/native` 实现，用于跟进上游 v5.6.0 与新增 provider。

## 包与入口

| 项目 | 值 |
|---|---|
| 插件 ID | `all-lego-v5` |
| Helper | `cmd/dns-helper-v5` |
| 包装目录 | `dnsv5/providers/<id>` |
| 兼容占位 | `dnsv5/legacy/<id>` |
| 元数据补充 | `dnsv5/meta/meta_gen.go` |
| 构建 | `make bundle-lego-v5` |
| 单包构建 | `make plugin KIND=dns ID=all-lego-v5 GOOS=linux GOARCH=amd64` |
| 实测体积（linux/amd64） | 约 37.2 MiB；压缩包约 13.3 MiB |

## Provider 覆盖

- 228 个 lego v5 provider，ID 与 v5 的 `providers/dns` 目录一致。
- 为兼容旧安装记录，v5 的两个重命名保留旧 ID：
  - `dnsupdate` 以 `rfc2136` 发布，`dnsupdate` 作为别名。
  - `webnamesru` 以 `webnames` 发布，`webnamesru` 作为别名。
- v5 已移除的 7 个 ID 保留同名占位：`dnspod`、`azure`、`brandit`、
  `cloudxns`、`googledomains`、`iij`、`iwantmyname`。调用这些条目会返回
  迁移提示；需要继续使用时请留在 v4 包。保留占位的目的是让新包完整覆盖
  旧包的 provider 集合，安装时能回收旧 helper，避免重复注册。
- 其余 41 个 v5 新增 provider 的字段从上游 `.toml` 的
  `[Configuration.Credentials]` / `[Configuration.Additional]` 生成；
  若 catalog 已有同名条目，则继续使用 catalog。
- 旧 194 个 provider 沿用历史 catalog 字段与环境变量。v5 更新了部分
  云厂商 SDK，个别 provider 可能新增可选字段或调整默认值；上线前建议
  用真实账号做一次 `present` / `cleanup` 回归。

## 生成

```bash
make gen-v5
```

生成器会读取 `go list -m -f '{{.Dir}}' github.com/go-acme/lego/v5`，
并重新生成：

- `dnsv5/providers/*/plugin.go`
- `dnsv5/legacy/*/plugin.go`
- `dnsv5/groups/all/all.go`
- `dnsv5/meta/meta_gen.go`

修改 `plugins.json`、生成器或 v5 依赖后运行 `make manifest`，CI 会校验
生成产物是否已提交。

## 协议与 context

lego v5 的 `challenge.Provider` 使用 `context.Context`，插件协议 v1 的
`present` / `cleanup` 没有跨进程 context。`dnsv5.Wrap` 用
`context.Background()` 适配；超时和进程终止仍由宿主控制。

## 构建标签

v5 helper 使用与上游 lego v5 release 相同的 DCE 标签：

```text
urfave_cli_no_template,ozzo_validation_no_template,grpcnotrace,validator_novalidatefn
```

v5 provider 的依赖会引入 validator/ozzo 的模板代码，加上这些标签后
linux/amd64 二进制约从 59 MiB 降到 37 MiB。`scripts/pluginctl` 在构建
`SDK=lego-v5` 的包时会自动追加这些标签。

## 安装与回退

`all-lego-v5` 的 provider 集合是旧 `all-lego` / `all-native` 的超集，因此从
旧包升级到 v5 时，宿主可以回收旧 helper。反向回退时请先停用或卸载
`all-lego-v5`，再安装 v4 包；否则旧包无法覆盖 v5 新增的 41 个 provider，
两个 helper 会同时存在并产生重复注册错误。

## 版本

`all-lego-v5` 的版本直接跟随 `go.mod` 中 `github.com/go-acme/lego/v5`，
当前为 `5.6.0`，不参与内容指纹递增，也不会被仓库发布 tag 覆盖。
