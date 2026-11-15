// Command deploy-plugin 是单部署目标插件入口。
//
// 构建时必须通过构建标签只注册一个部署目标，例如：
//
//	go build -tags plugin_deploy_ftp -o lactssl-deploy-ftp ./cmd/deploy-plugin
package main

import (
	"context"
	"os"

	"github.com/AXmishell/LACTSSL-Plugins/catalog"
	"github.com/AXmishell/LACTSSL-Plugins/deploy"
	"github.com/AXmishell/LACTSSL-Plugins/i18n"
	"github.com/AXmishell/LACTSSL-Plugins/protocol"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/helper"
)

func main() {
	helper.ApplyMemoryLimit()
	if len(os.Args) < 2 {
		helper.Fail("用法: deploy-plugin manifest|deploy")
	}
	switch os.Args[1] {
	case "manifest", "list":
		writeManifest()
	case "deploy":
		handle()
	default:
		helper.Fail("未知子命令: " + os.Args[1])
	}
}

// buildManifest 只输出规范目标，历史兼容 ID 不进入单插件包。
func buildManifest() protocol.DeployManifest {
	var manifest protocol.DeployManifest
	manifest.Protocol = protocol.ProtocolVersion
	manifest.Name = "lactssl deploy plugin"
	manifest.Version = serviceVersion()

	items := deploy.List()
	for _, plugin := range items {
		def, ok := catalog.LookupDeployer(plugin.ID())
		if !ok || def.Deprecated {
			continue
		}
		manifest.Deployers = append(manifest.Deployers, protocol.Deployer{
			ID:           plugin.ID(),
			Name:         def.LocalizedName(i18n.Default),
			Description:  def.LocalizedDesc(i18n.Default),
			Aliases:      plugin.Aliases(),
			Fields:       manifestFields(def.Fields),
			DeployFields: manifestFields(def.DeployFields),
		})
	}
	if len(manifest.Deployers) != 1 {
		helper.Failf("插件构建必须且只能包含一个部署目标，当前为 %d 个", len(manifest.Deployers))
	}
	manifest.Name += " " + manifest.Deployers[0].ID
	return manifest
}

func writeManifest() {
	if err := helper.WriteJSON(buildManifest()); err != nil {
		helper.Fail("输出 manifest 失败: " + err.Error())
	}
}

func manifestFields(fields []catalog.Field) []protocol.DeployField {
	if len(fields) == 0 {
		return nil
	}
	out := make([]protocol.DeployField, 0, len(fields))
	for _, field := range fields {
		item := protocol.DeployField{
			Key:         field.Key,
			Label:       field.LocalizedLabel(i18n.Default),
			Type:        field.Type,
			Required:    field.Required,
			Secret:      field.Secret,
			Placeholder: field.LocalizedPlaceholder(i18n.Default),
			Help:        field.LocalizedHelp(i18n.Default),
			Default:     field.Default,
		}
		for _, option := range field.Options {
			item.Options = append(item.Options, protocol.DeployOption{
				Label: option.LocalizedLabel(i18n.Default),
				Value: option.Value,
			})
		}
		if field.ShowIf != nil {
			item.ShowIf = &protocol.DeployCondition{Key: field.ShowIf.Key, Value: field.ShowIf.Value}
		}
		out = append(out, item)
	}
	return out
}

func handle() {
	var req protocol.DeployRequest
	if err := helper.ReadJSON(&req); err != nil {
		helper.Fail("解析请求失败: " + err.Error())
	}
	if req.Protocol != "" && req.Protocol != protocol.ProtocolVersion {
		helper.Fail("不支持的插件协议: " + req.Protocol)
	}
	plugin, ok := deploy.Lookup(req.Provider)
	if !ok {
		helper.Fail("不支持的部署目标: " + req.Provider)
	}
	if err := plugin.Deploy(context.Background(), deploy.Request{
		Auth:        req.Auth,
		Config:      req.Config,
		Certificate: req.Certificate,
	}); err != nil {
		helper.Fail(err.Error())
	}
}

// buildVersion 由 pluginctl 通过 -ldflags 注入，必须与仓库索引中的版本一致。
var buildVersion = ""

func serviceVersion() string {
	if buildVersion != "" {
		return buildVersion
	}
	return "dev"
}
