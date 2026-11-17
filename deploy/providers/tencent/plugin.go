// Package tencent 通过腾讯云 SSL 证书服务把证书部署到 CDN 或 COS。
//
// 对应 certd 的「腾讯云-部署到CDN-v2」与「腾讯云-部署证书到COS」插件。
package tencent

import (
	"context"
	"fmt"
	"strings"

	"github.com/AXmishell/LACTSSL-Plugins/deploy"
	"github.com/AXmishell/LACTSSL-Plugins/deploy/util"
	"github.com/AXmishell/LACTSSL-Plugins/i18n"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/tencentcloud"
)

type tencentPlugin struct{}
type cdnPlugin struct{}
type cosPlugin struct{}
type wafPlugin struct{}
type teoPlugin struct{}

func (tencentPlugin) ID() string        { return "tencent" }
func (tencentPlugin) Aliases() []string { return nil }
func (tencentPlugin) Deploy(_ context.Context, req deploy.Request) error {
	cfg := util.Merge(req.Auth, req.Config)
	target := strings.TrimSpace(cfg["target"])
	switch target {
	case "", "cdn", "cos", "teo", "waf":
		if target == "" {
			target = "cdn"
		}
		return deployTencent(target, util.Certificate(req.Certificate), cfg)
	default:
		return i18n.E("service.deploy.deploy_target_invalid", "type", target)
	}
}

func (cdnPlugin) ID() string        { return "tencent_cdn" }
func (cdnPlugin) Aliases() []string { return nil }
func (cdnPlugin) Deploy(_ context.Context, req deploy.Request) error {
	return deployTencent("cdn", util.Certificate(req.Certificate), util.Merge(req.Auth, req.Config))
}

func (cosPlugin) ID() string        { return "tencent_cos" }
func (cosPlugin) Aliases() []string { return nil }
func (cosPlugin) Deploy(_ context.Context, req deploy.Request) error {
	return deployTencent("cos", util.Certificate(req.Certificate), util.Merge(req.Auth, req.Config))
}

func (wafPlugin) ID() string        { return "tencent_waf" }
func (wafPlugin) Aliases() []string { return nil }
func (wafPlugin) Deploy(_ context.Context, req deploy.Request) error {
	return deployTencent("waf", util.Certificate(req.Certificate), util.Merge(req.Auth, req.Config))
}

func (teoPlugin) ID() string        { return "tencent_teo" }
func (teoPlugin) Aliases() []string { return nil }
func (teoPlugin) Deploy(_ context.Context, req deploy.Request) error {
	return deployTencent("teo", util.Certificate(req.Certificate), util.Merge(req.Auth, req.Config))
}

func init() {
	deploy.Register(tencentPlugin{})
	deploy.Register(cdnPlugin{})
	deploy.Register(cosPlugin{})
	deploy.Register(wafPlugin{})
	deploy.Register(teoPlugin{})
}

// 测试时可替换为本地服务地址。
var tencentSSLBaseURL = ""

const (
	sslService = "ssl"
	sslVersion = "2019-12-05"
)

func deployTencent(target string, cert *deploy.Certificate, cfg map[string]string) error {
	secretID := strings.TrimSpace(cfg["secret_id"])
	secretKey := cfg["secret_key"]
	if secretID == "" || secretKey == "" {
		return i18n.E("service.deploy.tencent_key_required")
	}
	domains := util.SplitList(cfg["domains"])
	if len(domains) == 0 {
		domains = util.Domains(cert)
	}
	if len(domains) == 0 {
		return i18n.E("service.deploy.cdn_domains_empty")
	}
	region := strings.TrimSpace(cfg["region"])
	if target == "cos" {
		if region == "" {
			return i18n.E("service.deploy.tencent_cos_region_required")
		}
		bucket := strings.TrimSpace(cfg["bucket"])
		if bucket == "" {
			return i18n.E("service.deploy.tencent_cos_bucket_required")
		}
	}

	client := &tencentcloud.Client{
		Credentials: tencentcloud.Credentials{SecretID: secretID, SecretKey: secretKey},
		HTTP:        util.Client(cfg),
	}
	switch {
	case tencentSSLBaseURL != "":
		client.Endpoint = tencentSSLBaseURL
	case util.Bool(cfg["intl"]):
		client.Endpoint = "https://ssl.intl.tencentcloudapi.com"
	default:
		// SSL 是全局服务，region 只作为 X-TC-Region 传递，不能拼进端点。
		client.Endpoint = "https://ssl.tencentcloudapi.com"
	}

	name := strings.TrimSpace(cfg["cert_name"])
	if name == "" {
		name = util.CertName(cert, "lactssl-"+util.CleanName(util.FirstDomain(cert)))
	}
	var uploaded struct {
		CertificateID string `json:"CertificateId"`
	}
	if _, err := client.Do(context.Background(), sslService, region, "UploadCertificate", sslVersion, map[string]any{
		"CertificatePublicKey":  cert.FullchainPEM,
		"CertificatePrivateKey": cert.KeyPEM,
		"Alias":                 name,
	}, &uploaded); err != nil {
		return err
	}
	if uploaded.CertificateID == "" {
		return i18n.E("service.deploy.tencent_cert_id_missing")
	}

	instances := make([]string, 0, len(domains))
	for _, domain := range domains {
		if target == "cos" {
			instances = append(instances, fmt.Sprintf("%s#%s#%s", region, strings.TrimSpace(cfg["bucket"]), domain))
			continue
		}
		instances = append(instances, domain)
	}
	if _, err := client.Do(context.Background(), sslService, region, "DeployCertificateInstance", sslVersion, map[string]any{
		"CertificateId":  uploaded.CertificateID,
		"ResourceType":   target,
		"Status":         1,
		"InstanceIdList": instances,
	}, nil); err != nil {
		return err
	}
	return nil
}
