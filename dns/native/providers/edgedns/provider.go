// Package edgedns 是 Akamai EdgeDNS 的自研实现。
//
// 使用 EdgeGrid EG1-HMAC-SHA256 签名 + Config DNS v2 REST API。
package edgedns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/edgegrid"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

const defaultTTL = 60

type plugin struct{}

func (plugin) ID() string { return "edgedns" }

func (plugin) Aliases() []string { return []string{"fastdns"} }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() { dns.Register(plugin{}) }

type provider struct {
	client  *httpapi.Client
	signer  edgegrid.Config
	baseURL string
	ttl     int
}

func newProvider(cfg map[string]string) (*provider, error) {
	host := firstNonEmpty(cfg["akamai_host"], os.Getenv("AKAMAI_HOST"))
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	conf := edgegrid.Config{
		Host:         host,
		ClientToken:  firstNonEmpty(cfg["akamai_client_token"], os.Getenv("AKAMAI_CLIENT_TOKEN")),
		ClientSecret: firstNonEmpty(cfg["akamai_client_secret"], os.Getenv("AKAMAI_CLIENT_SECRET")),
		AccessToken:  firstNonEmpty(cfg["akamai_access_token"], os.Getenv("AKAMAI_ACCESS_TOKEN")),
		AccountKey:   strings.TrimSpace(cfg["akamai_account_switch_key"]),
	}
	if conf.Host == "" || conf.ClientToken == "" || conf.ClientSecret == "" || conf.AccessToken == "" {
		path := firstNonEmpty(cfg["akamai_edgerc"], "~/.edgerc")
		section := firstNonEmpty(cfg["akamai_edgerc_section"], "default")
		values, err := readEdgeRC(path, section)
		if err != nil {
			return nil, fmt.Errorf("edgedns: %w", err)
		}
		conf.Host = firstNonEmpty(conf.Host, values["host"])
		conf.ClientToken = firstNonEmpty(conf.ClientToken, values["client_token"])
		conf.ClientSecret = firstNonEmpty(conf.ClientSecret, values["client_secret"])
		conf.AccessToken = firstNonEmpty(conf.AccessToken, values["access_token"])
		if conf.AccountKey == "" {
			conf.AccountKey = values["account_key"]
		}
	}
	if conf.Host == "" || conf.ClientToken == "" || conf.ClientSecret == "" || conf.AccessToken == "" {
		return nil, errors.New("edgedns: host, client_token, client_secret and access_token are required")
	}
	base := "https://" + strings.TrimSuffix(conf.Host, "/")
	return &provider{
		client:  httpapi.New(base),
		signer:  conf,
		baseURL: base,
		ttl:     defaultTTL,
	}, nil
}

type zoneList struct {
	Zones []struct {
		Zone string `json:"zone"`
	} `json:"zones"`
}

type record struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	TTL    int      `json:"ttl"`
	RData  []string `json:"rdata"`
	Active bool     `json:"active"`
}

func (p *provider) Present(domain, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domain)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("edgedns: %w", err)
	}
	name := dnsutil.UnFqdn(fqdn)
	existing, err := p.getRecord(zone, name)
	if err != nil {
		return fmt.Errorf("edgedns: %w", err)
	}
	value := dnsutil.QuoteTXT(keyAuth)
	if existing != nil {
		if dnsutil.ContainsTXT(existing.RData, value) {
			return nil
		}
		existing.RData = append(existing.RData, value)
		existing.TTL = p.ttl
		return p.putRecord(zone, name, *existing)
	}
	return p.createRecord(zone, record{
		Name:   name,
		Type:   "TXT",
		TTL:    p.ttl,
		RData:  []string{value},
		Active: true,
	})
}

func (p *provider) CleanUp(domain, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domain)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("edgedns: %w", err)
	}
	name := dnsutil.UnFqdn(fqdn)
	existing, err := p.getRecord(zone, name)
	if err != nil {
		return fmt.Errorf("edgedns: %w", err)
	}
	if existing == nil {
		return nil
	}
	remaining := dnsutil.RemoveTXT(existing.RData, keyAuth)
	if len(remaining) == len(existing.RData) {
		return nil
	}
	if len(remaining) == 0 {
		return p.deleteRecord(zone, name)
	}
	existing.RData = remaining
	return p.putRecord(zone, name, *existing)
}

func (p *provider) findZone(fqdn string) (string, error) {
	resp, err := p.do(http.MethodGet, "/config-dns/v2/zones", url.Values{"showAll": {"true"}, "pageSize": {"1000"}}, nil)
	if err != nil {
		return "", err
	}
	var payload zoneList
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return "", err
	}
	names := make([]string, 0, len(payload.Zones))
	for _, item := range payload.Zones {
		names = append(names, item.Zone)
	}
	name, err := dnsutil.ZoneName(fqdn, names)
	if err != nil {
		return "", err
	}
	return dnsutil.UnFqdn(name), nil
}

func (p *provider) getRecord(zone, name string) (*record, error) {
	path := "/config-dns/v2/zones/" + url.PathEscape(zone) + "/names/" + url.PathEscape(name) + "/types/TXT"
	resp, err := p.do(http.MethodGet, path, nil, nil)
	if err != nil {
		if httpapi.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var payload record
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func (p *provider) createRecord(zone string, value record) error {
	body, err := httpapi.EncodeJSON(value)
	if err != nil {
		return err
	}
	path := "/config-dns/v2/zones/" + url.PathEscape(zone) + "/names/" + url.PathEscape(value.Name) + "/types/TXT"
	_, err = p.do(http.MethodPost, path, nil, body)
	return err
}

func (p *provider) putRecord(zone, name string, value record) error {
	body, err := httpapi.EncodeJSON(value)
	if err != nil {
		return err
	}
	path := "/config-dns/v2/zones/" + url.PathEscape(zone) + "/names/" + url.PathEscape(name) + "/types/TXT"
	_, err = p.do(http.MethodPut, path, nil, body)
	return err
}

func (p *provider) deleteRecord(zone, name string) error {
	path := "/config-dns/v2/zones/" + url.PathEscape(zone) + "/names/" + url.PathEscape(name) + "/types/TXT"
	_, err := p.do(http.MethodDelete, path, nil, nil)
	if httpapi.IsNotFound(err) {
		return nil
	}
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
	if err := p.signer.Sign(req, body); err != nil {
		return nil, err
	}
	return p.client.Send(req)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func readEdgeRC(path, section string) (map[string]string, error) {
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read edgerc: %w", err)
	}
	current := ""
	values := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if current != section {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("edgerc section %q not found", section)
	}
	return values, nil
}
