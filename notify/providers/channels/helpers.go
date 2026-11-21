package channels

import (
	"regexp"
	"strings"

	"github.com/AXmishell/LACTSSL-Plugins/i18n"
	"github.com/AXmishell/LACTSSL-Plugins/notify"
)

var serverChanSC3KeyPattern = regexp.MustCompile(`^sctp(\d+)t`)

func serverChanEndpoint(sendKey, version string) (string, error) {
	switch version {
	case "turbo":
		return "https://sctapi.ftqq.com/" + sendKey + ".send", nil
	case "sc3":
		matches := serverChanSC3KeyPattern.FindStringSubmatch(strings.ToLower(sendKey))
		if len(matches) < 2 {
			return "", i18n.E("service.notify.serverchan_invalid_key")
		}
		return "https://" + matches[1] + ".push.ft07.com/send/" + sendKey + ".send", nil
	default:
		if matches := serverChanSC3KeyPattern.FindStringSubmatch(strings.ToLower(sendKey)); len(matches) >= 2 {
			return "https://" + matches[1] + ".push.ft07.com/send/" + sendKey + ".send", nil
		}
		return "https://sctapi.ftqq.com/" + sendKey + ".send", nil
	}
}

func sendSimpleBearerNamed(name string) func(cfg map[string]any, msg notify.Message) error {
	return func(cfg map[string]any, msg notify.Message) error {
		endpoint := cfgString(cfg, "url", "")
		if endpoint == "" {
			return missingChannelConfig(name)
		}
		return notifyJSON(endpoint, map[string]any{"title": msg.Title, "content": msg.Body}, bearer(cfgString(cfg, "token", "")))
	}
}
