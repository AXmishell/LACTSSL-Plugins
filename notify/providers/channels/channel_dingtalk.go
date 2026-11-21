package channels

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/notify"
)

func init() {
	registerChannel(newChannel("dingtalk", []notify.Field{
		password("webhook", "notifications.fieldWebhook", true),
		password("secret", "notifications.fieldSecret", false),
	}, sendDingTalk))
}

func sendDingTalk(cfg map[string]any, msg notify.Message) error {
	webhook := cfgString(cfg, "webhook", "")
	if webhook == "" {
		return missingChannelConfig("dingtalk")
	}
	if secret := cfgString(cfg, "secret", ""); secret != "" {
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(timestamp + "\n" + secret))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		separator := "?"
		if strings.Contains(webhook, "?") {
			separator = "&"
		}
		webhook += separator + "timestamp=" + timestamp + "&sign=" + url.QueryEscape(sign)
	}
	return notifyJSON(webhook, map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"title": msg.Title, "text": "### " + msg.Title + "\n\n" + msg.Body},
	}, nil)
}
