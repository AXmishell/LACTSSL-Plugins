// Package alidns 是阿里云 DNS 的重构实现。
package alidns

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/alibaba"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

const (
	defaultEndpoint = "https://alidns.aliyuncs.com"
	apiVersion      = "2015-01-09"
	defaultTTL      = 600
)

type plugin struct{}

func (plugin) ID() string { return "alidns" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() { dns.Register(plugin{}) }

type provider struct {
	client   *httpapi.Client
	signer   alibaba.Signer
	endpoint string
	ttl      int
}

func newProvider(cfg map[string]string) (*provider, error) {
	key := strings.TrimSpace(cfg["access_key_id"])
	secret := strings.TrimSpace(cfg["access_key_secret"])
	if key == "" || secret == "" {
		return nil, errors.New("alidns: access_key_id and access_key_secret are required")
	}
	return &provider{
		client:   httpapi.New(defaultEndpoint),
		signer:   alibaba.Signer{AccessKeyID: key, AccessKeySecret: secret},
		endpoint: defaultEndpoint,
		ttl:      defaultTTL,
	}, nil
}

type domain struct {
	DomainName string `json:"DomainName"`
	DomainID   string `json:"DomainId"`
	PunyCode   string `json:"PunyCode"`
}

type describeDomainsResponse struct {
	TotalCount int `json:"TotalCount"`
	PageNumber int `json:"PageNumber"`
	PageSize   int `json:"PageSize"`
	Domains    struct {
		Domain []domain `json:"Domain"`
	} `json:"Domains"`
}

type record struct {
	RecordID string `json:"RecordId"`
	RR       string `json:"RR"`
	Type     string `json:"Type"`
	Value    string `json:"Value"`
}

type describeRecordsResponse struct {
	TotalCount    int `json:"TotalCount"`
	PageNumber    int `json:"PageNumber"`
	PageSize      int `json:"PageSize"`
	DomainRecords struct {
		Record []record `json:"Record"`
	} `json:"DomainRecords"`
}

func (p *provider) Present(domainName, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("alidns: %w", err)
	}
	rr, err := dnsutil.RelativeName(fqdn, zone.DomainName)
	if err != nil {
		return fmt.Errorf("alidns: %w", err)
	}
	existing, err := p.listRecords(zone.DomainName)
	if err != nil {
		return fmt.Errorf("alidns: %w", err)
	}
	for _, item := range existing {
		if item.RR == rr && strings.EqualFold(item.Type, "TXT") && dnsutil.ContainsTXT([]string{item.Value}, keyAuth) {
			return nil
		}
	}
	return p.signer.Do(context.Background(), p.client, p.endpoint, "AddDomainRecord", apiVersion, map[string]string{
		"DomainName": zone.DomainName,
		"RR":         rr,
		"Type":       "TXT",
		"Value":      keyAuth,
		"TTL":        fmt.Sprint(p.ttl),
	}, nil)
}

func (p *provider) CleanUp(domainName, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("alidns: %w", err)
	}
	rr, err := dnsutil.RelativeName(fqdn, zone.DomainName)
	if err != nil {
		return fmt.Errorf("alidns: %w", err)
	}
	records, err := p.listRecords(zone.DomainName)
	if err != nil {
		return fmt.Errorf("alidns: %w", err)
	}
	for _, item := range records {
		if item.RR != rr || !strings.EqualFold(item.Type, "TXT") || !dnsutil.ContainsTXT([]string{item.Value}, keyAuth) {
			continue
		}
		if err := p.signer.Do(context.Background(), p.client, p.endpoint, "DeleteDomainRecord", apiVersion, map[string]string{
			"RecordId": item.RecordID,
		}, nil); err != nil {
			return fmt.Errorf("alidns: delete record: %w", err)
		}
	}
	return nil
}

func (p *provider) findZone(fqdn string) (domain, error) {
	var domains []domain
	for page := 1; page <= 20; page++ {
		var payload describeDomainsResponse
		err := p.signer.Do(context.Background(), p.client, p.endpoint, "DescribeDomains", apiVersion, map[string]string{
			"PageNumber": fmt.Sprint(page),
			"PageSize":   "100",
		}, &payload)
		if err != nil {
			return domain{}, err
		}
		domains = append(domains, payload.Domains.Domain...)
		if payload.TotalCount == 0 || len(domains) >= payload.TotalCount {
			break
		}
	}
	names := make([]string, 0, len(domains))
	byName := make(map[string]domain, len(domains))
	for _, item := range domains {
		names = append(names, item.DomainName)
		byName[strings.ToLower(dnsutil.Fqdn(item.DomainName))] = item
	}
	name, err := dnsutil.ZoneName(fqdn, names)
	if err != nil {
		return domain{}, err
	}
	return byName[strings.ToLower(name)], nil
}

func (p *provider) listRecords(zoneName string) ([]record, error) {
	var records []record
	for page := 1; page <= 20; page++ {
		var payload describeRecordsResponse
		err := p.signer.Do(context.Background(), p.client, p.endpoint, "DescribeDomainRecords", apiVersion, map[string]string{
			"DomainName": zoneName,
			"PageNumber": fmt.Sprint(page),
			"PageSize":   "500",
		}, &payload)
		if err != nil {
			return nil, err
		}
		records = append(records, payload.DomainRecords.Record...)
		if payload.TotalCount == 0 || len(records) >= payload.TotalCount {
			break
		}
	}
	return records, nil
}
