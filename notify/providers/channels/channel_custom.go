package channels

import (
	"encoding/json"
	"strings"

	"github.com/AXmishell/LACTSSL-Plugins/notify"
)

func init() {
	registerChannel(newChannel("custom", []notify.Field{
		text("url", "notifications.fieldUrl", true),
		selectField("method", "notifications.fieldMethod", "POST",
			opt("POST", "notifications.optionPost"),
			opt("GET", "notifications.optionGet"),
		),
		textarea("headers", "notifications.fieldHeaders", `{"Content-Type":"application/json"}`),
		textarea("body", "notifications.fieldBody", `{"title":"{title}","content":"{content}"}`),
	}, sendCustom))
}

func sendCustom(cfg map[string]any, msg notify.Message) error {
	endpoint := cfgString(cfg, "url", "")
	if endpoint == "" {
		return missingChannelConfig("custom")
	}
	method := strings.ToUpper(cfgString(cfg, "method", "POST"))
	headers := map[string]string{}
	if raw := cfgString(cfg, "headers", ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &headers)
	}
	bodyTemplate := cfgString(cfg, "body", `{"title":"{title}","content":"{content}"}`)
	replacer := strings.NewReplacer(
		"{title}", escapeJSONString(msg.Title),
		"{content}", escapeJSONString(msg.Body),
	)
	rendered := replacer.Replace(bodyTemplate)
	contentType := headers["Content-Type"]
	if contentType == "" {
		contentType = "application/json"
	}
	return notifyHTTP(method, endpoint, []byte(rendered), contentType, headers)
}
