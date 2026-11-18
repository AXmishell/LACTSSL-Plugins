// Package iij 是 IIJ DNS 的重构实现。
//
// 使用 IIJ DO API 的 HmacSHA256 签名与 JSON 接口。
package iij

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

const (
	defaultBaseURL = "https://do.api.iij.jp"
	apiVersion     = "20140601"
	defaultTTL     = 300
)

type plugin struct{}

func (plugin) ID() string { return "iij" }

func (plugin) Aliases() []string { return nil }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() {
	dns.Register(plugin{})
	dns.MarkVerifiable("iij")
}

type provider struct {
	client      *httpapi.Client
	accessKey   string
	secretKey   string
	serviceCode string
	ttl         int
	now         func() time.Time
}

func newProvider(cfg map[string]string) (*provider, error) {
	access := strings.TrimSpace(cfg["iij_api_access_key"])
	secret := strings.TrimSpace(cfg["iij_api_secret_key"])
	service := strings.TrimSpace(cfg["iij_do_service_code"])
	if access == "" || secret == "" || service == "" {
		return nil, errors.New("iij: iij_api_access_key, iij_api_secret_key and iij_do_service_code are required")
	}
	return &provider{
		client:      httpapi.New(defaultBaseURL),
		accessKey:   access,
		secretKey:   secret,
		serviceCode: service,
		ttl:         defaultTTL,
		now:         time.Now,
	}, nil
}

type zoneListResponse struct {
	RequestID     string   `json:"RequestId"`
	ZoneList      []string `json:"ZoneList"`
	ErrorResponse struct {
		ErrorType    string `json:"ErrorType"`
		ErrorMessage string `json:"ErrorMessage"`
	} `json:"ErrorResponse"`
}

type resourceRecord struct {
	ID         string `json:"Id"`
	Status     string `json:"Status"`
	Owner      string `json:"Owner"`
	TTL        string `json:"TTL"`
	RecordType string `json:"RecordType"`
	RData      string `json:"RData"`
}

type recordListResponse struct {
	RequestID     string           `json:"RequestId"`
	RecordList    []resourceRecord `json:"RecordList"`
	ErrorResponse struct {
		ErrorType    string `json:"ErrorType"`
		ErrorMessage string `json:"ErrorMessage"`
	} `json:"ErrorResponse"`
}

// Verify 只读检查凭据与 zone 归属，不写入任何 DNS 记录。
func (p *provider) Verify(_ context.Context, domain string) (string, error) {
	zone, _, err := p.splitDomain(domain)
	if err != nil {
		return "", err
	}
	return zone, nil
}

func (p *provider) Present(domain, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	zone, owner, err := p.splitDomain(domain)
	if err != nil {
		return fmt.Errorf("iij: %w", err)
	}
	body, _ := json.Marshal(map[string]string{
		"Owner":      owner,
		"TTL":        strconv.Itoa(p.ttl),
		"RecordType": "TXT",
		"RData":      txtValue,
	})
	if err := p.call(http.MethodPost, "/"+p.serviceCode+"/"+url.PathEscape(zone)+"/record.json", body); err != nil {
		return fmt.Errorf("iij: add record: %w", err)
	}
	return p.commit()
}

func (p *provider) CleanUp(domain, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	zone, owner, err := p.splitDomain(domain)
	if err != nil {
		return fmt.Errorf("iij: %w", err)
	}
	records, err := p.listRecords(zone)
	if err != nil {
		return fmt.Errorf("iij: %w", err)
	}
	for _, record := range records {
		if record.Owner == owner && strings.EqualFold(record.RecordType, "TXT") && dnsutil.UnquoteTXT(record.RData) == txtValue {
			path := "/" + p.serviceCode + "/" + url.PathEscape(zone) + "/record/" + url.PathEscape(record.ID) + ".json"
			if err := p.call(http.MethodDelete, path, nil); err != nil {
				return fmt.Errorf("iij: delete record: %w", err)
			}
			return p.commit()
		}
	}
	return nil
}

func (p *provider) splitDomain(domain string) (zone, owner string, err error) {
	zones, err := p.listZones()
	if err != nil {
		return "", "", err
	}
	zone, err = dnsutil.ZoneName(dnsutil.Fqdn(domain), zones)
	if err != nil {
		return "", "", err
	}
	relative, err := dnsutil.RelativeName(dnsutil.Fqdn(domain), zone)
	if err != nil {
		return "", "", err
	}
	relative = strings.TrimSuffix(relative, ".")
	owner = "_acme-challenge"
	if relative != "" && relative != "@" {
		owner += "." + relative
	}
	return dnsutil.UnFqdn(zone), owner, nil
}

func (p *provider) listZones() ([]string, error) {
	body, err := p.callResponse(http.MethodGet, "/"+p.serviceCode+"/zones.json", nil)
	if err != nil {
		return nil, err
	}
	var payload zoneListResponse
	if err := httpapi.JSON(body, &payload); err != nil {
		return nil, err
	}
	if payload.ErrorResponse.ErrorType != "" {
		return nil, fmt.Errorf("%s: %s", payload.ErrorResponse.ErrorType, payload.ErrorResponse.ErrorMessage)
	}
	return payload.ZoneList, nil
}

func (p *provider) listRecords(zone string) ([]resourceRecord, error) {
	body, err := p.callResponse(http.MethodGet, "/"+p.serviceCode+"/"+url.PathEscape(zone)+"/records/DETAIL.json", nil)
	if err != nil {
		return nil, err
	}
	var payload recordListResponse
	if err := httpapi.JSON(body, &payload); err != nil {
		return nil, err
	}
	if payload.ErrorResponse.ErrorType != "" {
		return nil, fmt.Errorf("%s: %s", payload.ErrorResponse.ErrorType, payload.ErrorResponse.ErrorMessage)
	}
	return payload.RecordList, nil
}

func (p *provider) commit() error {
	_, err := p.callResponse(http.MethodPut, "/"+p.serviceCode+"/commit.json", nil)
	return err
}

func (p *provider) call(method, path string, body []byte) error {
	_, err := p.callResponse(method, path, body)
	return err
}

func (p *provider) callResponse(method, path string, body []byte) ([]byte, error) {
	fullPath := "/r/" + apiVersion + path
	u, err := url.Parse(p.client.BaseURL + fullPath)
	if err != nil {
		return nil, err
	}
	req, err := p.client.NewRequest(context.Background(), method, u.String(), nil, nil, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	p.sign(req)
	resp, err := p.client.Send(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (p *provider) sign(req *http.Request) {
	now := time.Now
	if p.now != nil {
		now = p.now
	}
	req.Header.Set("x-iijapi-Expire", now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05Z"))
	req.Header.Set("x-iijapi-SignatureMethod", "HmacSHA256")
	req.Header.Set("x-iijapi-SignatureVersion", "2")

	target := stringToSign(req.Method, req.Header, req.URL)
	mac := hmac.New(sha256.New, []byte(p.secretKey))
	mac.Write([]byte(target))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	req.Header.Set("Authorization", "IIJAPI "+p.accessKey+":"+signature)
}

func stringToSign(method string, header http.Header, u *url.URL) string {
	var keys []string
	hasContentType := false
	for key := range header {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-iijapi-") {
			keys = append(keys, lower)
		} else if lower == "content-type" || lower == "content-md5" {
			keys = append(keys, lower)
			hasContentType = true
		}
	}
	sort.Strings(keys)
	target := []string{method, ""}
	if !hasContentType {
		target = append(target, "")
	}
	for _, key := range keys {
		if key == "content-type" || key == "content-md5" {
			target = append(target, header.Get(key))
		} else {
			target = append(target, key+":"+header.Get(key))
		}
	}
	target = append(target, u.Path)
	return strings.Join(target, "\n")
}
