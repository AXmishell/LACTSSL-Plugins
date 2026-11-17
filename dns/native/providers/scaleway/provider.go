// Package scaleway 是 Scaleway Domains API 的重构实现。
package scaleway

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
	defaultBaseURL = "https://api.scaleway.com"
	defaultTTL     = 60
)

type plugin struct{}

func (plugin) ID() string { return "scaleway" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() { dns.Register(plugin{}) }

type provider struct {
	client    *httpapi.Client
	token     string
	projectID string
	ttl       int
}

func newProvider(cfg map[string]string) (*provider, error) {
	token := firstNonEmpty(cfg["scw_secret_key"], cfg["scaleway_api_token"])
	if token == "" {
		return nil, errors.New("scaleway: scw_secret_key or scaleway_api_token is required")
	}
	return &provider{
		client:    httpapi.New(defaultBaseURL),
		token:     token,
		projectID: strings.TrimSpace(cfg["scaleway_project_id"]),
		ttl:       defaultTTL,
	}, nil
}

type zone struct {
	Domain    string `json:"domain"`
	Subdomain string `json:"subdomain"`
}

type listZonesResponse struct {
	DNSZones   []zone `json:"dns_zones"`
	TotalCount int    `json:"total_count"`
}

type record struct {
	Data string `json:"data"`
	Name string `json:"name"`
	TTL  int    `json:"ttl"`
	Type string `json:"type"`
}

type recordChange struct {
	Add    *recordChangeAdd    `json:"add,omitempty"`
	Delete *recordChangeDelete `json:"delete,omitempty"`
}

type recordChangeAdd struct {
	Records []record `json:"records"`
}

type recordChangeDelete struct {
	IDFields recordIdentifier `json:"id_fields"`
}

type recordIdentifier struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Data string `json:"data"`
}

type updateRequest struct {
	Changes                 []recordChange `json:"changes"`
	ReturnAllRecords        bool           `json:"return_all_records"`
	DisallowNewZoneCreation bool           `json:"disallow_new_zone_creation"`
}

func (p *provider) Present(domainName, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zoneName, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("scaleway: %w", err)
	}
	body, _ := httpapi.EncodeJSON(updateRequest{
		Changes: []recordChange{{Add: &recordChangeAdd{Records: []record{{
			Data: dnsutil.QuoteTXT(keyAuth),
			Name: fqdn,
			TTL:  p.ttl,
			Type: "TXT",
		}}}}},
		DisallowNewZoneCreation: true,
	})
	_, err = p.do(http.MethodPatch, "/domain/v2beta1/dns-zones/"+url.PathEscape(zoneName)+"/records", nil, body)
	return err
}

func (p *provider) CleanUp(domainName, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zoneName, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("scaleway: %w", err)
	}
	body, _ := httpapi.EncodeJSON(updateRequest{
		Changes: []recordChange{{Delete: &recordChangeDelete{IDFields: recordIdentifier{
			Name: fqdn,
			Type: "TXT",
			Data: dnsutil.QuoteTXT(keyAuth),
		}}}},
		DisallowNewZoneCreation: true,
	})
	_, err = p.do(http.MethodPatch, "/domain/v2beta1/dns-zones/"+url.PathEscape(zoneName)+"/records", nil, body)
	if httpapi.IsNotFound(err) {
		return nil
	}
	return err
}

func (p *provider) findZone(fqdn string) (string, error) {
	var zones []zone
	for page := 1; page <= 20; page++ {
		query := url.Values{"page": {strconv.Itoa(page)}, "page_size": {"100"}}
		if p.projectID != "" {
			query.Set("project_id", p.projectID)
		}
		resp, err := p.do(http.MethodGet, "/domain/v2beta1/dns-zones", query, nil)
		if err != nil {
			return "", err
		}
		var payload listZonesResponse
		if err := httpapi.JSON(resp.Body, &payload); err != nil {
			return "", err
		}
		zones = append(zones, payload.DNSZones...)
		if len(payload.DNSZones) == 0 || len(zones) >= payload.TotalCount {
			break
		}
	}
	names := make([]string, 0, len(zones))
	for _, item := range zones {
		name := item.Domain
		if item.Subdomain != "" {
			name = item.Subdomain + "." + item.Domain
		}
		names = append(names, name)
	}
	return dnsutil.ZoneName(fqdn, names)
}

func (p *provider) do(method, path string, query url.Values, body []byte) (*httpapi.Response, error) {
	header := make(http.Header)
	header.Set("X-Auth-Token", p.token)
	if body != nil {
		header.Set("Content-Type", "application/json")
	}
	return p.client.Do(context.Background(), method, path, query, header, body)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
