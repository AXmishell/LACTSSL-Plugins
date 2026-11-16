// Command dns-helper 是 LACTSSL DNS 插件协议的官方实现。
//
// 主程序通过一次性子进程调用它：
//
//	dns-helper manifest                 # 输出插件清单 JSON
//	dns-helper present                  # 从 stdin 读取 JSON 请求并添加 TXT 记录
//	dns-helper cleanup                  # 从 stdin 读取 JSON 请求并删除 TXT 记录
//
// 内存上限由主程序通过 LACTSSL_PLUGIN_MEMORY_LIMIT 传入。
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/AXmishell/LACTSSL-Plugins/catalog"
	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/i18n"
	"github.com/AXmishell/LACTSSL-Plugins/protocol"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/helper"
)

func main() {
	helper.ApplyMemoryLimit()
	if len(os.Args) < 2 {
		helper.Fail("用法: dns-helper manifest|present|cleanup")
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

func writeManifest() {
	if err := helper.WriteJSON(buildManifest()); err != nil {
		helper.Fail("输出 manifest 失败: " + err.Error())
	}
}

func buildManifest() protocol.DNSManifest {
	manifest := protocol.DNSManifest{
		Protocol:  protocol.ProtocolVersion,
		Name:      "lactssl dns helper",
		Version:   serviceVersion(),
		Providers: make([]protocol.DNSProvider, 0, len(dns.List())),
	}
	for _, plugin := range dns.List() {
		def, ok := catalog.LookupDNS(plugin.ID())
		if !ok {
			continue
		}
		manifest.Providers = append(manifest.Providers, protocol.DNSProvider{
			ID:          plugin.ID(),
			Name:        def.LocalizedName(i18n.Default),
			Description: def.LocalizedDesc(i18n.Default),
			Aliases:     plugin.Aliases(),
			Fields:      manifestFields(def.Fields),
		})
	}
	return manifest
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
	if req.Provider == "" {
		helper.Fail("请求缺少 provider")
	}
	plugin, ok := dns.Lookup(req.Provider)
	if !ok {
		helper.Fail("不支持的服务商: " + req.Provider)
	}
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

func run(provider dns.ChallengeProvider, action string, req protocol.DNSRequest) error {
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
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
