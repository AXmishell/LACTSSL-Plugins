// Package aliesa 是阿里云 ESA DNS 的重构实现。
package aliesa

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
	apiVersion = "2024-09-10"
	defaultTTL = 600
)

type plugin struct{}

func (plugin) ID() string { return "aliesa" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() {
	dns.Register(plugin{})
	dns.MarkVerifiable("aliesa")
}

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
		return nil, errors.New("aliesa: access_key_id and access_key_secret are required")
	}
	region := strings.TrimSpace(cfg["region_id"])
	if region == "" {
		region = "cn-hangzhou"
	}
	endpoint := "https://esa." + region + ".aliyuncs.com"
	return &provider{
		client:   httpapi.New(endpoint),
		signer:   alibaba.Signer{AccessKeyID: key, AccessKeySecret: secret},
		endpoint: endpoint,
		ttl:      defaultTTL,
	}, nil
}

type site struct {
	SiteID   int64  `json:"SiteId"`
	SiteName string `json:"SiteName"`
}

type listSitesResponse struct {
	TotalCount int    `json:"TotalCount"`
	Sites      []site `json:"Sites"`
}

type recordData struct {
	Value string `json:"Value"`
}

type record struct {
	RecordID   int64      `json:"RecordId"`
	RecordName string     `json:"RecordName"`
	Type       string     `json:"Type"`
	Data       recordData `json:"Data"`
	TTL        int        `json:"Ttl"`
}

type listRecordsResponse struct {
	TotalCount int      `json:"TotalCount"`
	Records    []record `json:"Records"`
}

// Verify 只读检查凭据与 zone 归属，不写入任何 DNS 记录。
func (p *provider) Verify(_ context.Context, domain string) (string, error) {
	fqdn := dnsutil.ChallengeFQDN(domain)
	s, err := p.findSite(fqdn)
	if err != nil {
		return "", err
	}
	return s.SiteName, nil
}

func (p *provider) Present(domainName, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	fqdn := dnsutil.ChallengeFQDN(domainName)
	site, err := p.findSite(fqdn)
	if err != nil {
		return fmt.Errorf("aliesa: %w", err)
	}
	records, err := p.listRecords(site.SiteID, fqdn)
	if err != nil {
		return fmt.Errorf("aliesa: %w", err)
	}
	for _, item := range records {
		if strings.EqualFold(item.Type, "TXT") && dnsutil.ContainsTXT([]string{item.Data.Value}, txtValue) {
			return nil
		}
	}
	return p.signer.Do(context.Background(), p.client, p.endpoint, "CreateRecord", apiVersion, map[string]string{
		"SiteId":     fmt.Sprint(site.SiteID),
		"Type":       "TXT",
		"RecordName": dnsutil.UnFqdn(fqdn),
		"Ttl":        fmt.Sprint(p.ttl),
		"Data.Value": txtValue,
	}, nil)
}

func (p *provider) CleanUp(domainName, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	fqdn := dnsutil.ChallengeFQDN(domainName)
	site, err := p.findSite(fqdn)
	if err != nil {
		return fmt.Errorf("aliesa: %w", err)
	}
	records, err := p.listRecords(site.SiteID, fqdn)
	if err != nil {
		return fmt.Errorf("aliesa: %w", err)
	}
	for _, item := range records {
		if !strings.EqualFold(item.Type, "TXT") || !dnsutil.ContainsTXT([]string{item.Data.Value}, txtValue) {
			continue
		}
		if err := p.signer.Do(context.Background(), p.client, p.endpoint, "DeleteRecord", apiVersion, map[string]string{
			"RecordId": fmt.Sprint(item.RecordID),
		}, nil); err != nil {
			return fmt.Errorf("aliesa: delete record: %w", err)
		}
	}
	return nil
}

func (p *provider) findSite(fqdn string) (site, error) {
	var sites []site
	for page := 1; page <= 20; page++ {
		var payload listSitesResponse
		err := p.signer.Do(context.Background(), p.client, p.endpoint, "ListSites", apiVersion, map[string]string{
			"PageNumber": fmt.Sprint(page),
			"PageSize":   "100",
		}, &payload)
		if err != nil {
			return site{}, err
		}
		sites = append(sites, payload.Sites...)
		if payload.TotalCount == 0 || len(sites) >= payload.TotalCount {
			break
		}
	}
	names := make([]string, 0, len(sites))
	byName := make(map[string]site, len(sites))
	for _, item := range sites {
		names = append(names, item.SiteName)
		byName[strings.ToLower(dnsutil.Fqdn(item.SiteName))] = item
	}
	name, err := dnsutil.ZoneName(fqdn, names)
	if err != nil {
		return site{}, err
	}
	return byName[strings.ToLower(name)], nil
}

func (p *provider) listRecords(siteID int64, fqdn string) ([]record, error) {
	var records []record
	for page := 1; page <= 20; page++ {
		var payload listRecordsResponse
		err := p.signer.Do(context.Background(), p.client, p.endpoint, "ListRecords", apiVersion, map[string]string{
			"SiteId":          fmt.Sprint(siteID),
			"RecordName":      dnsutil.UnFqdn(fqdn),
			"RecordMatchType": "exact",
			"Type":            "TXT",
			"PageNumber":      fmt.Sprint(page),
			"PageSize":        "100",
		}, &payload)
		if err != nil {
			return nil, err
		}
		records = append(records, payload.Records...)
		if payload.TotalCount == 0 || len(records) >= payload.TotalCount {
			break
		}
	}
	return records, nil
}
