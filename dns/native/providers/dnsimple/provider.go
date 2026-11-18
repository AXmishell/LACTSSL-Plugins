// Package dnsimple 是 DNSimple DNS 的重构实现。
package dnsimple

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

const (
	defaultBaseURL = "https://api.dnsimple.com/v2"
	defaultTTL     = 60
)

type plugin struct{}

func (plugin) ID() string { return "dnsimple" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() {
	dns.Register(plugin{})
	dns.MarkVerifiable("dnsimple")
}

type provider struct {
	client  *httpapi.Client
	token   string
	ttl     int
	account string
}

func newProvider(cfg map[string]string) (*provider, error) {
	token := strings.TrimSpace(cfg["oauth_token"])
	if token == "" {
		return nil, errors.New("dnsimple: oauth_token is required")
	}
	base := strings.TrimSpace(cfg["base_url"])
	if base == "" {
		base = defaultBaseURL
	}
	return &provider{client: httpapi.New(base), token: token, ttl: defaultTTL}, nil
}

type whoamiResponse struct {
	Data struct {
		Account *struct {
			ID int `json:"id"`
		} `json:"account"`
	} `json:"data"`
}

type zone struct {
	Name string `json:"name"`
}

type listZonesResponse struct {
	Data []zone `json:"data"`
}

type record struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
}

type listRecordsResponse struct {
	Data []record `json:"data"`
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
		return fmt.Errorf("dnsimple: %w", err)
	}
	sub, err := dnsutil.RelativeName(fqdn, zoneName)
	if err != nil {
		return fmt.Errorf("dnsimple: %w", err)
	}
	if sub == "@" {
		sub = ""
	}
	body, _ := httpapi.EncodeJSON(map[string]any{
		"name":    sub,
		"type":    "TXT",
		"content": txtValue,
		"ttl":     p.ttl,
	})
	_, err = p.do(http.MethodPost, p.accountPath()+"/zones/"+url.PathEscape(zoneName)+"/records", nil, body, map[string]string{"Content-Type": "application/json"})
	return err
}

func (p *provider) CleanUp(domainName, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zoneName, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("dnsimple: %w", err)
	}
	sub, err := dnsutil.RelativeName(fqdn, zoneName)
	if err != nil {
		return fmt.Errorf("dnsimple: %w", err)
	}
	if sub == "@" {
		sub = ""
	}
	records, err := p.listRecords(zoneName, sub)
	if err != nil {
		return fmt.Errorf("dnsimple: %w", err)
	}
	for _, item := range records {
		if item.Type != "TXT" || !dnsutil.ContainsTXT([]string{item.Content}, txtValue) {
			continue
		}
		_, err := p.do(http.MethodDelete, p.accountPath()+"/zones/"+url.PathEscape(zoneName)+"/records/"+strconv.Itoa(item.ID), nil, nil, nil)
		if err != nil && !httpapi.IsNotFound(err) {
			return fmt.Errorf("dnsimple: delete record: %w", err)
		}
	}
	return nil
}

func (p *provider) accountPath() string {
	return "/" + url.PathEscape(p.account)
}

func (p *provider) findZone(fqdn string) (string, error) {
	if err := p.ensureAccount(); err != nil {
		return "", err
	}
	resp, err := p.do(http.MethodGet, p.accountPath()+"/zones", url.Values{"per_page": {"100"}}, nil, nil)
	if err != nil {
		return "", err
	}
	var payload listZonesResponse
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return "", err
	}
	names := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		names = append(names, item.Name)
	}
	return dnsutil.ZoneName(fqdn, names)
}

func (p *provider) listRecords(zoneName, sub string) ([]record, error) {
	query := url.Values{"per_page": {"100"}, "type": {"TXT"}}
	if sub != "" {
		query.Set("name", sub)
	}
	resp, err := p.do(http.MethodGet, p.accountPath()+"/zones/"+url.PathEscape(zoneName)+"/records", query, nil, nil)
	if err != nil {
		return nil, err
	}
	var payload listRecordsResponse
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

func (p *provider) ensureAccount() error {
	if p.account != "" {
		return nil
	}
	resp, err := p.do(http.MethodGet, "/whoami", nil, nil, nil)
	if err != nil {
		return err
	}
	var payload whoamiResponse
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return err
	}
	if payload.Data.Account == nil {
		return errors.New("dnsimple: user tokens are not supported, use an account token")
	}
	p.account = strconv.Itoa(payload.Data.Account.ID)
	return nil
}

func (p *provider) do(method, path string, query url.Values, body []byte, headers map[string]string) (*httpapi.Response, error) {
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+p.token)
	for key, value := range headers {
		header.Set(key, value)
	}
	return p.client.Do(context.Background(), method, path, query, header, body)
}
