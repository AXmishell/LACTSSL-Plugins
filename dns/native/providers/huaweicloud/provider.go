// Package huaweicloud 是华为云 DNS 的自研实现。
//
// 使用 AK/SK SDK-HMAC-SHA256 签名 + DNS REST API。
package huaweicloud

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/huawei"
)

const defaultTTL = 300

type plugin struct{}

func (plugin) ID() string { return "huaweicloud" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() { dns.Register(plugin{}) }

type provider struct {
	client *httpapi.Client
	access string
	secret string
	region string
	ttl    int
	now    func() time.Time
}

func newProvider(cfg map[string]string) (*provider, error) {
	access := strings.TrimSpace(cfg["access_key_id"])
	secret := strings.TrimSpace(cfg["secret_access_key"])
	region := strings.TrimSpace(cfg["region"])
	if access == "" || secret == "" || region == "" {
		return nil, errors.New("huaweicloud: access_key_id, secret_access_key and region are required")
	}
	return &provider{
		client: httpapi.New("https://dns." + region + ".myhuaweicloud.com"),
		access: access,
		secret: secret,
		region: region,
		ttl:    defaultTTL,
		now:    time.Now,
	}, nil
}

type zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type listZonesResponse struct {
	Zones []zone `json:"zones"`
}

type recordSet struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	TTL     int      `json:"ttl"`
	Records []string `json:"records"`
	Status  string   `json:"status"`
}

type listRecordSetsResponse struct {
	Recordsets []recordSet `json:"recordsets"`
}

type recordSetBody struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	TTL     int      `json:"ttl"`
	Records []string `json:"records"`
}

func (p *provider) Present(domain, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domain)
	zoneID, err := p.findZoneID(fqdn)
	if err != nil {
		return fmt.Errorf("huaweicloud: %w", err)
	}
	existing, err := p.findRecordSet(zoneID, fqdn)
	if err != nil {
		return fmt.Errorf("huaweicloud: %w", err)
	}
	value := dnsutil.QuoteTXT(keyAuth)
	if existing == nil {
		body, _ := httpapi.EncodeJSON(recordSetBody{
			Name:    fqdn,
			Type:    "TXT",
			TTL:     p.ttl,
			Records: []string{value},
		})
		_, err = p.do(http.MethodPost, "/v2/zones/"+url.PathEscape(zoneID)+"/recordsets", nil, body)
		return err
	}
	if dnsutil.ContainsTXT(existing.Records, value) {
		return nil
	}
	records := append(append([]string(nil), existing.Records...), value)
	body, _ := httpapi.EncodeJSON(recordSetBody{
		Name:    existing.Name,
		Type:    existing.Type,
		TTL:     existing.TTL,
		Records: records,
	})
	_, err = p.do(http.MethodPut, "/v2/zones/"+url.PathEscape(zoneID)+"/recordsets/"+url.PathEscape(existing.ID), nil, body)
	return err
}

func (p *provider) CleanUp(domain, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domain)
	zoneID, err := p.findZoneID(fqdn)
	if err != nil {
		return fmt.Errorf("huaweicloud: %w", err)
	}
	existing, err := p.findRecordSet(zoneID, fqdn)
	if err != nil {
		return fmt.Errorf("huaweicloud: %w", err)
	}
	if existing == nil {
		return nil
	}
	remaining := dnsutil.RemoveTXT(existing.Records, keyAuth)
	if len(remaining) == len(existing.Records) {
		return nil
	}
	if len(remaining) == 0 {
		_, err = p.do(http.MethodDelete, "/v2/zones/"+url.PathEscape(zoneID)+"/recordsets/"+url.PathEscape(existing.ID), nil, nil)
		return err
	}
	body, _ := httpapi.EncodeJSON(recordSetBody{
		Name:    existing.Name,
		Type:    existing.Type,
		TTL:     existing.TTL,
		Records: remaining,
	})
	_, err = p.do(http.MethodPut, "/v2/zones/"+url.PathEscape(zoneID)+"/recordsets/"+url.PathEscape(existing.ID), nil, body)
	return err
}

func (p *provider) findZoneID(fqdn string) (string, error) {
	resp, err := p.do(http.MethodGet, "/v2/zones", url.Values{"limit": {"500"}}, nil)
	if err != nil {
		return "", err
	}
	var payload listZonesResponse
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return "", err
	}
	names := make([]string, 0, len(payload.Zones))
	byName := make(map[string]zone, len(payload.Zones))
	for _, item := range payload.Zones {
		names = append(names, item.Name)
		byName[strings.ToLower(dnsutil.Fqdn(item.Name))] = item
	}
	name, err := dnsutil.ZoneName(fqdn, names)
	if err != nil {
		return "", err
	}
	return byName[strings.ToLower(name)].ID, nil
}

func (p *provider) findRecordSet(zoneID, fqdn string) (*recordSet, error) {
	query := url.Values{"name": {fqdn}, "type": {"TXT"}}
	resp, err := p.do(http.MethodGet, "/v2/zones/"+url.PathEscape(zoneID)+"/recordsets", query, nil)
	if err != nil {
		return nil, err
	}
	var payload listRecordSetsResponse
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return nil, err
	}
	for _, item := range payload.Recordsets {
		if strings.EqualFold(dnsutil.Fqdn(item.Name), dnsutil.Fqdn(fqdn)) && strings.EqualFold(item.Type, "TXT") {
			return &item, nil
		}
	}
	return nil, nil
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
	signer := huawei.Signer{AccessKey: p.access, SecretKey: p.secret, Now: p.now}
	if err := signer.Sign(req, body); err != nil {
		return nil, err
	}
	return p.client.Send(req)
}
