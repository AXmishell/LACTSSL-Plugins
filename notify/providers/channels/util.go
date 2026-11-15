// Package channels 提供官方通知 helper 内置的渠道实现。
package channels

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/i18n"
	"github.com/AXmishell/LACTSSL-Plugins/notify"
	"github.com/AXmishell/LACTSSL-Plugins/outbound"
)

func cfgString(cfg map[string]any, key, fallback string) string {
	value, ok := cfg[key]
	if !ok || value == nil {
		return fallback
	}
	switch item := value.(type) {
	case string:
		if strings.TrimSpace(item) == "" {
			return fallback
		}
		return strings.TrimSpace(item)
	case float64:
		return strconv.FormatFloat(item, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(item)
	default:
		return fmt.Sprint(item)
	}
}

func cfgInt(cfg map[string]any, key string, fallback int) int {
	raw := cfgString(cfg, key, "")
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func cfgBool(cfg map[string]any, key string) bool {
	switch value := cfg[key].(type) {
	case bool:
		return value
	case string:
		return value == "true"
	default:
		return false
	}
}

func cfgList(cfg map[string]any, key string) []string {
	switch value := cfg[key].(type) {
	case []any:
		list := make([]string, 0, len(value))
		for _, item := range value {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				list = append(list, text)
			}
		}
		return list
	case []string:
		return value
	case string:
		parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
		list := make([]string, 0, len(parts))
		for _, part := range parts {
			if text := strings.TrimSpace(part); text != "" {
				list = append(list, text)
			}
		}
		return list
	default:
		return nil
	}
}

func notifyHTTP(method, endpoint string, payload []byte, contentType string, headers map[string]string) error {
	if err := outbound.ValidateURL(endpoint); err != nil {
		return err
	}
	req, err := http.NewRequest(method, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := outbound.NewClient(15*time.Second, false).Do(req)
	if err != nil {
		return fmt.Errorf("%s", outbound.SafeError(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text := strings.TrimSpace(string(body))
		if text == "" {
			text = resp.Status
		}
		return fmt.Errorf("%s", text)
	}
	return nil
}

func notifyJSON(endpoint string, payload any, headers map[string]string) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return notifyHTTP(http.MethodPost, endpoint, data, "application/json", headers)
}

func notifyForm(endpoint string, values url.Values, headers map[string]string) error {
	return notifyHTTP(http.MethodPost, endpoint, []byte(values.Encode()), "application/x-www-form-urlencoded", headers)
}

func bearer(token string) map[string]string {
	if token == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

func missingChannelConfig(name string) error {
	return i18n.E("service.notify.channel_not_configured", "channel", name)
}

func channelMessage(title, body string) string {
	if strings.TrimSpace(title) == "" {
		return body
	}
	return title + "\n\n" + body
}

func escapeJSONString(value string) string {
	data, _ := json.Marshal(value)
	text := string(data)
	return strings.Trim(text, `"`)
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, limit))
}

// channel 用一份定义加发送函数实现 Notifier，避免为每个渠道维护样板类型。
type channel struct {
	def  notify.Definition
	send func(cfg map[string]any, msg notify.Message) error
}

func (c channel) Definition() notify.Definition { return c.def }
func (c channel) Notify(cfg map[string]any, msg notify.Message) error {
	return c.send(cfg, msg)
}

func newChannel(id string, fields []notify.Field, send func(cfg map[string]any, msg notify.Message) error) channel {
	return channel{
		def: notify.Definition{
			ID:     id,
			Name:   id,
			Fields: fields,
		},
		send: send,
	}
}

func text(key, labelKey string, required bool) notify.Field {
	return notify.Field{Key: key, LabelKey: labelKey, Type: "text", Required: required}
}

func password(key, labelKey string, required bool) notify.Field {
	return notify.Field{Key: key, LabelKey: labelKey, Type: "password", Secret: true, Required: required}
}

func number(key, labelKey string, def int) notify.Field {
	return notify.Field{Key: key, LabelKey: labelKey, Type: "number", Default: strconv.Itoa(def)}
}

func selectField(key, labelKey, def string, options ...notify.Option) notify.Field {
	return notify.Field{Key: key, LabelKey: labelKey, Type: "select", Default: def, Options: options}
}

func opt(value, labelKey string) notify.Option {
	return notify.Option{Value: value, LabelKey: labelKey}
}

func textarea(key, labelKey, placeholder string) notify.Field {
	return notify.Field{Key: key, LabelKey: labelKey, Type: "textarea", Placeholder: placeholder}
}
