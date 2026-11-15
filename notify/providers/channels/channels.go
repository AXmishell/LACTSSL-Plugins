package channels

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/i18n"
	"github.com/AXmishell/LACTSSL-Plugins/notify"
	"github.com/AXmishell/LACTSSL-Plugins/outbound"
)

var all = []channel{
	newChannel("gotify", []notify.Field{
		text("url", "notifications.fieldServerUrl", true),
		password("token", "notifications.fieldToken", true),
		number("priority", "notifications.fieldPriority", 5),
	}, sendGotify),
	newChannel("ntfy", []notify.Field{
		text("url", "notifications.fieldServerUrl", false),
		text("topic", "notifications.fieldTopic", true),
		password("token", "notifications.fieldToken", false),
		number("priority", "notifications.fieldPriority", 3),
	}, sendNtfy),
	newChannel("gocqhttp", []notify.Field{
		text("url", "notifications.fieldServerUrl", false),
		password("access_token", "notifications.fieldAccessToken", false),
		selectField("target_type", "notifications.fieldTargetType", "private",
			opt("private", "notifications.optionPrivate"),
			opt("group", "notifications.optionGroup"),
		),
		text("target_id", "notifications.fieldTargetId", true),
	}, sendGoCQHTTP),
	newChannel("serverchan", []notify.Field{
		password("sendkey", "notifications.fieldSendKey", true),
		selectField("version", "notifications.fieldVersion", "auto",
			opt("auto", "notifications.serverchanVersionAuto"),
			opt("turbo", "notifications.serverchanVersionTurbo"),
			opt("sc3", "notifications.serverchanVersionSC3"),
		),
	}, sendServerChan),
	newChannel("pushdeer", []notify.Field{
		text("url", "notifications.fieldServerUrl", false),
		password("pushkey", "notifications.fieldPushKey", true),
	}, sendPushDeer),
	newChannel("bark", []notify.Field{
		text("url", "notifications.fieldServerUrl", false),
		password("key", "notifications.fieldKey", true),
		text("sound", "notifications.fieldSound", false),
		text("group", "notifications.fieldGroup", false),
	}, sendBark),
	newChannel("telegram", []notify.Field{
		password("bot_token", "notifications.fieldBotToken", true),
		text("chat_id", "notifications.fieldChatId", true),
	}, sendTelegram),
	newChannel("dingtalk", []notify.Field{
		password("webhook", "notifications.fieldWebhook", true),
		password("secret", "notifications.fieldSecret", false),
	}, sendDingTalk),
	newChannel("wecom_bot", []notify.Field{
		password("webhook", "notifications.fieldWebhook", true),
	}, sendWecomBot),
	newChannel("wecom_app", []notify.Field{
		text("corp_id", "notifications.fieldCorpId", true),
		password("corp_secret", "notifications.fieldCorpSecret", true),
		number("agent_id", "notifications.fieldAgentId", 0),
		text("to_user", "notifications.fieldToUser", false),
	}, sendWecomApp),
	newChannel("wecom_smart", []notify.Field{
		text("url", "notifications.fieldUrl", true),
		password("token", "notifications.fieldToken", false),
	}, sendSimpleBearerNamed("wecom_smart")),
	newChannel("igot", []notify.Field{
		password("key", "notifications.fieldKey", true),
	}, sendIGot),
	newChannel("pushplus", []notify.Field{
		password("token", "notifications.fieldToken", true),
		text("topic", "notifications.fieldTopic", false),
	}, sendPushPlus),
	newChannel("weplus", []notify.Field{
		text("url", "notifications.fieldUrl", true),
		password("token", "notifications.fieldToken", false),
	}, sendSimpleBearerNamed("weplus")),
	newChannel("wxpusher", []notify.Field{
		password("app_token", "notifications.fieldAppToken", true),
		text("uids", "notifications.fieldUids", false),
		text("topic_ids", "notifications.fieldTopicIds", false),
	}, sendWxPusher),
	newChannel("wxpusher_spt", []notify.Field{
		password("spt", "notifications.fieldSpt", true),
	}, sendWxPusherSPT),
	newChannel("openilink", []notify.Field{
		text("url", "notifications.fieldUrl", true),
		password("token", "notifications.fieldToken", false),
	}, sendSimpleBearerNamed("openilink")),
	newChannel("wpush", []notify.Field{
		text("url", "notifications.fieldUrl", true),
		password("token", "notifications.fieldToken", false),
	}, sendSimpleBearerNamed("wpush")),
	newChannel("synology_chat", []notify.Field{
		password("url", "notifications.fieldWebhook", true),
	}, sendSynologyChat),
	newChannel("feishu", []notify.Field{
		password("webhook", "notifications.fieldWebhook", true),
		password("secret", "notifications.fieldSecret", false),
	}, sendFeishu),
	newChannel("pushme", []notify.Field{
		text("url", "notifications.fieldServerUrl", false),
		password("push_key", "notifications.fieldPushKey", true),
	}, sendPushMe),
	newChannel("chronocat", []notify.Field{
		text("url", "notifications.fieldServerUrl", false),
		password("token", "notifications.fieldToken", false),
		selectField("target_type", "notifications.fieldTargetType", "private",
			opt("private", "notifications.optionPrivate"),
			opt("group", "notifications.optionGroup"),
		),
		text("target", "notifications.fieldTarget", true),
	}, sendChronoCat),
	newChannel("custom", []notify.Field{
		text("url", "notifications.fieldUrl", true),
		selectField("method", "notifications.fieldMethod", "POST",
			opt("POST", "notifications.optionPost"),
			opt("GET", "notifications.optionGet"),
		),
		textarea("headers", "notifications.fieldHeaders", `{"Content-Type":"application/json"}`),
		textarea("body", "notifications.fieldBody", `{"title":"{title}","content":"{content}"}`),
	}, sendCustom),
}

func init() {
	for _, item := range all {
		notify.Register(item)
	}
}

func sendGotify(cfg map[string]any, msg notify.Message) error {
	base := cfgString(cfg, "url", "")
	token := cfgString(cfg, "token", "")
	if base == "" || token == "" {
		return missingChannelConfig("gotify")
	}
	endpoint := strings.TrimRight(base, "/") + "/message?token=" + url.QueryEscape(token)
	priority := cfgInt(cfg, "priority", 5)
	if priority < 0 || priority > 10 {
		priority = 5
	}
	return notifyJSON(endpoint, map[string]any{
		"title":    msg.Title,
		"message":  msg.Body,
		"priority": priority,
	}, nil)
}

func sendNtfy(cfg map[string]any, msg notify.Message) error {
	base := cfgString(cfg, "url", "https://ntfy.sh")
	topic := cfgString(cfg, "topic", "")
	if topic == "" {
		return missingChannelConfig("ntfy")
	}
	headers := map[string]string{
		"Title":    msg.Title,
		"Priority": strconv.Itoa(cfgInt(cfg, "priority", 3)),
	}
	for key, value := range bearer(cfgString(cfg, "token", "")) {
		headers[key] = value
	}
	return notifyHTTP(http.MethodPost, strings.TrimRight(base, "/")+"/"+url.PathEscape(topic), []byte(msg.Body), "text/plain", headers)
}

func sendGoCQHTTP(cfg map[string]any, msg notify.Message) error {
	base := cfgString(cfg, "url", "http://127.0.0.1:5700")
	targetID := cfgString(cfg, "target_id", "")
	if targetID == "" {
		return missingChannelConfig("gocqhttp")
	}
	endpoint := strings.TrimRight(base, "/") + "/send_private_msg"
	key := "user_id"
	if cfgString(cfg, "target_type", "private") == "group" {
		endpoint = strings.TrimRight(base, "/") + "/send_group_msg"
		key = "group_id"
	}
	return notifyJSON(endpoint, map[string]any{key: targetID, "message": channelMessage(msg.Title, msg.Body)}, bearer(cfgString(cfg, "access_token", "")))
}

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

func sendServerChan(cfg map[string]any, msg notify.Message) error {
	sendKey := cfgString(cfg, "sendkey", "")
	if sendKey == "" {
		return i18n.E("service.notify.serverchan_not_configured")
	}
	endpoint, err := serverChanEndpoint(sendKey, cfgString(cfg, "version", "auto"))
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("title", msg.Title)
	form.Set("desp", msg.Body)
	resp, err := outbound.NewClient(15*time.Second, false).PostForm(endpoint, form)
	if err != nil {
		return i18n.E("service.notify.serverchan_request_failed", "error", outbound.SafeError(err))
	}
	defer resp.Body.Close()
	body, _ := readLimited(resp.Body, 2048)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text := strings.TrimSpace(string(body))
		if text == "" {
			text = resp.Status
		}
		return i18n.E("service.notify.serverchan_failed", "error", text)
	}
	var result struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err == nil && result.Code != nil && *result.Code != 0 {
		text := strings.TrimSpace(result.Message)
		if text == "" {
			text = strconv.Itoa(*result.Code)
		}
		return i18n.E("service.notify.serverchan_failed", "error", text)
	}
	return nil
}

func sendPushDeer(cfg map[string]any, msg notify.Message) error {
	base := cfgString(cfg, "url", "https://api2.pushdeer.com")
	pushKey := cfgString(cfg, "pushkey", "")
	if pushKey == "" {
		return missingChannelConfig("pushdeer")
	}
	return notifyForm(strings.TrimRight(base, "/")+"/message/push", url.Values{
		"pushkey": {pushKey},
		"text":    {msg.Title},
		"desp":    {msg.Body},
	}, nil)
}

func sendBark(cfg map[string]any, msg notify.Message) error {
	base := cfgString(cfg, "url", "https://api.day.app")
	key := cfgString(cfg, "key", "")
	if key == "" {
		return missingChannelConfig("bark")
	}
	payload := map[string]any{"title": msg.Title, "body": msg.Body}
	if sound := cfgString(cfg, "sound", ""); sound != "" {
		payload["sound"] = sound
	}
	if group := cfgString(cfg, "group", ""); group != "" {
		payload["group"] = group
	}
	return notifyJSON(strings.TrimRight(base, "/")+"/"+url.PathEscape(key), payload, nil)
}

func sendTelegram(cfg map[string]any, msg notify.Message) error {
	token := cfgString(cfg, "bot_token", "")
	chatID := cfgString(cfg, "chat_id", "")
	if token == "" || chatID == "" {
		return missingChannelConfig("telegram")
	}
	return notifyJSON("https://api.telegram.org/bot"+token+"/sendMessage", map[string]any{
		"chat_id":                  chatID,
		"text":                     channelMessage(msg.Title, msg.Body),
		"disable_web_page_preview": true,
	}, nil)
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

func sendWecomApp(cfg map[string]any, msg notify.Message) error {
	corpID := cfgString(cfg, "corp_id", "")
	corpSecret := cfgString(cfg, "corp_secret", "")
	agentID := cfgInt(cfg, "agent_id", 0)
	if corpID == "" || corpSecret == "" || agentID == 0 {
		return missingChannelConfig("wecom_app")
	}
	tokenURL := "https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=" + url.QueryEscape(corpID) + "&corpsecret=" + url.QueryEscape(corpSecret)
	resp, err := outbound.NewClient(15*time.Second, false).Get(tokenURL)
	if err != nil {
		return i18n.E("service.notify.channel_request_failed", "error", outbound.SafeError(err))
	}
	defer resp.Body.Close()
	var tokenResult struct {
		AccessToken string `json:"access_token"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResult); err != nil {
		return err
	}
	if tokenResult.AccessToken == "" {
		return i18n.E("service.notify.channel_failed", "error", tokenResult.ErrMsg)
	}
	toUser := cfgString(cfg, "to_user", "@all")
	return notifyJSON("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token="+url.QueryEscape(tokenResult.AccessToken), map[string]any{
		"touser":  toUser,
		"msgtype": "text",
		"agentid": agentID,
		"text":    map[string]any{"content": channelMessage(msg.Title, msg.Body)},
	}, nil)
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

func sendIGot(cfg map[string]any, msg notify.Message) error {
	key := cfgString(cfg, "key", "")
	if key == "" {
		return missingChannelConfig("igot")
	}
	return notifyForm("https://push.hellyw.com/"+url.PathEscape(key), url.Values{
		"title":   {msg.Title},
		"content": {msg.Body},
	}, nil)
}

func sendPushPlus(cfg map[string]any, msg notify.Message) error {
	token := cfgString(cfg, "token", "")
	if token == "" {
		return missingChannelConfig("pushplus")
	}
	payload := map[string]any{"token": token, "title": msg.Title, "content": msg.Body, "template": "markdown"}
	if topic := cfgString(cfg, "topic", ""); topic != "" {
		payload["topic"] = topic
	}
	return notifyJSON("https://www.pushplus.plus/send", payload, nil)
}

func sendWxPusher(cfg map[string]any, msg notify.Message) error {
	appToken := cfgString(cfg, "app_token", "")
	if appToken == "" {
		return missingChannelConfig("wxpusher")
	}
	return notifyJSON("https://wxpusher.zjiecode.com/api/send/message", map[string]any{
		"appToken":    appToken,
		"content":     channelMessage(msg.Title, msg.Body),
		"summary":     msg.Title,
		"contentType": 1,
		"uids":        cfgList(cfg, "uids"),
		"topicIds":    cfgList(cfg, "topic_ids"),
	}, nil)
}

func sendWxPusherSPT(cfg map[string]any, msg notify.Message) error {
	spt := cfgString(cfg, "spt", "")
	if spt == "" {
		return missingChannelConfig("wxpusher_spt")
	}
	return notifyJSON("https://wxpusher.zjiecode.com/api/send/message/simple-push", map[string]any{
		"spt":     spt,
		"content": channelMessage(msg.Title, msg.Body),
		"summary": msg.Title,
	}, nil)
}

func sendSynologyChat(cfg map[string]any, msg notify.Message) error {
	endpoint := cfgString(cfg, "url", "")
	if endpoint == "" {
		return missingChannelConfig("synology_chat")
	}
	payload, _ := json.Marshal(map[string]string{"text": channelMessage(msg.Title, msg.Body)})
	return notifyForm(endpoint, url.Values{"payload": {string(payload)}}, nil)
}

func sendFeishu(cfg map[string]any, msg notify.Message) error {
	webhook := cfgString(cfg, "webhook", "")
	if webhook == "" {
		return missingChannelConfig("feishu")
	}
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]any{"text": channelMessage(msg.Title, msg.Body)},
	}
	if secret := cfgString(cfg, "secret", ""); secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(timestamp+"\n"+secret))
		mac.Write([]byte{})
		payload["timestamp"] = timestamp
		payload["sign"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	}
	return notifyJSON(webhook, payload, nil)
}

func sendPushMe(cfg map[string]any, msg notify.Message) error {
	base := cfgString(cfg, "url", "https://push.i-i.me")
	key := cfgString(cfg, "push_key", "")
	if key == "" {
		return missingChannelConfig("pushme")
	}
	return notifyForm(strings.TrimRight(base, "/")+"/"+url.PathEscape(key), url.Values{
		"title":   {msg.Title},
		"content": {msg.Body},
	}, nil)
}

func sendChronoCat(cfg map[string]any, msg notify.Message) error {
	endpoint := cfgString(cfg, "url", "http://127.0.0.1:5500")
	target := cfgString(cfg, "target", "")
	if target == "" {
		return missingChannelConfig("chronocat")
	}
	return notifyJSON(strings.TrimRight(endpoint, "/")+"/api/message/send", map[string]any{
		"target":      target,
		"target_type": cfgString(cfg, "target_type", "private"),
		"message":     channelMessage(msg.Title, msg.Body),
	}, bearer(cfgString(cfg, "token", "")))
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
