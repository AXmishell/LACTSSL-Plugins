// Package helper 提供官方插件 helper 的启动与协议辅助。
//
// 三个 helper 共用同一套内存预算、JSON 编解码与错误退出约定，
// 避免实现随时间产生差异。
package helper

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/AXmishell/LACTSSL-Plugins/protocol"
)

// ApplyMemoryLimit 根据主程序传入的预算设置 Go 堆软上限。
// 硬性的常驻内存限制由主程序轮询 RSS 并在超限时结束进程实现，
// 这里不使用 RLIMIT_AS：Go 运行时需要预留大量虚拟地址空间。
func ApplyMemoryLimit() {
	raw := strings.TrimSpace(os.Getenv(protocol.MemoryLimitEnv))
	if raw == "" {
		return
	}
	limit, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || limit <= 0 {
		return
	}
	debug.SetMemoryLimit(limit * 4 / 5)
}

// Fail 把错误写入 stderr 并以非 0 退出。
func Fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}

// Failf 是 Fail 的格式化版本。
func Failf(format string, args ...any) {
	Fail(fmt.Sprintf(format, args...))
}

// WriteJSON 把 v 编码到 stdout，保留非 ASCII 字符且不转义 HTML。
func WriteJSON(v any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(v)
}

// ReadJSON 从 stdin 解码一个 JSON 对象。
func ReadJSON(v any) error {
	return json.NewDecoder(os.Stdin).Decode(v)
}

// HasFeature 判断主程序是否声明了某个部署插件特性。
func HasFeature(name string) bool {
	for _, item := range strings.Split(os.Getenv(protocol.DeployFeaturesEnv), ",") {
		if strings.TrimSpace(item) == name {
			return true
		}
	}
	return false
}
