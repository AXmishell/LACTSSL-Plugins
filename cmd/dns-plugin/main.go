// Command dns-plugin 是单服务商 DNS 插件入口。
//
// 构建时必须通过构建标签只导入一个服务商，例如：
//
//	go build -tags plugin_dns_cloudflare -o lactssl-dns-cloudflare ./cmd/dns-plugin
//
// manifest 会校验注册表中恰好有一个服务商；present/cleanup 的行为与 bundle helper 一致。
package main

import (
	"fmt"
	"os"

	"github.com/AXmishell/LACTSSL-Plugins/catalog"
	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/i18n"
	"github.com/AXmishell/LACTSSL-Plugins/protocol"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/helper"
	"github.com/go-acme/lego/v4/challenge"
)

func main() {
	helper.ApplyMemoryLimit()
	if len(os.Args) < 2 {
		helper.Fail("用法: dns-plugin manifest|present|cleanup")
	}
	switch os.Args[1] {
	case "manifest", "list":
		writeManifest()
	case "present", "cleanup":
		handle(os.Args[1])
	default:
		helper.Fail("未知子命令: " + os.Args[1])
	}
}

func buildManifest() protocol.DNSManifest {
	items := dns.List()
	if len(items) != 1 {
		helper.Failf("插件构建必须且只能包含一个 DNS 服务商，当前为 %d 个", len(items))
	}
	plugin := items[0]
	def, ok := catalog.LookupDNS(plugin.ID())
	if !ok {
		helper.Fail("服务商元数据缺失: " + plugin.ID())
	}
	return protocol.DNSManifest{
		Protocol: protocol.ProtocolVersion,
		Name:     "lactssl dns plugin " + plugin.ID(),
		Version:  serviceVersion(),
		Providers: []protocol.DNSProvider{{
			ID:          plugin.ID(),
			Name:        def.LocalizedName(i18n.Default),
			Description: def.LocalizedDesc(i18n.Default),
			Aliases:     plugin.Aliases(),
			Fields:      manifestFields(def.Fields),
		}},
	}
}

func writeManifest() {
	if err := helper.WriteJSON(buildManifest()); err != nil {
		helper.Fail("输出 manifest 失败: " + err.Error())
	}
}

func manifestFields(fields []catalog.Field) []protocol.DNSField {
	if len(fields) == 0 {
		return nil
	}
	out := make([]protocol.DNSField, 0, len(fields))
	for _, field := range fields {
		item := protocol.DNSField{
			Key:         field.Key,
			Label:       field.LocalizedLabel(i18n.Default),
			Type:        field.Type,
			Required:    field.Required,
			Secret:      field.Secret,
			Placeholder: field.LocalizedPlaceholder(i18n.Default),
			Help:        field.LocalizedHelp(i18n.Default),
			Default:     field.Default,
		}
		for _, option := range field.Options {
			item.Options = append(item.Options, protocol.DNSOption{
				Label: option.LocalizedLabel(i18n.Default),
				Value: option.Value,
			})
		}
		if field.ShowIf != nil {
			item.ShowIf = &protocol.DNSCondition{Key: field.ShowIf.Key, Value: field.ShowIf.Value}
		}
		out = append(out, item)
	}
	return out
}

func handle(action string) {
	var req protocol.DNSRequest
	if err := helper.ReadJSON(&req); err != nil {
		helper.Fail("解析请求失败: " + err.Error())
	}
	if req.Provider != "" && req.Provider != dns.List()[0].ID() {
		helper.Fail("插件不提供该服务商: " + req.Provider)
	}
	items := dns.List()
	if len(items) != 1 {
		helper.Failf("插件构建必须且只能包含一个 DNS 服务商，当前为 %d 个", len(items))
	}
	plugin := items[0]
	def, ok := catalog.LookupDNS(plugin.ID())
	if !ok {
		helper.Fail("服务商元数据缺失: " + plugin.ID())
	}
	for key, value := range catalog.EnvForProvider(def, req.Config) {
		if value == "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			helper.Fail("设置环境变量失败: " + err.Error())
		}
	}
	provider, err := plugin.New(req.Config)
	if err != nil {
		helper.Fail("初始化服务商失败: " + err.Error())
	}
	if err := run(provider, action, req); err != nil {
		helper.Fail(err.Error())
	}
}

func run(provider challenge.Provider, action string, req protocol.DNSRequest) error {
	switch action {
	case "present":
		return provider.Present(req.Domain, req.Token, req.KeyAuth)
	case "cleanup":
		return provider.CleanUp(req.Domain, req.Token, req.KeyAuth)
	default:
		return fmt.Errorf("未知动作: %s", action)
	}
}

// buildVersion 由 pluginctl 通过 -ldflags 注入，必须与仓库索引中的版本一致。
var buildVersion = ""

func serviceVersion() string {
	if buildVersion != "" {
		return buildVersion
	}
	return "dev"
}
