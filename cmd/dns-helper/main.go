// Command dns-helper 是 LACTSSL DNS 插件协议的官方实现。
//
// 主程序通过一次性子进程调用它：
//
//	dns-helper manifest                 # 输出插件清单 JSON
//	dns-helper present                  # 从 stdin 读取 JSON 请求并添加 TXT 记录
//	dns-helper cleanup                  # 从 stdin 读取 JSON 请求并删除 TXT 记录
//	dns-helper verify                   # 从 stdin 读取 JSON 请求并执行只读探测
//
// 内存上限由主程序通过 LACTSSL_PLUGIN_MEMORY_LIMIT 传入。
package main

import (
	"context"
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
		helper.Fail("用法: dns-helper manifest|present|cleanup|verify")
	}
	switch os.Args[1] {
	case "manifest", "list":
		writeManifest()
	case "present", "cleanup", "verify":
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
		Protocol:     protocol.ProtocolVersion,
		Capabilities: []string{protocol.FeatureVerify},
		Name:         "lactssl dns helper",
		Version:      serviceVersion(),
		Providers:    make([]protocol.DNSProvider, 0, len(dns.List())),
	}
	for _, plugin := range dns.List() {
		def, ok := catalog.LookupDNS(plugin.ID())
		if !ok {
			continue
		}
		manifest.Providers = append(manifest.Providers, protocol.DNSProvider{
			ID:              plugin.ID(),
			Name:            def.LocalizedName(i18n.Default),
			NameI18n:        localizedTextMap(func(locale i18n.Locale) string { return def.LocalizedName(locale) }),
			Description:     def.LocalizedDesc(i18n.Default),
			DescriptionI18n: localizedTextMap(func(locale i18n.Locale) string { return def.LocalizedDesc(locale) }),
			Aliases:         plugin.Aliases(),
			Fields:          manifestFields(def.Fields),
			Capabilities:    providerCapabilities(plugin.ID()),
		})
	}
	return manifest
}

// providerCapabilities 只给实现了只读探测的服务商声明 verify-v1。
func providerCapabilities(id string) []string {
	if dns.IsVerifiable(id) {
		return []string{protocol.FeatureVerify}
	}
	return nil
}

func manifestFields(fields []catalog.Field) []protocol.DNSField {
	if len(fields) == 0 {
		return nil
	}
	out := make([]protocol.DNSField, 0, len(fields))
	for _, field := range fields {
		item := protocol.DNSField{
			Key:             field.Key,
			Label:           field.LocalizedLabel(i18n.Default),
			LabelI18n:       localizedTextMap(func(locale i18n.Locale) string { return field.LocalizedLabel(locale) }),
			Type:            field.Type,
			Required:        field.Required,
			Secret:          field.Secret,
			Placeholder:     field.LocalizedPlaceholder(i18n.Default),
			PlaceholderI18n: localizedTextMap(func(locale i18n.Locale) string { return field.LocalizedPlaceholder(locale) }),
			Help:            field.LocalizedHelp(i18n.Default),
			HelpI18n:        localizedTextMap(func(locale i18n.Locale) string { return field.LocalizedHelp(locale) }),
			Default:         field.Default,
		}
		for _, option := range field.Options {
			item.Options = append(item.Options, protocol.DNSOption{
				Label:     option.LocalizedLabel(i18n.Default),
				LabelI18n: localizedTextMap(func(locale i18n.Locale) string { return option.LocalizedLabel(locale) }),
				Value:     option.Value,
			})
		}
		if field.ShowIf != nil {
			item.ShowIf = &protocol.DNSCondition{Key: field.ShowIf.Key, Value: field.ShowIf.Value}
		}
		out = append(out, item)
	}
	return out
}

// localizedTextMap 展开全部受支持语言；默认语言条目始终存在。
// 没有翻译时 Localized* 会回退到默认语言。
func localizedTextMap(text func(i18n.Locale) string) map[string]string {
	locales := i18n.Supported()
	out := make(map[string]string, len(locales))
	for _, locale := range locales {
		out[string(locale)] = text(locale)
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
	if action == "verify" {
		handleVerify(provider, req)
		return
	}
	if err := run(provider, action, req); err != nil {
		helper.Fail(err.Error())
	}
}

// handleVerify 执行只读探测。未实现 Verifier 的服务商返回结构化 unsupported，
// 让宿主在 auto 模式下回退到 DNS-01 写入测试。
func handleVerify(provider dns.ChallengeProvider, req protocol.DNSRequest) {
	loc := i18n.Normalize(firstNonEmpty(req.Locale, string(i18n.Default)))
	response := protocol.VerifyResponse{OK: true, Checks: make([]protocol.VerifyCheck, 0, 2)}
	verifier, ok := provider.(dns.Verifier)
	if !ok {
		response.Checks = append(response.Checks, protocol.VerifyCheck{
			Key: "verify", Status: protocol.VerifyStatusUnsupported,
			Message: i18n.T(loc, "service.verify.unsupported", nil),
		})
		if err := helper.WriteJSON(response); err != nil {
			helper.Fail("输出验证结果失败: " + err.Error())
		}
		return
	}
	zone, err := verifier.Verify(context.Background(), req.Domain)
	if err != nil {
		response.OK = false
		response.Checks = append(response.Checks, protocol.VerifyCheck{
			Key: "zone", Status: protocol.VerifyStatusFailed,
			Message: i18n.T(loc, "service.verify.zone_failed", map[string]any{"domain": req.Domain, "error": err.Error()}),
		})
	} else {
		response.Checks = append(response.Checks, protocol.VerifyCheck{
			Key: "zone", Status: protocol.VerifyStatusOK,
			Message: i18n.T(loc, "service.verify.zone_ok", map[string]any{"domain": req.Domain, "zone": zone}),
		})
	}
	if err := helper.WriteJSON(response); err != nil {
		helper.Fail("输出验证结果失败: " + err.Error())
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
