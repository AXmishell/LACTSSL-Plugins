// Package vultr 是 Vultr DNS 的自研实现。
package vultr

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
	defaultBaseURL = "https://api.vultr.com/v2"
	defaultTTL     = 60
)

type plugin struct{}

func (plugin) ID() string { return "vultr" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() { dns.Register(plugin{}) }

type provider struct {
	client *httpapi.Client
	apiKey string
	ttl    int
}

func newProvider(cfg map[string]string) (*provider, error) {
	apiKey := strings.TrimSpace(cfg["api_key"])
	if apiKey == "" {
		return nil, errors.New("vultr: api_key is required")
	}
	return &provider{client: httpapi.New(defaultBaseURL), apiKey: apiKey, ttl: defaultTTL}, nil
}

type domain struct {
	Domain string `json:"domain"`
}

type listDomainsResponse struct {
	Domains []domain `json:"domains"`
	Meta    struct {
		Links struct {
			Next string `json:"next"`
		} `json:"links"`
	} `json:"meta"`
}

type record struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Data string `json:"data"`
	TTL  int    `json:"ttl"`
}

type listRecordsResponse struct {
	Records []record `json:"records"`
	Meta    struct {
		Links struct {
			Next string `json:"next"`
		} `json:"links"`
	} `json:"meta"`
}

func (p *provider) Present(domainName, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("vultr: %w", err)
	}
	sub, err := dnsutil.RelativeName(fqdn, zone)
	if err != nil {
		return fmt.Errorf("vultr: %w", err)
	}
	body, _ := httpapi.EncodeJSON(map[string]any{
		"name":     sub,
		"type":     "TXT",
		"data":     dnsutil.QuoteTXT(keyAuth),
		"ttl":      p.ttl,
		"priority": 0,
	})
	_, err = p.do(http.MethodPost, "/domains/"+url.PathEscape(zone)+"/records", nil, body)
	return err
}

func (p *provider) CleanUp(domainName, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("vultr: %w", err)
	}
	sub, err := dnsutil.RelativeName(fqdn, zone)
	if err != nil {
		return fmt.Errorf("vultr: %w", err)
	}
	records, err := p.listRecords(zone)
	if err != nil {
		return fmt.Errorf("vultr: %w", err)
	}
	for _, item := range records {
		if item.Type == "TXT" && item.Name == sub && dnsutil.ContainsTXT([]string{item.Data}, keyAuth) {
			if _, err := p.do(http.MethodDelete, "/domains/"+url.PathEscape(zone)+"/records/"+url.PathEscape(item.ID), nil, nil); err != nil {
				return fmt.Errorf("vultr: delete record: %w", err)
			}
		}
	}
	return nil
}

func (p *provider) findZone(fqdn string) (string, error) {
	var domains []domain
	cursor := ""
	for page := 0; page < 20; page++ {
		query := url.Values{"per_page": {"500"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		resp, err := p.do(http.MethodGet, "/domains", query, nil)
		if err != nil {
			return "", err
		}
		var payload listDomainsResponse
		if err := httpapi.JSON(resp.Body, &payload); err != nil {
			return "", err
		}
		domains = append(domains, payload.Domains...)
		if payload.Meta.Links.Next == "" {
			break
		}
		cursor = payload.Meta.Links.Next
	}
	names := make([]string, 0, len(domains))
	for _, item := range domains {
		names = append(names, item.Domain)
	}
	return dnsutil.ZoneName(fqdn, names)
}

func (p *provider) listRecords(zone string) ([]record, error) {
	var records []record
	cursor := ""
	for page := 0; page < 20; page++ {
		query := url.Values{"per_page": {"500"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		resp, err := p.do(http.MethodGet, "/domains/"+url.PathEscape(zone)+"/records", query, nil)
		if err != nil {
			return nil, err
		}
		var payload listRecordsResponse
		if err := httpapi.JSON(resp.Body, &payload); err != nil {
			return nil, err
		}
		records = append(records, payload.Records...)
		if payload.Meta.Links.Next == "" {
			break
		}
		cursor = payload.Meta.Links.Next
	}
	return records, nil
}

func (p *provider) do(method, path string, query url.Values, body []byte) (*httpapi.Response, error) {
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+p.apiKey)
	if body != nil {
		header.Set("Content-Type", "application/json")
	}
	return p.client.Do(context.Background(), method, path, query, header, body)
}
