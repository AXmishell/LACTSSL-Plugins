// Package ovh 是 OVH DNS 的重构实现。
package ovh

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
	ovhsig "github.com/AXmishell/LACTSSL-Plugins/sdk/ovh"
)

const defaultTTL = 60

type plugin struct{}

func (plugin) ID() string { return "ovh" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() {
	dns.Register(plugin{})
	dns.MarkVerifiable("ovh")
}

type provider struct {
	client  *httpapi.Client
	baseURL string
	signer  ovhsig.Signer
	token   string
	ttl     int
	time    int64
}

func newProvider(cfg map[string]string) (*provider, error) {
	base := ovhsig.Endpoint(cfg["ovh_endpoint"])
	if base == "" {
		return nil, fmt.Errorf("ovh: unknown endpoint %q", cfg["ovh_endpoint"])
	}
	p := &provider{client: httpapi.New(base), baseURL: base, ttl: defaultTTL}
	if token := strings.TrimSpace(cfg["ovh_access_token"]); token != "" {
		p.token = token
		return p, nil
	}
	appKey := strings.TrimSpace(cfg["ovh_application_key"])
	appSecret := strings.TrimSpace(cfg["ovh_application_secret"])
	consumerKey := strings.TrimSpace(cfg["ovh_consumer_key"])
	if appKey == "" || appSecret == "" || consumerKey == "" {
		return nil, errors.New("ovh: application key, application secret and consumer key are required")
	}
	p.signer = ovhsig.Signer{ApplicationKey: appKey, ApplicationSecret: appSecret, ConsumerKey: consumerKey}
	return p, nil
}

type record struct {
	ID        int64  `json:"id"`
	FieldType string `json:"fieldType"`
	SubDomain string `json:"subDomain"`
	Target    string `json:"target"`
	TTL       int    `json:"ttl"`
}

// Verify 只读检查凭据与 zone 归属，不写入任何 DNS 记录。
func (p *provider) Verify(_ context.Context, domain string) (string, error) {
	fqdn := dnsutil.ChallengeFQDN(domain)
	z, err := p.findZone(fqdn)
	if err != nil {
		return "", err
	}
	return z, nil
}

func (p *provider) Present(domainName, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("ovh: %w", err)
	}
	sub, err := dnsutil.RelativeName(fqdn, zone)
	if err != nil {
		return fmt.Errorf("ovh: %w", err)
	}
	body, _ := httpapi.EncodeJSON(record{FieldType: "TXT", SubDomain: sub, Target: txtValue, TTL: p.ttl})
	resp, err := p.do(http.MethodPost, "/domain/zone/"+url.PathEscape(zone)+"/record", nil, body)
	if err != nil {
		return fmt.Errorf("ovh: create record: %w", err)
	}
	_ = resp
	return p.refresh(zone)
}

func (p *provider) CleanUp(domainName, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	fqdn := dnsutil.ChallengeFQDN(domainName)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("ovh: %w", err)
	}
	sub, err := dnsutil.RelativeName(fqdn, zone)
	if err != nil {
		return fmt.Errorf("ovh: %w", err)
	}
	ids, err := p.listRecordIDs(zone, sub)
	if err != nil {
		return fmt.Errorf("ovh: %w", err)
	}
	removed := false
	for _, id := range ids {
		item, err := p.getRecord(zone, id)
		if err != nil {
			if httpapi.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("ovh: %w", err)
		}
		if !strings.EqualFold(item.FieldType, "TXT") || !dnsutil.ContainsTXT([]string{item.Target}, txtValue) {
			continue
		}
		if _, err := p.do(http.MethodDelete, "/domain/zone/"+url.PathEscape(zone)+"/record/"+strconv.FormatInt(id, 10), nil, nil); err != nil {
			return fmt.Errorf("ovh: delete record: %w", err)
		}
		removed = true
	}
	if removed {
		return p.refresh(zone)
	}
	return nil
}

func (p *provider) findZone(fqdn string) (string, error) {
	resp, err := p.do(http.MethodGet, "/domain/zone", nil, nil)
	if err != nil {
		return "", err
	}
	var zones []string
	if err := httpapi.JSON(resp.Body, &zones); err != nil {
		return "", err
	}
	return dnsutil.ZoneName(fqdn, zones)
}

func (p *provider) listRecordIDs(zone, sub string) ([]int64, error) {
	query := url.Values{"fieldType": {"TXT"}, "subDomain": {sub}}
	resp, err := p.do(http.MethodGet, "/domain/zone/"+url.PathEscape(zone)+"/record", query, nil)
	if err != nil {
		return nil, err
	}
	var ids []int64
	if err := httpapi.JSON(resp.Body, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func (p *provider) getRecord(zone string, id int64) (record, error) {
	resp, err := p.do(http.MethodGet, "/domain/zone/"+url.PathEscape(zone)+"/record/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return record{}, err
	}
	var item record
	if err := httpapi.JSON(resp.Body, &item); err != nil {
		return record{}, err
	}
	return item, nil
}

func (p *provider) refresh(zone string) error {
	_, err := p.do(http.MethodPost, "/domain/zone/"+url.PathEscape(zone)+"/refresh", nil, nil)
	return err
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
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	} else {
		if p.time == 0 {
			p.time = p.serverTime()
		}
		p.signer.Sign(req, body, p.time)
	}
	return p.client.Send(req)
}

func (p *provider) serverTime() int64 {
	req, err := p.client.NewRequest(context.Background(), http.MethodGet, "/auth/time", nil, nil, nil)
	if err != nil {
		return 0
	}
	resp, err := p.client.Send(req)
	if err != nil {
		return 0
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(resp.Body)), 10, 64)
	if err != nil {
		return 0
	}
	return value
}
