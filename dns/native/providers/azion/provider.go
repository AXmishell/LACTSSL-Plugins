// Package azion 是 Azion Edge DNS 的重构实现。
package azion

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
	defaultBaseURL  = "https://api.azionapi.net"
	defaultTTL      = 60
	defaultPageSize = 50
)

type plugin struct{}

func (plugin) ID() string { return "azion" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() {
	dns.Register(plugin{})
	dns.MarkVerifiable("azion")
}

type provider struct {
	client   *httpapi.Client
	token    string
	pageSize int
	ttl      int
}

func newProvider(cfg map[string]string) (*provider, error) {
	token := strings.TrimSpace(cfg["azion_personal_token"])
	if token == "" {
		return nil, errors.New("azion: azion_personal_token is required")
	}
	pageSize := defaultPageSize
	if raw := strings.TrimSpace(cfg["azion_page_size"]); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			pageSize = value
		}
	}
	return &provider{client: httpapi.New(defaultBaseURL), token: token, pageSize: pageSize, ttl: defaultTTL}, nil
}

type zone struct {
	ID     int32  `json:"id"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
}

type zonesResponse struct {
	Results    []zone `json:"results"`
	TotalPages int    `json:"total_pages"`
}

type record struct {
	ID          int32    `json:"record_id"`
	Entry       string   `json:"entry"`
	RecordType  string   `json:"record_type"`
	AnswersList []string `json:"answers_list"`
	TTL         int      `json:"ttl"`
}

type recordsResponse struct {
	Results    []record `json:"results"`
	TotalPages int      `json:"total_pages"`
}

// Verify 只读检查凭据与 zone 归属，不写入任何 DNS 记录。
func (p *provider) Verify(_ context.Context, domain string) (string, error) {
	fqdn := dnsutil.ChallengeFQDN(domain)
	z, err := p.findZone(fqdn)
	if err != nil {
		return "", err
	}
	return z.Name, nil
}

func (p *provider) Present(domainName, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("azion: %w", err)
	}
	entry, err := dnsutil.RelativeName(fqdn, zone.Domain)
	if err != nil {
		return fmt.Errorf("azion: %w", err)
	}
	existing, err := p.findRecord(zone.ID, entry)
	if err != nil {
		return fmt.Errorf("azion: %w", err)
	}
	if existing == nil {
		body, _ := httpapi.EncodeJSON(map[string]any{
			"entry": entry, "record_type": "TXT", "answers_list": []string{txtValue}, "ttl": p.ttl,
		})
		_, err = p.do(http.MethodPost, "/intelligent_dns/"+strconv.Itoa(int(zone.ID))+"/records", nil, body)
		return err
	}
	if dnsutil.ContainsTXT(existing.AnswersList, txtValue) {
		return nil
	}
	existing.AnswersList = append(existing.AnswersList, txtValue)
	return p.putRecord(zone.ID, *existing)
}

func (p *provider) CleanUp(domainName, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("azion: %w", err)
	}
	entry, err := dnsutil.RelativeName(fqdn, zone.Domain)
	if err != nil {
		return fmt.Errorf("azion: %w", err)
	}
	existing, err := p.findRecord(zone.ID, entry)
	if err != nil {
		return fmt.Errorf("azion: %w", err)
	}
	if existing == nil {
		return nil
	}
	remaining := dnsutil.RemoveTXT(existing.AnswersList, txtValue)
	if len(remaining) == len(existing.AnswersList) {
		return nil
	}
	if len(remaining) == 0 {
		_, err = p.do(http.MethodDelete, "/intelligent_dns/"+strconv.Itoa(int(zone.ID))+"/records/"+strconv.Itoa(int(existing.ID)), nil, nil)
		if httpapi.IsNotFound(err) {
			return nil
		}
		return err
	}
	existing.AnswersList = remaining
	return p.putRecord(zone.ID, *existing)
}

func (p *provider) findZone(fqdn string) (zone, error) {
	var zones []zone
	for page := 1; page <= 20; page++ {
		query := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(p.pageSize)}}
		resp, err := p.do(http.MethodGet, "/intelligent_dns", query, nil)
		if err != nil {
			return zone{}, err
		}
		var payload zonesResponse
		if err := httpapi.JSON(resp.Body, &payload); err != nil {
			return zone{}, err
		}
		zones = append(zones, payload.Results...)
		if payload.TotalPages == 0 || page >= payload.TotalPages {
			break
		}
	}
	names := make([]string, 0, len(zones))
	byName := make(map[string]zone, len(zones))
	for _, item := range zones {
		name := item.Domain
		if name == "" {
			name = item.Name
		}
		names = append(names, name)
		byName[strings.ToLower(dnsutil.Fqdn(name))] = item
	}
	name, err := dnsutil.ZoneName(fqdn, names)
	if err != nil {
		return zone{}, err
	}
	return byName[strings.ToLower(name)], nil
}

func (p *provider) findRecord(zoneID int32, entry string) (*record, error) {
	for page := 1; page <= 20; page++ {
		query := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(p.pageSize)}}
		resp, err := p.do(http.MethodGet, "/intelligent_dns/"+strconv.Itoa(int(zoneID))+"/records", query, nil)
		if err != nil {
			return nil, err
		}
		var payload recordsResponse
		if err := httpapi.JSON(resp.Body, &payload); err != nil {
			return nil, err
		}
		for _, item := range payload.Results {
			if strings.EqualFold(item.Entry, entry) && strings.EqualFold(item.RecordType, "TXT") {
				return &item, nil
			}
		}
		if payload.TotalPages == 0 || page >= payload.TotalPages {
			break
		}
	}
	return nil, nil
}

func (p *provider) putRecord(zoneID int32, value record) error {
	body, _ := httpapi.EncodeJSON(map[string]any{
		"entry": value.Entry, "record_type": "TXT", "answers_list": value.AnswersList, "ttl": value.TTL,
	})
	_, err := p.do(http.MethodPut, "/intelligent_dns/"+strconv.Itoa(int(zoneID))+"/records/"+strconv.Itoa(int(value.ID)), nil, body)
	return err
}

func (p *provider) do(method, path string, query url.Values, body []byte) (*httpapi.Response, error) {
	header := make(http.Header)
	header.Set("Authorization", "Token "+p.token)
	header.Set("Accept", "application/json; version=3")
	if body != nil {
		header.Set("Content-Type", "application/json")
	}
	return p.client.Do(context.Background(), method, path, query, header, body)
}
