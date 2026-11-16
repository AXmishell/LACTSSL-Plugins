// Package sakuracloud 是 Sakura Cloud DNS 的自研实现。
//
// 使用 Basic Auth + CommonServiceItem API，覆盖 DNS 区域查找与 TXT 记录更新。
package sakuracloud

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

const (
	defaultBaseURL = "https://secure.sakura.ad.jp/cloud/zone/is1a/api/cloud/1.1"
	defaultTTL     = 60
)

type plugin struct{}

func (plugin) ID() string { return "sakuracloud" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() { dns.Register(plugin{}) }

type provider struct {
	client *httpapi.Client
	token  string
	secret string
	ttl    int
	mu     sync.Mutex
}

func newProvider(cfg map[string]string) (*provider, error) {
	token := strings.TrimSpace(cfg["sakuracloud_access_token"])
	secret := strings.TrimSpace(cfg["sakuracloud_access_token_secret"])
	if token == "" || secret == "" {
		return nil, errors.New("sakuracloud: access token and secret are required")
	}
	return &provider{
		client: httpapi.New(defaultBaseURL),
		token:  token,
		secret: secret,
		ttl:    defaultTTL,
	}, nil
}

type record struct {
	Name  string `json:"Name"`
	Type  string `json:"Type"`
	RData string `json:"RData"`
	TTL   int    `json:"TTL"`
}

type settings struct {
	DNS dnsSettings `json:"DNS"`
}

type dnsSettings struct {
	ResourceRecordSets []record `json:"ResourceRecordSets"`
}

type serviceItem struct {
	ID           string   `json:"ID"`
	Name         string   `json:"Name"`
	Settings     settings `json:"Settings"`
	SettingsHash string   `json:"SettingsHash"`
	Status       struct {
		Zone string `json:"Zone"`
	} `json:"Status"`
}

type findResponse struct {
	CommonServiceItems []serviceItem `json:"CommonServiceItems"`
	Total              int           `json:"Total"`
	From               int           `json:"From"`
	Count              int           `json:"Count"`
}

func (p *provider) Present(domain, _ string, keyAuth string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	zone, err := p.findZone(dnsutil.ChallengeFQDN(domain))
	if err != nil {
		return fmt.Errorf("sakuracloud: %w", err)
	}
	sub, err := dnsutil.RelativeName(dnsutil.ChallengeFQDN(domain), zone.name)
	if err != nil {
		return fmt.Errorf("sakuracloud: %w", err)
	}
	records := append([]record(nil), zone.item.Settings.DNS.ResourceRecordSets...)
	values := make([]string, 0, len(records))
	for _, item := range records {
		if item.Name == sub && strings.EqualFold(item.Type, "TXT") {
			values = append(values, item.RData)
		}
	}
	if dnsutil.ContainsTXT(values, keyAuth) {
		return nil
	}
	records = append(records, record{Name: sub, Type: "TXT", RData: keyAuth, TTL: p.ttl})
	return p.update(zone.item, records)
}

func (p *provider) CleanUp(domain, _ string, keyAuth string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	zone, err := p.findZone(dnsutil.ChallengeFQDN(domain))
	if err != nil {
		return fmt.Errorf("sakuracloud: %w", err)
	}
	sub, err := dnsutil.RelativeName(dnsutil.ChallengeFQDN(domain), zone.name)
	if err != nil {
		return fmt.Errorf("sakuracloud: %w", err)
	}
	records := make([]record, 0, len(zone.item.Settings.DNS.ResourceRecordSets))
	removed := false
	for _, item := range zone.item.Settings.DNS.ResourceRecordSets {
		if item.Name == sub && strings.EqualFold(item.Type, "TXT") && dnsutil.ContainsTXT([]string{item.RData}, keyAuth) {
			removed = true
			continue
		}
		records = append(records, item)
	}
	if !removed {
		return nil
	}
	return p.update(zone.item, records)
}

type foundZone struct {
	name string
	item serviceItem
}

func (p *provider) findZone(fqdn string) (foundZone, error) {
	var zones []foundZone
	from := 0
	for page := 0; page < 20; page++ {
		query := url.Values{"From": {strconv.Itoa(from)}, "Count": {"1000"}}
		resp, err := p.client.Do(context.Background(), http.MethodGet, "/commonserviceitem", query, p.headers(), nil)
		if err != nil {
			return foundZone{}, err
		}
		var payload findResponse
		if err := httpapi.JSON(resp.Body, &payload); err != nil {
			return foundZone{}, err
		}
		for _, item := range payload.CommonServiceItems {
			name := item.Status.Zone
			if name == "" {
				name = item.Name
			}
			zones = append(zones, foundZone{name: name, item: item})
		}
		if payload.Total == 0 || from+len(payload.CommonServiceItems) >= payload.Total || len(payload.CommonServiceItems) == 0 {
			break
		}
		from += len(payload.CommonServiceItems)
	}
	names := make([]string, 0, len(zones))
	byName := make(map[string]foundZone, len(zones))
	for _, zone := range zones {
		names = append(names, zone.name)
		byName[strings.ToLower(dnsutil.Fqdn(zone.name))] = zone
	}
	name, err := dnsutil.ZoneName(fqdn, names)
	if err != nil {
		return foundZone{}, err
	}
	return byName[strings.ToLower(name)], nil
}

func (p *provider) update(item serviceItem, records []record) error {
	body, err := httpapi.EncodeJSON(map[string]any{
		"CommonServiceItem": map[string]any{
			"Settings":     map[string]any{"DNS": map[string]any{"ResourceRecordSets": records}},
			"SettingsHash": item.SettingsHash,
		},
	})
	if err != nil {
		return err
	}
	headers := p.headers()
	headers.Set("Content-Type", "application/json")
	_, err = p.client.Do(context.Background(), http.MethodPut, "/commonserviceitem/"+url.PathEscape(item.ID)+"/settings", nil, headers, body)
	return err
}

func (p *provider) headers() http.Header {
	header := make(http.Header)
	header.Set("Authorization", "Basic "+basicAuth(p.token, p.secret))
	return header
}

func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}
