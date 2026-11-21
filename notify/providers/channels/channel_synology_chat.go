package channels

import (
	"encoding/json"
	"net/url"

	"github.com/AXmishell/LACTSSL-Plugins/notify"
)

func init() {
	registerChannel(newChannel("synology_chat", []notify.Field{
		password("url", "notifications.fieldWebhook", true),
	}, sendSynologyChat))
}

func sendSynologyChat(cfg map[string]any, msg notify.Message) error {
	endpoint := cfgString(cfg, "url", "")
	if endpoint == "" {
		return missingChannelConfig("synology_chat")
	}
	payload, _ := json.Marshal(map[string]string{"text": channelMessage(msg.Title, msg.Body)})
	return notifyForm(endpoint, url.Values{"payload": {string(payload)}}, nil)
}
