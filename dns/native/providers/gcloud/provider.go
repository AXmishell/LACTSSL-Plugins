// Package gcloud 是 Google Cloud DNS 的重构实现。
//
// 使用 service account JWT -> OAuth2 token + Cloud DNS REST API。
package gcloud

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/gcp"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

const (
	defaultBaseURL = "https://dns.googleapis.com"
	defaultTTL     = 60
)

type plugin struct{}

func (plugin) ID() string { return "gcloud" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() { dns.Register(plugin{}) }

type tokenProvider interface {
	Token(context.Context) (string, error)
}

type provider struct {
	client       *httpapi.Client
	tokens       tokenProvider
	project      string
	allowPrivate bool
	ttl          int
}

func newProvider(cfg map[string]string) (*provider, error) {
	raw := strings.TrimSpace(cfg["service_account"])
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	}
	if raw == "" {
		return nil, errors.New("gcloud: service_account is required")
	}
	account, err := gcp.ParseServiceAccount(raw)
	if err != nil {
		return nil, fmt.Errorf("gcloud: %w", err)
	}
	project := strings.TrimSpace(cfg["project"])
	if project == "" {
		project = account.ProjectID
	}
	if project == "" {
		return nil, errors.New("gcloud: project is required")
	}
	return &provider{
		client:       httpapi.New(defaultBaseURL),
		tokens:       &gcp.TokenSource{Account: account},
		project:      project,
		allowPrivate: parseBool(cfg["allow_private_zone"]),
		ttl:          defaultTTL,
	}, nil
}

type managedZone struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	DNSName    string `json:"dnsName"`
	Visibility string `json:"visibility"`
}

type listManagedZonesResponse struct {
	ManagedZones []managedZone `json:"managedZones"`
}

type rrset struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	TTL     int      `json:"ttl"`
	RRDatas []string `json:"rrdatas"`
}

type listRRsetsResponse struct {
	RRSets []rrset `json:"rrsets"`
}

type changeRequest struct {
	Additions []rrset `json:"additions,omitempty"`
	Deletions []rrset `json:"deletions,omitempty"`
}

func (p *provider) Present(domain, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domain)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("gcloud: %w", err)
	}
	existing, err := p.getRRset(zone.Name, fqdn)
	if err != nil {
		return fmt.Errorf("gcloud: %w", err)
	}
	value := dnsutil.QuoteTXT(keyAuth)
	values := []string{}
	var deletions []rrset
	if existing != nil {
		values = append(values, existing.RRDatas...)
		deletions = append(deletions, *existing)
	}
	if dnsutil.ContainsTXT(values, value) {
		return nil
	}
	values = append(values, value)
	_, err = p.change(zone.Name, changeRequest{
		Additions: []rrset{{Name: fqdn, Type: "TXT", TTL: p.ttl, RRDatas: values}},
		Deletions: deletions,
	})
	return err
}

func (p *provider) CleanUp(domain, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domain)
	zone, err := p.findZone(fqdn)
	if err != nil {
		return fmt.Errorf("gcloud: %w", err)
	}
	existing, err := p.getRRset(zone.Name, fqdn)
	if err != nil {
		return fmt.Errorf("gcloud: %w", err)
	}
	if existing == nil {
		return nil
	}
	remaining := dnsutil.RemoveTXT(existing.RRDatas, keyAuth)
	if len(remaining) == len(existing.RRDatas) {
		return nil
	}
	request := changeRequest{Deletions: []rrset{*existing}}
	if len(remaining) > 0 {
		request.Additions = []rrset{{Name: fqdn, Type: "TXT", TTL: existing.TTL, RRDatas: remaining}}
	}
	_, err = p.change(zone.Name, request)
	return err
}

func (p *provider) findZone(fqdn string) (managedZone, error) {
	path := "/dns/v1/projects/" + url.PathEscape(p.project) + "/managedZones"
	resp, err := p.do(http.MethodGet, path, url.Values{"maxResults": {"100"}}, nil)
	if err != nil {
		return managedZone{}, err
	}
	var payload listManagedZonesResponse
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return managedZone{}, err
	}
	names := make([]string, 0, len(payload.ManagedZones))
	byName := make(map[string]managedZone, len(payload.ManagedZones))
	for _, zone := range payload.ManagedZones {
		if !p.allowPrivate && strings.EqualFold(zone.Visibility, "private") {
			continue
		}
		names = append(names, zone.DNSName)
		byName[strings.ToLower(dnsutil.Fqdn(zone.DNSName))] = zone
	}
	name, err := dnsutil.ZoneName(fqdn, names)
	if err != nil {
		return managedZone{}, err
	}
	return byName[strings.ToLower(name)], nil
}

func (p *provider) getRRset(zoneName, fqdn string) (*rrset, error) {
	path := "/dns/v1/projects/" + url.PathEscape(p.project) + "/managedZones/" + url.PathEscape(zoneName) + "/rrsets"
	resp, err := p.do(http.MethodGet, path, url.Values{"name": {fqdn}, "type": {"TXT"}}, nil)
	if err != nil {
		return nil, err
	}
	var payload listRRsetsResponse
	if err := httpapi.JSON(resp.Body, &payload); err != nil {
		return nil, err
	}
	for _, item := range payload.RRSets {
		if strings.EqualFold(dnsutil.Fqdn(item.Name), dnsutil.Fqdn(fqdn)) && strings.EqualFold(item.Type, "TXT") {
			return &item, nil
		}
	}
	return nil, nil
}

func (p *provider) change(zoneName string, request changeRequest) ([]byte, error) {
	body, err := httpapi.EncodeJSON(request)
	if err != nil {
		return nil, err
	}
	path := "/dns/v1/projects/" + url.PathEscape(p.project) + "/managedZones/" + url.PathEscape(zoneName) + "/changes"
	resp, err := p.do(http.MethodPost, path, nil, body)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (p *provider) do(method, path string, query url.Values, body []byte) (*httpapi.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, err := p.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+token)
	if body != nil {
		header.Set("Content-Type", "application/json")
	}
	return p.client.Do(ctx, method, path, query, header, body)
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
