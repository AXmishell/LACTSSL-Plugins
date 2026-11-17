package catalog

import (
	"strings"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/i18n"
	legodns "github.com/go-acme/lego/v4/providers/dns"
)

func TestCounts(t *testing.T) {
	if got := len(DNSProviders()); got != 194 {
		t.Fatalf("dns providers = %d, want 194", got)
	}
	if got := len(Deployers()); got != 46 {
		t.Fatalf("deployers = %d, want 46", got)
	}
	if got := len(DeprecatedDeployTypes()); got != 21 {
		t.Fatalf("deprecated deploy types = %d, want 21", got)
	}
}

func TestDeployMergeGroups(t *testing.T) {
	groups := map[string][]string{
		"safeline":   {"waf", "panel", "portal"},
		"baota":      {"site", "panel", "docker"},
		"acepanel":   {"site", "panel"},
		"tencent":    {"cdn", "cos", "teo", "waf"},
		"aliyun":     {"cdn", "dcdn", "esa", "oss", "waf"},
		"volcengine": {"cdn", "dcdn"},
	}
	legacy := map[string]string{
		"safeline_panel": "safeline", "safeline_portal": "safeline",
		"baota_site": "baota", "baota_panel": "baota", "baota_docker": "baota",
		"acepanel_panel": "acepanel",
		"tencent_cdn":    "tencent", "tencent_cos": "tencent", "tencent_teo": "tencent", "tencent_waf": "tencent",
		"aliyun_cdn": "aliyun", "aliyun_dcdn": "aliyun", "aliyun_esa": "aliyun", "aliyun_oss": "aliyun", "aliyun_waf": "aliyun",
		"volcengine_cdn": "volcengine", "volcengine_dcdn": "volcengine",
	}
	deprecated := DeprecatedDeployTypes()
	for old, canonical := range legacy {
		if deprecated[old] != canonical {
			t.Errorf("deprecated %s = %q, want %q", old, deprecated[old], canonical)
		}
	}
	for id, values := range groups {
		def, ok := LookupDeployer(id)
		if !ok {
			t.Errorf("canonical deployer %s missing", id)
			continue
		}
		if def.Deprecated {
			t.Errorf("canonical deployer %s should not be deprecated", id)
		}
		var target *Field
		for index := range def.DeployFields {
			if def.DeployFields[index].Key == "target" {
				target = &def.DeployFields[index]
				break
			}
		}
		if target == nil {
			t.Errorf("canonical deployer %s has no target field", id)
			continue
		}
		got := make([]string, 0, len(target.Options))
		for _, option := range target.Options {
			got = append(got, option.Value)
		}
		if strings.Join(got, ",") != strings.Join(values, ",") {
			t.Errorf("%s target options = %v, want %v", id, got, values)
		}
	}
	if _, ok := deprecated["btwaf"]; ok {
		t.Fatal("btwaf must stay independent from baota")
	}
}

func TestLookupNormalizesID(t *testing.T) {
	if _, ok := LookupDNS(" Cloudflare "); !ok {
		t.Fatal("lookup should normalize ID")
	}
	if _, ok := LookupDeployer("QINIU"); !ok {
		t.Fatal("lookup should normalize deployer ID")
	}
}

func TestEnvForCloudflareTokenWins(t *testing.T) {
	def, ok := LookupDNS("cloudflare")
	if !ok {
		t.Fatal("cloudflare metadata missing")
	}
	envs := EnvForProvider(def, map[string]string{"api_token": "token", "email": "a@b.c", "api_key": "key"})
	if envs["CLOUDFLARE_DNS_API_TOKEN"] != "token" {
		t.Fatalf("token env = %q", envs["CLOUDFLARE_DNS_API_TOKEN"])
	}
	if _, ok := envs["CLOUDFLARE_EMAIL"]; ok {
		t.Fatal("email should be removed when token is set")
	}
	if _, ok := envs["CLOUDFLARE_API_KEY"]; ok {
		t.Fatal("api key should be removed when token is set")
	}
}

func TestDeployCatalogI18n(t *testing.T) {
	for _, locale := range []i18n.Locale{i18n.ZhCN, i18n.EnUS} {
		for _, provider := range Deployers() {
			keys := []string{provider.LabelKey, provider.DescKey}
			fields := append(append([]Field{}, provider.Fields...), provider.DeployFields...)
			for _, field := range fields {
				keys = append(keys, field.LabelKey, field.HelpKey, field.PlaceholderKey)
				for _, option := range field.Options {
					keys = append(keys, option.LabelKey)
				}
			}
			for _, key := range keys {
				if key == "" {
					continue
				}
				if text := i18n.T(locale, key, nil); text == key {
					t.Errorf("%s: missing translation %q", locale, key)
				}
			}
		}
	}
}

func TestLegoSupportsProviders(t *testing.T) {
	for _, def := range DNSProviders() {
		if _, err := legodns.NewDNSChallengeProviderByName(def.Value); err != nil && strings.Contains(err.Error(), "unsupported DNS provider") {
			t.Errorf("provider %s is not supported by lego: %v", def.Value, err)
		}
	}
}
