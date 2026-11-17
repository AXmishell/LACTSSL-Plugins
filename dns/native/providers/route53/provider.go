// Package route53 是 AWS Route 53 的重构实现。
//
// 使用 SigV4 + Route 53 REST XML API，覆盖 hosted zone 查找与 TXT 记录增删。
package route53

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/sigv4"
)

const (
	defaultBaseURL = "https://route53.amazonaws.com"
	apiPath        = "/2013-04-01"
	defaultTTL     = 10
)

type plugin struct{}

func (plugin) ID() string { return "route53" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() { dns.Register(plugin{}) }

type provider struct {
	client         *httpapi.Client
	credentials    sigv4.Credentials
	hostedZoneID   string
	privateZone    bool
	ttl            int
	waitForChange  bool
	changeTimeout  time.Duration
	changeInterval time.Duration
	now            func() time.Time
}

func newProvider(cfg map[string]string) (*provider, error) {
	access := firstNonEmpty(cfg["access_key_id"], os.Getenv("AWS_ACCESS_KEY_ID"))
	secret := firstNonEmpty(cfg["secret_access_key"], os.Getenv("AWS_SECRET_ACCESS_KEY"))
	session := firstNonEmpty(cfg["session_token"], os.Getenv("AWS_SESSION_TOKEN"))
	if access == "" || secret == "" {
		return nil, errors.New("route53: access_key_id and secret_access_key are required")
	}
	return &provider{
		client:         httpapi.New(defaultBaseURL),
		credentials:    sigv4.Credentials{AccessKeyID: access, SecretAccessKey: secret, SessionToken: session},
		hostedZoneID:   strings.TrimSpace(cfg["hosted_zone_id"]),
		privateZone:    parseBool(cfg["private_zone"]),
		ttl:            defaultTTL,
		waitForChange:  true,
		changeTimeout:  45 * time.Second,
		changeInterval: 2 * time.Second,
		now:            time.Now,
	}, nil
}

type hostedZone struct {
	ID     string `xml:"Id"`
	Name   string `xml:"Name"`
	Config struct {
		PrivateZone bool `xml:"PrivateZone"`
	} `xml:"Config"`
}

type listHostedZonesResponse struct {
	HostedZones []hostedZone `xml:"HostedZones>HostedZone"`
	IsTruncated bool         `xml:"IsTruncated"`
	NextMarker  string       `xml:"NextMarker"`
}

type resourceRecord struct {
	Value string `xml:"Value"`
}

type resourceRecordSet struct {
	Name            string           `xml:"Name"`
	Type            string           `xml:"Type"`
	TTL             int64            `xml:"TTL"`
	ResourceRecords []resourceRecord `xml:"ResourceRecords>ResourceRecord"`
}

type listResourceRecordSetsResponse struct {
	ResourceRecordSets []resourceRecordSet `xml:"ResourceRecordSets>ResourceRecordSet"`
	IsTruncated        bool                `xml:"IsTruncated"`
	NextRecordName     string              `xml:"NextRecordName"`
	NextRecordType     string              `xml:"NextRecordType"`
}

type changeRequest struct {
	XMLName     xml.Name    `xml:"https://route53.amazonaws.com/doc/2013-04-01/ ChangeResourceRecordSetsRequest"`
	ChangeBatch changeBatch `xml:"ChangeBatch"`
}

type changeBatch struct {
	Comment string   `xml:"Comment,omitempty"`
	Changes []change `xml:"Changes>Change"`
}

type change struct {
	Action            string            `xml:"Action"`
	ResourceRecordSet resourceRecordSet `xml:"ResourceRecordSet"`
}

type changeResponse struct {
	ChangeInfo struct {
		ID     string `xml:"Id"`
		Status string `xml:"Status"`
	} `xml:"ChangeInfo"`
}

func (p *provider) Present(domain, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domain)
	zoneID, err := p.findZoneID(fqdn)
	if err != nil {
		return fmt.Errorf("route53: %w", err)
	}
	existing, err := p.getRecordSet(zoneID, fqdn)
	if err != nil {
		return fmt.Errorf("route53: %w", err)
	}
	values := []string{}
	if existing != nil {
		for _, record := range existing.ResourceRecords {
			values = append(values, record.Value)
		}
	}
	quoted := dnsutil.QuoteTXT(keyAuth)
	if dnsutil.ContainsTXT(values, quoted) {
		return nil
	}
	values = append(values, quoted)
	return p.change(zoneID, "UPSERT", resourceRecordSet{
		Name:            fqdn,
		Type:            "TXT",
		TTL:             int64(p.ttl),
		ResourceRecords: toResourceRecords(values),
	})
}

func (p *provider) CleanUp(domain, _ string, keyAuth string) error {
	fqdn := dnsutil.ChallengeFQDN(domain)
	zoneID, err := p.findZoneID(fqdn)
	if err != nil {
		return fmt.Errorf("route53: %w", err)
	}
	existing, err := p.getRecordSet(zoneID, fqdn)
	if err != nil {
		return fmt.Errorf("route53: %w", err)
	}
	if existing == nil {
		return nil
	}
	values := make([]string, 0, len(existing.ResourceRecords))
	for _, record := range existing.ResourceRecords {
		values = append(values, record.Value)
	}
	remaining := dnsutil.RemoveTXT(values, dnsutil.QuoteTXT(keyAuth))
	if len(remaining) == len(values) {
		return nil
	}
	if len(remaining) == 0 {
		return p.change(zoneID, "DELETE", *existing)
	}
	return p.change(zoneID, "UPSERT", resourceRecordSet{
		Name:            fqdn,
		Type:            "TXT",
		TTL:             int64(p.ttl),
		ResourceRecords: toResourceRecords(remaining),
	})
}

func (p *provider) findZoneID(fqdn string) (string, error) {
	if p.hostedZoneID != "" {
		return normalizeZoneID(p.hostedZoneID), nil
	}
	var zones []hostedZone
	marker := ""
	for page := 0; page < 20; page++ {
		query := url.Values{"maxitems": {"100"}}
		if marker != "" {
			query.Set("marker", marker)
		}
		resp, err := p.do(http.MethodGet, apiPath+"/hostedzone", query, nil, nil)
		if err != nil {
			return "", err
		}
		var payload listHostedZonesResponse
		if err := httpapi.XML(resp.Body, &payload); err != nil {
			return "", err
		}
		zones = append(zones, payload.HostedZones...)
		if !payload.IsTruncated || payload.NextMarker == "" {
			break
		}
		marker = payload.NextMarker
	}
	names := make([]string, 0, len(zones))
	byName := make(map[string]hostedZone, len(zones))
	for _, zone := range zones {
		if p.privateZone != zone.Config.PrivateZone {
			continue
		}
		names = append(names, zone.Name)
		byName[strings.ToLower(dnsutil.Fqdn(zone.Name))] = zone
	}
	name, err := dnsutil.ZoneName(fqdn, names)
	if err != nil {
		return "", err
	}
	return normalizeZoneID(byName[strings.ToLower(name)].ID), nil
}

func (p *provider) getRecordSet(zoneID, fqdn string) (*resourceRecordSet, error) {
	query := url.Values{"name": {fqdn}, "type": {"TXT"}, "maxitems": {"100"}}
	resp, err := p.do(http.MethodGet, apiPath+"/hostedzone/"+url.PathEscape(zoneID)+"/rrset", query, nil, nil)
	if err != nil {
		return nil, err
	}
	var payload listResourceRecordSetsResponse
	if err := httpapi.XML(resp.Body, &payload); err != nil {
		return nil, err
	}
	for _, item := range payload.ResourceRecordSets {
		if strings.EqualFold(dnsutil.Fqdn(item.Name), dnsutil.Fqdn(fqdn)) && strings.EqualFold(item.Type, "TXT") {
			return &item, nil
		}
	}
	return nil, nil
}

func (p *provider) change(zoneID, action string, recordSet resourceRecordSet) error {
	body, err := httpapi.EncodeXML(changeRequest{
		ChangeBatch: changeBatch{
			Comment: "Managed by LACTSSL",
			Changes: []change{{Action: action, ResourceRecordSet: recordSet}},
		},
	})
	if err != nil {
		return err
	}
	resp, err := p.do(http.MethodPost, apiPath+"/hostedzone/"+url.PathEscape(zoneID)+"/rrset/", nil, body, map[string]string{"Content-Type": "application/xml"})
	if err != nil {
		return err
	}
	if !p.waitForChange {
		return nil
	}
	var payload changeResponse
	if err := httpapi.XML(resp.Body, &payload); err != nil {
		return err
	}
	if payload.ChangeInfo.ID == "" {
		return nil
	}
	return p.waitChange(normalizeChangeID(payload.ChangeInfo.ID))
}

func (p *provider) waitChange(changeID string) error {
	deadline := time.Now().Add(p.changeTimeout)
	for {
		resp, err := p.do(http.MethodGet, apiPath+"/change/"+url.PathEscape(changeID), nil, nil, nil)
		if err != nil {
			return err
		}
		var payload changeResponse
		if err := httpapi.XML(resp.Body, &payload); err != nil {
			return err
		}
		if strings.EqualFold(payload.ChangeInfo.Status, "INSYNC") {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("route53: change %s is still %s", changeID, payload.ChangeInfo.Status)
		}
		time.Sleep(p.changeInterval)
	}
}

func (p *provider) do(method, path string, query url.Values, body []byte, headers map[string]string) (*httpapi.Response, error) {
	header := make(http.Header)
	for key, value := range headers {
		header.Set(key, value)
	}
	req, err := p.client.NewRequest(context.Background(), method, path, query, header, body)
	if err != nil {
		return nil, err
	}
	signer := sigv4.Signer{Region: "us-east-1", Service: "route53", Credentials: p.credentials, Now: p.now}
	if err := signer.Sign(req, body); err != nil {
		return nil, err
	}
	return p.client.Send(req)
}

func toResourceRecords(values []string) []resourceRecord {
	out := make([]resourceRecord, 0, len(values))
	for _, value := range values {
		out = append(out, resourceRecord{Value: value})
	}
	return out
}

func normalizeZoneID(id string) string {
	id = strings.TrimSpace(id)
	return strings.TrimPrefix(id, "/hostedzone/")
}

func normalizeChangeID(id string) string {
	id = strings.TrimSpace(id)
	return strings.TrimPrefix(id, "/change/")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
