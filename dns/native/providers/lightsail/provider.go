// Package lightsail 是 AWS Lightsail DNS 的重构实现。
//
// 使用 SigV4 + Lightsail JSON 1.1 API，覆盖 TXT 记录增删。
package lightsail

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/sigv4"
)

const defaultRegion = "us-east-1"

type plugin struct{}

func (plugin) ID() string { return "lightsail" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() {
	dns.Register(plugin{})
	dns.MarkVerifiable("lightsail")
}

type provider struct {
	client      *httpapi.Client
	credentials sigv4.Credentials
	region      string
	zone        string
	now         func() time.Time
}

func newProvider(cfg map[string]string) (*provider, error) {
	access := firstNonEmpty(cfg["access_key_id"], os.Getenv("AWS_ACCESS_KEY_ID"))
	secret := firstNonEmpty(cfg["secret_access_key"], os.Getenv("AWS_SECRET_ACCESS_KEY"))
	session := firstNonEmpty(cfg["session_token"], os.Getenv("AWS_SESSION_TOKEN"))
	zone := strings.TrimSpace(cfg["dns_zone"])
	region := firstNonEmpty(cfg["lightsail_region"], os.Getenv("LIGHTSAIL_REGION"), defaultRegion)
	if access == "" || secret == "" {
		return nil, errors.New("lightsail: access_key_id and secret_access_key are required")
	}
	if zone == "" {
		return nil, errors.New("lightsail: dns_zone is required")
	}
	return &provider{
		client:      httpapi.New("https://lightsail." + region + ".amazonaws.com"),
		credentials: sigv4.Credentials{AccessKeyID: access, SecretAccessKey: secret, SessionToken: session},
		region:      region,
		zone:        zone,
		now:         time.Now,
	}, nil
}

type domainEntry struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

type createDomainEntryRequest struct {
	DomainName  string      `json:"domainName"`
	DomainEntry domainEntry `json:"domainEntry"`
}

type deleteDomainEntryRequest struct {
	DomainName  string      `json:"domainName"`
	DomainEntry domainEntry `json:"domainEntry"`
}

// Verify 只读检查凭据与 zone 归属，不写入任何 DNS 记录。
func (p *provider) Verify(_ context.Context, domain string) (string, error) {
	body, err := httpapi.EncodeJSON(map[string]string{"domainName": p.zone})
	if err != nil {
		return "", err
	}
	if _, err := p.call("GetDomain", body); err != nil {
		return "", fmt.Errorf("lightsail: get domain: %w", err)
	}
	return p.zone, nil
}

func (p *provider) Present(domain, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	body, err := httpapi.EncodeJSON(createDomainEntryRequest{
		DomainName: p.zone,
		DomainEntry: domainEntry{
			Name:   dnsutil.ChallengeFQDN(domain),
			Target: dnsutil.QuoteTXT(txtValue),
			Type:   "TXT",
		},
	})
	if err != nil {
		return err
	}
	_, err = p.call("CreateDomainEntry", body)
	if err != nil {
		return fmt.Errorf("lightsail: create domain entry: %w", err)
	}
	return nil
}

func (p *provider) CleanUp(domain, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	body, err := httpapi.EncodeJSON(deleteDomainEntryRequest{
		DomainName: p.zone,
		DomainEntry: domainEntry{
			Name:   dnsutil.ChallengeFQDN(domain),
			Target: dnsutil.QuoteTXT(txtValue),
			Type:   "TXT",
		},
	})
	if err != nil {
		return err
	}
	_, err = p.call("DeleteDomainEntry", body)
	if err != nil && !httpapi.IsNotFound(err) {
		return fmt.Errorf("lightsail: delete domain entry: %w", err)
	}
	return nil
}

func (p *provider) call(action string, body []byte) (*httpapi.Response, error) {
	header := make(http.Header)
	header.Set("Content-Type", "application/x-amz-json-1.1")
	header.Set("X-Amz-Target", "Lightsail_20161128."+action)
	req, err := p.client.NewRequest(context.Background(), http.MethodPost, "/", nil, header, body)
	if err != nil {
		return nil, err
	}
	signer := sigv4.Signer{Region: p.region, Service: "lightsail", Credentials: p.credentials, Now: p.now}
	if err := signer.Sign(req, body); err != nil {
		return nil, err
	}
	return p.client.Send(req)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
