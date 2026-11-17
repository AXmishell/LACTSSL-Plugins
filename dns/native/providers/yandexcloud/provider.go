// Package yandexcloud 是 Yandex Cloud DNS 的重构实现。
//
// 只使用标准库：IAM token + REST API，覆盖 _acme-challenge TXT 的增删。
package yandexcloud

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
	defaultBaseURL = "https://dns.api.cloud.yandex.net"
	defaultTTL     = 60
)

type plugin struct{}

func (plugin) ID() string { return "yandexcloud" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() { dns.Register(plugin{}) }

type provider struct {
	client   *httpapi.Client
	token    string
	folderID string
	ttl      int
}

func newProvider(cfg map[string]string) (*provider, error) {
	token := strings.TrimSpace(cfg["yandex_cloud_iam_token"])
	folderID := strings.TrimSpace(cfg["yandex_cloud_folder_id"])
	if token == "" {
		return nil, errors.New("yandexcloud: yandex_cloud_iam_token is required")
	}
	if folderID == "" {
		return nil, errors.New("yandexcloud: yandex_cloud_folder_id is required")
	}
	return &provider{
		client:   httpapi.New(defaultBaseURL),
		token:    token,
		folderID: folderID,
		ttl:      defaultTTL,
	}, nil
}

type dnsZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Zone string `json:"zone"`
}

type listZonesResponse struct {
	Zones         []dnsZone `json:"dnsZones"`
	NextPageToken string    `json:"nextPageToken"`
}

type recordSet struct {
	Name string   `json:"name"`
	Type string   `json:"type"`
	TTL  int64    `json:"ttl"`
	Data []string `json:"data"`
}

type getRecordSetResponse struct {
	Name string   `json:"name"`
	Type string   `json:"type"`
	TTL  int64    `json:"ttl"`
	Data []string `json:"data"`
}

type updateRecordSetsRequest struct {
	DNSZoneID string      `json:"dnsZoneId"`
	Deletions []recordSet `json:"deletions,omitempty"`
	Additions []recordSet `json:"additions,omitempty"`
}

func (p *provider) Present(domain, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domain)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("yandexcloud: %w", err)
	}
	existing, err := p.getRecordSet(zone.ID, fqdn)
	if err != nil {
		return fmt.Errorf("yandexcloud: %w", err)
	}
	values := []string{}
	var deletions []recordSet
	if existing != nil {
		values = append(values, existing.Data...)
		deletions = append(deletions, *existing)
	}
	if dnsutil.ContainsTXT(values, keyAuth) {
		return nil
	}
	values = append(values, dnsutil.QuoteTXT(keyAuth))
	return p.update(zone.ID, deletions, []recordSet{{
		Name: fqdn,
		Type: "TXT",
		TTL:  int64(p.ttl),
		Data: values,
	}})
}

func (p *provider) CleanUp(domain, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domain)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("yandexcloud: %w", err)
	}
	existing, err := p.getRecordSet(zone.ID, fqdn)
	if err != nil {
		return fmt.Errorf("yandexcloud: %w", err)
	}
	if existing == nil || !dnsutil.ContainsTXT(existing.Data, keyAuth) {
		return nil
	}
	remaining := dnsutil.RemoveTXT(existing.Data, keyAuth)
	var additions []recordSet
	if len(remaining) > 0 {
		additions = []recordSet{{
			Name: fqdn,
			Type: "TXT",
			TTL:  existing.TTL,
			Data: remaining,
		}}
	}
	return p.update(zone.ID, []recordSet{*existing}, additions)
}

func (p *provider) findZone(fqdn string) (dnsZone, error) {
	query := url.Values{"folderId": {p.folderID}, "pageSize": {"1000"}}
	var lastErr error
	for page := 0; page < 20; page++ {
		resp, err := p.client.Do(context.Background(), http.MethodGet, "/dns/v1/zones", query, p.headers(), nil)
		if err != nil {
			return dnsZone{}, err
		}
		var payload listZonesResponse
		if err := httpapi.JSON(resp.Body, &payload); err != nil {
			return dnsZone{}, err
		}
		names := make([]string, 0, len(payload.Zones))
		byName := make(map[string]dnsZone, len(payload.Zones))
		for _, zone := range payload.Zones {
			name := zone.Zone
			if name == "" {
				name = zone.Name
			}
			names = append(names, name)
			byName[strings.ToLower(dnsutil.Fqdn(name))] = zone
		}
		if len(names) > 0 {
			if name, err := dnsutil.ZoneName(fqdn, names); err == nil {
				return byName[strings.ToLower(name)], nil
			}
		}
		if payload.NextPageToken == "" {
			break
		}
		query.Set("pageToken", payload.NextPageToken)
		lastErr = errors.New("no matching DNS zone")
	}
	if lastErr == nil {
		lastErr = errors.New("no matching DNS zone")
	}
	return dnsZone{}, lastErr
}

func (p *provider) getRecordSet(zoneID, fqdn string) (*recordSet, error) {
	query := url.Values{"name": {fqdn}, "type": {"TXT"}}
	resp, err := p.client.Do(context.Background(), http.MethodGet, "/dns/v1/zones/"+url.PathEscape(zoneID)+":getRecordSet", query, p.headers(), nil)
	if err != nil {
		if httpapi.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var payload getRecordSetResponse
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return nil, err
	}
	return &recordSet{Name: payload.Name, Type: payload.Type, TTL: payload.TTL, Data: payload.Data}, nil
}

func (p *provider) update(zoneID string, deletions, additions []recordSet) error {
	body, err := httpapi.EncodeJSON(updateRecordSetsRequest{
		DNSZoneID: zoneID,
		Deletions: deletions,
		Additions: additions,
	})
	if err != nil {
		return err
	}
	headers := p.headers()
	headers.Set("Content-Type", "application/json")
	_, err = p.client.Do(context.Background(), http.MethodPost, "/dns/v1/zones/"+url.PathEscape(zoneID)+":updateRecordSets", nil, headers, body)
	return err
}

func (p *provider) headers() http.Header {
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+p.token)
	return header
}
