package channels

import (
	"github.com/AXmishell/LACTSSL-Plugins/notify"
)

func init() {
	registerChannel(newChannel("openilink", []notify.Field{
		text("url", "notifications.fieldUrl", true),
		password("token", "notifications.fieldToken", false),
	}, sendSimpleBearerNamed("openilink")))
}
