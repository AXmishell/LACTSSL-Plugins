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
	if got := len(Deployers()); got != 43 {
		t.Fatalf("deployers = %d, want 43", got)
	}
	if got := len(DeprecatedDeployTypes()); got != 6 {
		t.Fatalf("deprecated deploy types = %d, want 6", got)
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
