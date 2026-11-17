// Package aliyun 是合并后的阿里云部署目标分派器。
//
// 规范 ID aliyun 通过 target 选择 CDN、DCDN、ESA、OSS 或 WAF，
// 具体实现仍由 aliyuncdn、aliyuncloud、aliyunoss 提供，历史 ID 保持可用。
package aliyun

import (
	"context"
	"strings"

	"github.com/AXmishell/LACTSSL-Plugins/deploy"
	"github.com/AXmishell/LACTSSL-Plugins/deploy/providers/aliyuncdn"
	"github.com/AXmishell/LACTSSL-Plugins/deploy/providers/aliyuncloud"
	"github.com/AXmishell/LACTSSL-Plugins/deploy/providers/aliyunoss"
	"github.com/AXmishell/LACTSSL-Plugins/deploy/util"
	"github.com/AXmishell/LACTSSL-Plugins/i18n"
)

type plugin struct{}

func (plugin) ID() string        { return "aliyun" }
func (plugin) Aliases() []string { return nil }

func (plugin) Deploy(_ context.Context, req deploy.Request) error {
	cert := util.Certificate(req.Certificate)
	cfg := util.Merge(req.Auth, req.Config)
	switch strings.TrimSpace(cfg["target"]) {
	case "", "cdn":
		return aliyuncdn.DeployCDN(cert, cfg)
	case "dcdn":
		return aliyuncloud.DeployDCDN(cert, cfg)
	case "esa":
		return aliyuncloud.DeployESA(cert, cfg)
	case "oss":
		return aliyunoss.DeployOSS(cert, cfg)
	case "waf":
		return aliyuncloud.DeployWAF(cert, cfg)
	default:
		return i18n.E("service.deploy.deploy_target_invalid", "type", cfg["target"])
	}
}

func init() {
	deploy.Register(plugin{})
}
