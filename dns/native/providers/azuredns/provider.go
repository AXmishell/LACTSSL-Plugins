// Package azuredns 是 Azure DNS / Private DNS 的重构实现。
//
// 使用 Azure AD client credentials + ARM REST API。
package azuredns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/azure"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

const (
	publicAPIVersion  = "2018-05-01"
	privateAPIVersion = "2020-06-01"
	defaultTTL        = 60
)

type plugin struct{}

func (plugin) ID() string { return "azuredns" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() { dns.Register(plugin{}) }

type tokenProvider interface {
	Token(context.Context) (string, error)
}

type provider struct {
	client        *httpapi.Client
	tokens        tokenProvider
	subscription  string
	resourceGroup string
	zoneName      string
	private       bool
	apiVersion    string
	ttl           int
}

func newProvider(cfg map[string]string) (*provider, error) {
	env, err := azure.EnvironmentByName(cfg["azure_environment"])
	if err != nil {
		return nil, err
	}
	subscription := strings.TrimSpace(cfg["subscription_id"])
	resourceGroup := strings.TrimSpace(cfg["resource_group"])
	if subscription == "" || resourceGroup == "" {
		return nil, errors.New("azuredns: subscription_id and resource_group are required")
	}
	private := parseBool(cfg["private_zone"])
	version := publicAPIVersion
	if private {
		version = privateAPIVersion
	}
	return &provider{
		client:        httpapi.New(env.ManagementBaseURL),
		tokens:        &azure.TokenSource{TenantID: cfg["tenant_id"], ClientID: cfg["client_id"], ClientSecret: cfg["client_secret"], Environment: env},
		subscription:  subscription,
		resourceGroup: resourceGroup,
		zoneName:      strings.TrimSpace(cfg["zone_name"]),
		private:       private,
		apiVersion:    version,
		ttl:           defaultTTL,
	}, nil
}

type zone struct {
	Name string `json:"name"`
}

type listZonesResponse struct {
	Value []zone `json:"value"`
}

type txtRecord struct {
	Value []string `json:"value"`
}

type recordSetProperties struct {
	TTL        int         `json:"TTL,omitempty"`
	TXTRecords []txtRecord `json:"TXTRecords,omitempty"`
}

type recordSet struct {
	Properties recordSetProperties `json:"properties"`
}

func (p *provider) Present(domain, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domain)
	zoneName, relative, err := p.resolve(fqdn)
	if err != nil {
		return fmt.Errorf("azuredns: %w", err)
	}
	existing, err := p.getRecord(zoneName, relative)
	if err != nil {
		return fmt.Errorf("azuredns: %w", err)
	}
	values := []string{}
	if existing != nil {
		for _, item := range existing.Properties.TXTRecords {
			values = append(values, item.Value...)
		}
	}
	if dnsutil.ContainsTXT(values, keyAuth) {
		return nil
	}
	values = append(values, keyAuth)
	return p.putRecord(zoneName, relative, values)
}

func (p *provider) CleanUp(domain, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domain)
	zoneName, relative, err := p.resolve(fqdn)
	if err != nil {
		return fmt.Errorf("azuredns: %w", err)
	}
	existing, err := p.getRecord(zoneName, relative)
	if err != nil {
		return fmt.Errorf("azuredns: %w", err)
	}
	if existing == nil {
		return nil
	}
	var values []string
	for _, item := range existing.Properties.TXTRecords {
		values = append(values, item.Value...)
	}
	remaining := dnsutil.RemoveTXT(values, keyAuth)
	if len(remaining) == len(values) {
		return nil
	}
	if len(remaining) == 0 {
		return p.deleteRecord(zoneName, relative)
	}
	return p.putRecord(zoneName, relative, remaining)
}

func (p *provider) resolve(fqdn string) (zoneName, relative string, err error) {
	if p.zoneName != "" {
		zoneName = strings.TrimSuffix(p.zoneName, ".")
		relative, err = dnsutil.RelativeName(fqdn, zoneName)
		return zoneName, relative, err
	}
	zones, err := p.listZones()
	if err != nil {
		return "", "", err
	}
	names := make([]string, 0, len(zones))
	for _, item := range zones {
		names = append(names, item.Name)
	}
	zoneName, err = dnsutil.ZoneName(fqdn, names)
	if err != nil {
		return "", "", err
	}
	zoneName = strings.TrimSuffix(zoneName, ".")
	relative, err = dnsutil.RelativeName(fqdn, zoneName)
	return zoneName, relative, err
}

func (p *provider) listZones() ([]zone, error) {
	path := "/subscriptions/" + url.PathEscape(p.subscription) + "/resourceGroups/" + url.PathEscape(p.resourceGroup) + "/providers/Microsoft.Network/"
	if p.private {
		path += "privateDnsZones"
	} else {
		path += "dnsZones"
	}
	resp, err := p.do(http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	var payload listZonesResponse
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return nil, err
	}
	return payload.Value, nil
}

func (p *provider) getRecord(zoneName, relative string) (*recordSet, error) {
	resp, err := p.do(http.MethodGet, p.recordPath(zoneName, relative), nil, nil)
	if err != nil {
		if httpapi.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var payload recordSet
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func (p *provider) putRecord(zoneName, relative string, values []string) error {
	body, err := httpapi.EncodeJSON(recordSet{Properties: recordSetProperties{
		TTL:        p.ttl,
		TXTRecords: []txtRecord{{Value: values}},
	}})
	if err != nil {
		return err
	}
	_, err = p.do(http.MethodPut, p.recordPath(zoneName, relative), body, map[string]string{"Content-Type": "application/json"})
	return err
}

func (p *provider) deleteRecord(zoneName, relative string) error {
	_, err := p.do(http.MethodDelete, p.recordPath(zoneName, relative), nil, nil)
	if httpapi.IsNotFound(err) {
		return nil
	}
	return err
}

func (p *provider) recordPath(zoneName, relative string) string {
	base := "/subscriptions/" + url.PathEscape(p.subscription) + "/resourceGroups/" + url.PathEscape(p.resourceGroup) + "/providers/Microsoft.Network/"
	if p.private {
		base += "privateDnsZones/"
	} else {
		base += "dnsZones/"
	}
	return base + url.PathEscape(zoneName) + "/TXT/" + url.PathEscape(relative)
}

func (p *provider) do(method, path string, body []byte, headers map[string]string) (*httpapi.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, err := p.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+token)
	for key, value := range headers {
		header.Set(key, value)
	}
	query := url.Values{"api-version": {p.apiVersion}}
	return p.client.Do(ctx, method, path, query, header, body)
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
