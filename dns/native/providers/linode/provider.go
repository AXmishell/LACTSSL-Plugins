// Package linode 是 Linode DNS 的自研实现。
package linode

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
	defaultBaseURL = "https://api.linode.com/v4"
	defaultTTL     = 300
)

type plugin struct{}

func (plugin) ID() string { return "linode" }

func (plugin) Aliases() []string { return []string{"linodev4"} }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() { dns.Register(plugin{}) }

type provider struct {
	client *httpapi.Client
	token  string
	ttl    int
}

func newProvider(cfg map[string]string) (*provider, error) {
	token := strings.TrimSpace(cfg["token"])
	if token == "" {
		return nil, errors.New("linode: token is required")
	}
	return &provider{client: httpapi.New(defaultBaseURL), token: token, ttl: defaultTTL}, nil
}

type domain struct {
	ID     int    `json:"id"`
	Domain string `json:"domain"`
}

type listDomainsResponse struct {
	Data  []domain `json:"data"`
	Page  int      `json:"page"`
	Pages int      `json:"pages"`
}

type record struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Target string `json:"target"`
	TTLSec int    `json:"ttl_sec"`
}

type listRecordsResponse struct {
	Data  []record `json:"data"`
	Page  int      `json:"page"`
	Pages int      `json:"pages"`
}

func (p *provider) Present(domainName, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("linode: %w", err)
	}
	body, _ := httpapi.EncodeJSON(map[string]any{
		"type":    "TXT",
		"name":    dnsutil.UnFqdn(fqdn),
		"target":  keyAuth,
		"ttl_sec": p.ttl,
	})
	_, err = p.do(http.MethodPost, "/domains/"+strconv.Itoa(zone.ID)+"/records", nil, body)
	return err
}

func (p *provider) CleanUp(domainName, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("linode: %w", err)
	}
	records, err := p.listRecords(zone.ID)
	if err != nil {
		return fmt.Errorf("linode: %w", err)
	}
	for _, item := range records {
		if item.Type != "TXT" {
			continue
		}
		if item.Name != dnsutil.UnFqdn(fqdn) {
			continue
		}
		if !dnsutil.ContainsTXT([]string{item.Target}, keyAuth) {
			continue
		}
		if _, err := p.do(http.MethodDelete, "/domains/"+strconv.Itoa(zone.ID)+"/records/"+strconv.Itoa(item.ID), nil, nil); err != nil {
			return fmt.Errorf("linode: delete record: %w", err)
		}
	}
	return nil
}

func (p *provider) findZone(fqdn string) (domain, error) {
	var domains []domain
	page := 1
	for ; page <= 20; page++ {
		query := url.Values{"page_size": {"500"}, "page": {strconv.Itoa(page)}}
		resp, err := p.do(http.MethodGet, "/domains", query, nil)
		if err != nil {
			return domain{}, err
		}
		var payload listDomainsResponse
		if err := httpapi.JSON(resp.Body, &payload); err != nil {
			return domain{}, err
		}
		domains = append(domains, payload.Data...)
		if payload.Pages == 0 || page >= payload.Pages {
			break
		}
	}
	names := make([]string, 0, len(domains))
	byName := make(map[string]domain, len(domains))
	for _, item := range domains {
		names = append(names, item.Domain)
		byName[strings.ToLower(dnsutil.Fqdn(item.Domain))] = item
	}
	name, err := dnsutil.ZoneName(fqdn, names)
	if err != nil {
		return domain{}, err
	}
	return byName[strings.ToLower(name)], nil
}

func (p *provider) listRecords(domainID int) ([]record, error) {
	var records []record
	page := 1
	for ; page <= 20; page++ {
		query := url.Values{"page_size": {"500"}, "page": {strconv.Itoa(page)}}
		resp, err := p.do(http.MethodGet, "/domains/"+strconv.Itoa(domainID)+"/records", query, nil)
		if err != nil {
			return nil, err
		}
		var payload listRecordsResponse
		if err := httpapi.JSON(resp.Body, &payload); err != nil {
			return nil, err
		}
		records = append(records, payload.Data...)
		if payload.Pages == 0 || page >= payload.Pages {
			break
		}
	}
	return records, nil
}

func (p *provider) do(method, path string, query url.Values, body []byte) (*httpapi.Response, error) {
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+p.token)
	if body != nil {
		header.Set("Content-Type", "application/json")
	}
	return p.client.Do(context.Background(), method, path, query, header, body)
}
