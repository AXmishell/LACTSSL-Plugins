// Package exoscale 是 Exoscale DNS 的自研实现。
package exoscale

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	exosig "github.com/AXmishell/LACTSSL-Plugins/sdk/exoscale"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

const (
	defaultEndpoint = "https://api-ch-gva-2.exoscale.com/v2"
	defaultTTL      = 60
)

type plugin struct{}

func (plugin) ID() string { return "exoscale" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() { dns.Register(plugin{}) }

type provider struct {
	client *httpapi.Client
	signer exosig.Signer
	ttl    int
}

func newProvider(cfg map[string]string) (*provider, error) {
	key := strings.TrimSpace(cfg["exoscale_api_key"])
	secret := strings.TrimSpace(cfg["exoscale_api_secret"])
	if key == "" || secret == "" {
		return nil, errors.New("exoscale: exoscale_api_key and exoscale_api_secret are required")
	}
	endpoint := strings.TrimSpace(cfg["exoscale_endpoint"])
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	return &provider{
		client: httpapi.New(endpoint),
		signer: exosig.Signer{APIKey: key, APISecret: secret},
		ttl:    defaultTTL,
	}, nil
}

type domain struct {
	ID          string `json:"id"`
	UnicodeName string `json:"unicode-name"`
}

type listDomainsResponse struct {
	Domains []domain `json:"dns-domains"`
}

type record struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
}

type listRecordsResponse struct {
	Records []record `json:"dns-domain-records"`
}

func (p *provider) Present(domainName, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("exoscale: %w", err)
	}
	name, err := dnsutil.RelativeName(fqdn, zone.UnicodeName)
	if err != nil {
		return fmt.Errorf("exoscale: %w", err)
	}
	body, _ := httpapi.EncodeJSON(map[string]any{
		"name": name, "type": "TXT", "content": keyAuth, "ttl": p.ttl,
	})
	_, err = p.do(http.MethodPost, "/dns-domain/"+url.PathEscape(zone.ID)+"/record", nil, body)
	return err
}

func (p *provider) CleanUp(domainName, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("exoscale: %w", err)
	}
	name, err := dnsutil.RelativeName(fqdn, zone.UnicodeName)
	if err != nil {
		return fmt.Errorf("exoscale: %w", err)
	}
	records, err := p.listRecords(zone.ID)
	if err != nil {
		return fmt.Errorf("exoscale: %w", err)
	}
	for _, item := range records {
		if item.Name != name || !strings.EqualFold(item.Type, "TXT") {
			continue
		}
		if !dnsutil.ContainsTXT([]string{item.Content}, keyAuth) {
			continue
		}
		if _, err := p.do(http.MethodDelete, "/dns-domain/"+url.PathEscape(zone.ID)+"/record/"+url.PathEscape(item.ID), nil, nil); err != nil {
			return fmt.Errorf("exoscale: delete record: %w", err)
		}
	}
	return nil
}

func (p *provider) findZone(fqdn string) (domain, error) {
	resp, err := p.do(http.MethodGet, "/dns-domain", nil, nil)
	if err != nil {
		return domain{}, err
	}
	var payload listDomainsResponse
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return domain{}, err
	}
	names := make([]string, 0, len(payload.Domains))
	byName := make(map[string]domain, len(payload.Domains))
	for _, item := range payload.Domains {
		names = append(names, item.UnicodeName)
		byName[strings.ToLower(dnsutil.Fqdn(item.UnicodeName))] = item
	}
	name, err := dnsutil.ZoneName(fqdn, names)
	if err != nil {
		return domain{}, err
	}
	return byName[strings.ToLower(name)], nil
}

func (p *provider) listRecords(zoneID string) ([]record, error) {
	resp, err := p.do(http.MethodGet, "/dns-domain/"+url.PathEscape(zoneID)+"/record", nil, nil)
	if err != nil {
		return nil, err
	}
	var payload listRecordsResponse
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return nil, err
	}
	return payload.Records, nil
}

func (p *provider) do(method, path string, query url.Values, body []byte) (*httpapi.Response, error) {
	header := make(http.Header)
	if body != nil {
		header.Set("Content-Type", "application/json")
	}
	req, err := p.client.NewRequest(context.Background(), method, path, query, header, body)
	if err != nil {
		return nil, err
	}
	if err := p.signer.Sign(req, body); err != nil {
		return nil, err
	}
	return p.client.Send(req)
}
