// Package volcengine 是火山引擎 DNS 的重构实现。
package volcengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
	volcsig "github.com/AXmishell/LACTSSL-Plugins/sdk/volcengine"
)

const (
	apiVersion = "2018-08-01"
	defaultTTL = 600
)

type plugin struct{}

func (plugin) ID() string { return "volcengine" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() { dns.Register(plugin{}) }

type provider struct {
	client  *httpapi.Client
	signer  volcsig.Signer
	baseURL string
	ttl     int
}

func newProvider(cfg map[string]string) (*provider, error) {
	access := strings.TrimSpace(cfg["access_key"])
	secret := strings.TrimSpace(cfg["secret_key"])
	if access == "" || secret == "" {
		return nil, errors.New("volcengine: access_key and secret_key are required")
	}
	scheme := strings.TrimSpace(cfg["api_scheme"])
	if scheme == "" {
		scheme = "https"
	}
	host := strings.TrimSpace(cfg["api_host"])
	if host == "" {
		host = "open.volcengineapi.com"
	}
	region := strings.TrimSpace(cfg["region"])
	if region == "" {
		region = "cn-beijing"
	}
	base := scheme + "://" + host
	return &provider{
		client:  httpapi.New(base),
		signer:  volcsig.Signer{AccessKey: access, SecretKey: secret, Region: region, Service: "dns"},
		baseURL: base,
		ttl:     defaultTTL,
	}, nil
}

type zone struct {
	ZID      int64  `json:"ZID"`
	ZoneName string `json:"ZoneName"`
}

type listZonesResult struct {
	Total int    `json:"Total"`
	Zones []zone `json:"Zones"`
}

type record struct {
	RecordID string `json:"RecordID"`
	Host     string `json:"Host"`
	Type     string `json:"Type"`
	Value    string `json:"Value"`
	TTL      int64  `json:"TTL"`
}

type listRecordsResult struct {
	TotalCount int      `json:"TotalCount"`
	Records    []record `json:"Records"`
}

type envelope struct {
	ResponseMetadata struct {
		Error struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"ResponseMetadata"`
	Result json.RawMessage `json:"Result"`
}

func (p *provider) Present(domainName, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("volcengine: %w", err)
	}
	sub, err := dnsutil.RelativeName(fqdn, zone.ZoneName)
	if err != nil {
		return fmt.Errorf("volcengine: %w", err)
	}
	body, _ := httpapi.EncodeJSON(map[string]any{
		"Host": sub, "TTL": p.ttl, "Type": "TXT", "Value": txtValue, "ZID": zone.ZID,
	})
	_, err = p.call(http.MethodPost, "CreateRecord", nil, body, nil)
	return err
}

func (p *provider) CleanUp(domainName, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("volcengine: %w", err)
	}
	sub, err := dnsutil.RelativeName(fqdn, zone.ZoneName)
	if err != nil {
		return fmt.Errorf("volcengine: %w", err)
	}
	records, err := p.listRecords(zone.ZID, sub)
	if err != nil {
		return fmt.Errorf("volcengine: %w", err)
	}
	for _, item := range records {
		if !strings.EqualFold(item.Type, "TXT") || !dnsutil.ContainsTXT([]string{item.Value}, txtValue) {
			continue
		}
		body, _ := httpapi.EncodeJSON(map[string]any{"RecordID": item.RecordID})
		if _, err := p.call(http.MethodPost, "DeleteRecord", nil, body, nil); err != nil {
			return fmt.Errorf("volcengine: delete record: %w", err)
		}
	}
	return nil
}

func (p *provider) findZone(fqdn string) (zone, error) {
	labels := strings.Split(dnsutil.UnFqdn(fqdn), ".")
	for index := 0; index < len(labels)-1; index++ {
		candidate := strings.Join(labels[index:], ".")
		query := url.Values{"Key": {candidate}, "SearchMode": {"exact"}}
		var result listZonesResult
		if _, err := p.call(http.MethodGet, "ListZones", query, nil, &result); err != nil {
			return zone{}, err
		}
		if result.Total == 1 && len(result.Zones) == 1 {
			return result.Zones[0], nil
		}
	}
	return zone{}, fmt.Errorf("zone not found for %s", fqdn)
}

func (p *provider) listRecords(zid int64, sub string) ([]record, error) {
	query := url.Values{
		"ZID":        {fmt.Sprint(zid)},
		"Host":       {sub},
		"Type":       {"TXT"},
		"PageNumber": {"1"},
		"PageSize":   {"100"},
		"SearchMode": {"exact"},
	}
	var result listRecordsResult
	if _, err := p.call(http.MethodGet, "ListRecords", query, nil, &result); err != nil {
		return nil, err
	}
	return result.Records, nil
}

func (p *provider) call(method, action string, query url.Values, body []byte, out any) (json.RawMessage, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("Action", action)
	query.Set("Version", apiVersion)
	header := make(http.Header)
	if body != nil {
		header.Set("Content-Type", "application/json")
	}
	req, err := p.client.NewRequest(context.Background(), method, "/", query, header, body)
	if err != nil {
		return nil, err
	}
	if err := p.signer.Sign(req, body); err != nil {
		return nil, err
	}
	resp, err := p.client.Send(req)
	if err != nil {
		return nil, err
	}
	var payload envelope
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return nil, err
	}
	if payload.ResponseMetadata.Error.Code != "" {
		return nil, fmt.Errorf("%s: %s", payload.ResponseMetadata.Error.Code, payload.ResponseMetadata.Error.Message)
	}
	if out != nil && len(payload.Result) > 0 {
		if err := json.Unmarshal(payload.Result, out); err != nil {
			return nil, err
		}
	}
	return payload.Result, nil
}
