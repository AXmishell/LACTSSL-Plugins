// Package catalog 保存官方 DNS / 部署插件共用的静态元数据。
//
// 数据从主仓库的 ProviderDef 导出，保证服务商 ID、字段 key、lego 环境变量
// 与历史版本逐项一致。主仓库通过 import 本包获得同一份数据，避免两处维护。
package catalog

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed data.json
var raw []byte

// Option 是下拉选项。
type Option struct {
	Label    string `json:"label,omitempty"`
	LabelKey string `json:"label_key,omitempty"`
	Value    string `json:"value"`
}

// Condition 描述字段显示条件。
type Condition struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Field 描述服务商或部署目标的配置字段。
type Field struct {
	Key            string `json:"key"`
	Label          string `json:"label,omitempty"`
	LabelKey       string `json:"label_key,omitempty"`
	Type           string `json:"type,omitempty"`
	Required       bool   `json:"required,omitempty"`
	Secret         bool   `json:"secret,omitempty"`
	Placeholder    string `json:"placeholder,omitempty"`
	PlaceholderKey string `json:"placeholder_key,omitempty"`
	Help           string `json:"help,omitempty"`
	HelpKey        string `json:"help_key,omitempty"`
	Default        string `json:"default,omitempty"`
	// Env 是字段对应的 lego 环境变量名，仅 DNS 服务商使用。
	Env     string     `json:"env,omitempty"`
	Options []Option   `json:"options,omitempty"`
	ShowIf  *Condition `json:"show_if,omitempty"`
}

// Provider 描述一个 DNS 服务商或部署目标。
type Provider struct {
	Value        string  `json:"value"`
	Label        string  `json:"label,omitempty"`
	LabelKey     string  `json:"label_key,omitempty"`
	Desc         string  `json:"desc,omitempty"`
	DescKey      string  `json:"desc_key,omitempty"`
	Deprecated   bool    `json:"deprecated,omitempty"`
	LegacyKeep   bool    `json:"legacy_keep,omitempty"`
	Fields       []Field `json:"fields,omitempty"`
	DeployFields []Field `json:"deploy_fields,omitempty"`
}

type data struct {
	DNS                   []Provider        `json:"dns"`
	Deploy                []Provider        `json:"deploy"`
	DeprecatedDeployTypes map[string]string `json:"deprecated_deploy_types"`
}

var loaded data

func init() {
	if err := json.Unmarshal(raw, &loaded); err != nil {
		panic("解析插件元数据失败: " + err.Error())
	}
}

// DNSProviders 返回全部官方 DNS 服务商元数据，顺序与历史版本一致。
func DNSProviders() []Provider {
	out := make([]Provider, len(loaded.DNS))
	copy(out, loaded.DNS)
	return out
}

// Deployers 返回全部官方部署目标元数据，顺序与历史版本一致。
func Deployers() []Provider {
	out := make([]Provider, len(loaded.Deploy))
	copy(out, loaded.Deploy)
	return out
}

// DeprecatedDeployTypes 返回已合并的旧部署目标 ID 到规范 ID 的映射。
func DeprecatedDeployTypes() map[string]string {
	out := make(map[string]string, len(loaded.DeprecatedDeployTypes))
	for key, value := range loaded.DeprecatedDeployTypes {
		out[key] = value
	}
	return out
}

// LookupDNS 按 ID 查找 DNS 服务商元数据。
func LookupDNS(id string) (Provider, bool) {
	return lookup(loaded.DNS, id)
}

// LookupDeployer 按 ID 查找部署目标元数据。
func LookupDeployer(id string) (Provider, bool) {
	return lookup(loaded.Deploy, id)
}

func lookup(list []Provider, id string) (Provider, bool) {
	id = normalize(id)
	for _, item := range list {
		if normalize(item.Value) == id {
			return item, true
		}
	}
	return Provider{}, false
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// EnvForProvider 把 DNS 服务商表单字段转换为 lego 环境变量。
func EnvForProvider(def Provider, cfg map[string]string) map[string]string {
	envs := make(map[string]string, len(def.Fields))
	for _, field := range def.Fields {
		if field.Env == "" {
			continue
		}
		envs[field.Env] = cfg[field.Key]
	}

	// Cloudflare 的 API Token 与 Global API Key 是两套互斥凭据。
	if def.Value == "cloudflare" {
		if strings.TrimSpace(cfg["api_token"]) != "" {
			delete(envs, "CLOUDFLARE_EMAIL")
			delete(envs, "CLOUDFLARE_API_KEY")
		} else {
			delete(envs, "CLOUDFLARE_DNS_API_TOKEN")
		}
	}
	return envs
}
