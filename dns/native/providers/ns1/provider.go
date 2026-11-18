// Package ns1 是 NS1 DNS 的重构实现。
package ns1

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

const (
	defaultBaseURL = "https://api.nsone.net/v1"
	defaultTTL     = 60
)

type plugin struct{}

func (plugin) ID() string { return "ns1" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() {
	dns.Register(plugin{})
	dns.MarkVerifiable("ns1")
}

type provider struct {
	client *httpapi.Client
	apiKey string
	ttl    int
}

func newProvider(cfg map[string]string) (*provider, error) {
	apiKey := strings.TrimSpace(cfg["ns1_api_key"])
	if apiKey == "" {
		return nil, errors.New("ns1: ns1_api_key is required")
	}
	return &provider{client: httpapi.New(defaultBaseURL), apiKey: apiKey, ttl: defaultTTL}, nil
}

type zone struct {
	Zone string `json:"zone"`
}

type answer struct {
	Rdata []string `json:"rdata"`
}

type record struct {
	Zone       string   `json:"zone"`
	Domain     string   `json:"domain"`
	Type       string   `json:"type"`
	TTL        int      `json:"ttl"`
	Answers    []answer `json:"answers"`
	Filters    []any    `json:"filters"`
	Tags       []string `json:"tags"`
	BlockedTag []string `json:"blockedTags"`
}

// Verify 只读检查凭据与 zone 归属，不写入任何 DNS 记录。
func (p *provider) Verify(_ context.Context, domain string) (string, error) {
	fqdn := dnsutil.ChallengeFQDN(domain)
	z, err := p.findZone(fqdn)
	if err != nil {
		return "", err
	}
	return z, nil
}

func (p *provider) Present(domainName, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zoneName, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("ns1: %w", err)
	}
	name := dnsutil.UnFqdn(fqdn)
	existing, err := p.getRecord(zoneName, name)
	if err != nil {
		return fmt.Errorf("ns1: %w", err)
	}
	if existing == nil {
		return p.putRecord(zoneName, name, record{
			Zone: zoneName, Domain: name, Type: "TXT", TTL: p.ttl,
			Answers: []answer{{Rdata: []string{txtValue}}},
			Filters: []any{}, Tags: []string{}, BlockedTag: []string{},
		})
	}
	values := answerValues(existing.Answers)
	if dnsutil.ContainsTXT(values, txtValue) {
		return nil
	}
	existing.Answers = append(existing.Answers, answer{Rdata: []string{txtValue}})
	return p.putRecord(zoneName, name, *existing)
}

func (p *provider) CleanUp(domainName, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zoneName, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("ns1: %w", err)
	}
	name := dnsutil.UnFqdn(fqdn)
	existing, err := p.getRecord(zoneName, name)
	if err != nil {
		return fmt.Errorf("ns1: %w", err)
	}
	if existing == nil {
		return nil
	}
	var answers []answer
	removed := false
	for _, item := range existing.Answers {
		if len(item.Rdata) == 1 && dnsutil.ContainsTXT(item.Rdata, txtValue) {
			removed = true
			continue
		}
		answers = append(answers, item)
	}
	if !removed {
		return nil
	}
	if len(answers) == 0 {
		_, err := p.do(http.MethodDelete, "/zones/"+url.PathEscape(zoneName)+"/"+url.PathEscape(name)+"/TXT", nil, nil)
		if httpapi.IsNotFound(err) {
			return nil
		}
		return err
	}
	existing.Answers = answers
	return p.putRecord(zoneName, name, *existing)
}

func (p *provider) findZone(fqdn string) (string, error) {
	resp, err := p.do(http.MethodGet, "/zones", nil, nil)
	if err != nil {
		return "", err
	}
	var zones []zone
	if err := httpapi.JSON(resp.Body, &zones); err != nil {
		return "", err
	}
	names := make([]string, 0, len(zones))
	for _, item := range zones {
		names = append(names, item.Zone)
	}
	return dnsutil.ZoneName(fqdn, names)
}

func (p *provider) getRecord(zoneName, name string) (*record, error) {
	resp, err := p.do(http.MethodGet, "/zones/"+url.PathEscape(zoneName)+"/"+url.PathEscape(name)+"/TXT", nil, nil)
	if err != nil {
		if httpapi.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var payload record
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func (p *provider) putRecord(zoneName, name string, value record) error {
	body, err := httpapi.EncodeJSON(value)
	if err != nil {
		return err
	}
	_, err = p.do(http.MethodPut, "/zones/"+url.PathEscape(zoneName)+"/"+url.PathEscape(name)+"/TXT", body, map[string]string{"Content-Type": "application/json"})
	return err
}

func (p *provider) do(method, path string, body []byte, headers map[string]string) (*httpapi.Response, error) {
	header := make(http.Header)
	header.Set("X-NSONE-Key", p.apiKey)
	for key, value := range headers {
		header.Set(key, value)
	}
	return p.client.Do(context.Background(), method, path, nil, header, body)
}

func answerValues(answers []answer) []string {
	var values []string
	for _, item := range answers {
		values = append(values, item.Rdata...)
	}
	return values
}
