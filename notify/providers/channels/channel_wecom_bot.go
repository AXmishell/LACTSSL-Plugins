package channels

import (
	"github.com/AXmishell/LACTSSL-Plugins/notify"
)

func init() {
	registerChannel(newChannel("wecom_bot", []notify.Field{
		password("webhook", "notifications.fieldWebhook", true),
	}, sendWecomBot))
}

func sendWecomBot(cfg map[string]any, msg notify.Message) error {
	webhook := cfgString(cfg, "webhook", "")
	if webhook == "" {
		return missingChannelConfig("wecom_bot")
	}
	return notifyJSON(webhook, map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"content": msg.Title + "\n" + msg.Body},
	}, nil)
}
