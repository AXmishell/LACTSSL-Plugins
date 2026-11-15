// Command notify-plugin 是单通知渠道插件入口。
//
// 构建时必须通过构建标签只注册一个渠道，例如：
//
//	go build -tags plugin_notify_telegram -o lactssl-notify-telegram ./cmd/notify-plugin
package main

import (
	"os"

	"github.com/AXmishell/LACTSSL-Plugins/i18n"
	"github.com/AXmishell/LACTSSL-Plugins/notify"
	"github.com/AXmishell/LACTSSL-Plugins/protocol"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/helper"
)

func main() {
	helper.ApplyMemoryLimit()
	if len(os.Args) < 2 {
		helper.Fail("用法: notify-plugin manifest|send")
	}
	switch os.Args[1] {
	case "manifest", "list":
		writeManifest()
	case "send":
		handle()
	default:
		helper.Fail("未知子命令: " + os.Args[1])
	}
}

func buildManifest() protocol.NotifyManifest {
	items := notify.List()
	if len(items) != 1 {
		helper.Failf("插件构建必须且只能包含一个通知渠道，当前为 %d 个", len(items))
	}
	def := items[0].Definition()
	return protocol.NotifyManifest{
		Protocol: protocol.ProtocolVersion,
		Name:     "lactssl notify plugin " + def.ID,
		Version:  serviceVersion(),
		Channels: []protocol.NotifyChannel{{
			ID:          def.ID,
			Name:        def.Name,
			Description: def.Description,
			Aliases:     def.Aliases,
			Fields:      def.Fields,
		}},
	}
}

func writeManifest() {
	if err := helper.WriteJSON(buildManifest()); err != nil {
		helper.Fail("输出 manifest 失败: " + err.Error())
	}
}

func handle() {
	var req protocol.NotifyRequest
	if err := helper.ReadJSON(&req); err != nil {
		helper.Fail("解析请求失败: " + err.Error())
	}
	if req.Protocol != "" && req.Protocol != protocol.ProtocolVersion {
		helper.Fail("不支持的插件协议: " + req.Protocol)
	}
	channel, ok := notify.Lookup(req.Channel)
	if !ok {
		helper.Fail("未知通知渠道: " + req.Channel)
	}
	if err := channel.Notify(req.Config, notify.Message{Title: req.Title, Body: req.Body}); err != nil {
		fail(err.Error())
	}
	writeResult(true, "")
}

func writeResult(ok bool, message string) {
	_ = helper.WriteJSON(map[string]any{"ok": ok, "message": message})
}

// buildVersion 由 pluginctl 通过 -ldflags 注入，必须与仓库索引中的版本一致。
var buildVersion = ""

func serviceVersion() string {
	if buildVersion != "" {
		return buildVersion
	}
	return "dev"
}

func fail(message string) {
	writeResult(false, message)
	helper.Fail(i18n.T(i18n.Default, "plugin.notify.failed", map[string]any{"error": message}))
}
