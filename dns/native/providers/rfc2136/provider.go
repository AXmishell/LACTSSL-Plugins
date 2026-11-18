// Package rfc2136 是 RFC 2136 Dynamic DNS Update 的重构实现。
//
// 使用 miekg/dns 处理 DNS UPDATE 报文与 TSIG；GSS-TSIG 仍由原 lego
// 实现提供，native 变体会明确报错，避免静默失败。
package rfc2136

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/dnsutil"
	mdns "github.com/miekg/dns"
)

const defaultTTL = 60

type plugin struct{}

func (plugin) ID() string { return "rfc2136" }

func (plugin) Aliases() []string { return []string{"dnsupdate"} }

func (plugin) New(cfg map[string]string) (dns.ChallengeProvider, error) {
	return newProvider(cfg)
}

func init() {
	dns.Register(plugin{})
	dns.MarkVerifiable("rfc2136")
}

type provider struct {
	nameserver string
	zones      []string
	tsigKey    string
	tsigSecret string
	tsigAlg    string
	ttl        int
	timeout    time.Duration
}

func newProvider(cfg map[string]string) (*provider, error) {
	nameserver := strings.TrimSpace(cfg["nameserver"])
	if nameserver == "" {
		return nil, errors.New("rfc2136: nameserver is required")
	}
	if _, _, err := net.SplitHostPort(nameserver); err != nil {
		if strings.Contains(err.Error(), "missing port") {
			nameserver = net.JoinHostPort(nameserver, "53")
		} else {
			return nil, fmt.Errorf("rfc2136: %w", err)
		}
	}
	alg := strings.TrimSpace(cfg["tsig_algorithm"])
	if alg == "" {
		alg = mdns.HmacMD5
	}
	if strings.EqualFold(dnsutil.UnFqdn(alg), "gss") || strings.EqualFold(alg, "gss-tsig") {
		return nil, errors.New("rfc2136: GSS-TSIG is not supported by the native provider; use the original lego bundle for GSS-TSIG")
	}
	if !supportedAlgorithm(alg) {
		return nil, fmt.Errorf("rfc2136: unsupported TSIG algorithm %q", alg)
	}
	zones := splitList(cfg["zones"])
	return &provider{
		nameserver: nameserver,
		zones:      zones,
		tsigKey:    strings.TrimSpace(cfg["tsig_key"]),
		tsigSecret: strings.TrimSpace(cfg["tsig_secret"]),
		tsigAlg:    mdns.Fqdn(alg),
		ttl:        defaultTTL,
		timeout:    30 * time.Second,
	}, nil
}

// Verify 只读检查凭据与 zone 归属，不写入任何 DNS 记录。
func (p *provider) Verify(_ context.Context, domain string) (string, error) {
	fqdn := dnsutil.ChallengeFQDN(domain)
	z, err := p.resolveZone(fqdn)
	if err != nil {
		return "", err
	}
	return z, nil
}

func (p *provider) Present(domain, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	fqdn := dnsutil.ChallengeFQDN(domain)
	zone, err := p.resolveZone(fqdn)
	if err != nil {
		return fmt.Errorf("rfc2136: %w", err)
	}
	msg, err := p.updateMessage(zone, fqdn, txtValue, true)
	if err != nil {
		return fmt.Errorf("rfc2136: %w", err)
	}
	return p.send(msg)
}

func (p *provider) CleanUp(domain, _ string, keyAuth string) error {
	txtValue := dnsutil.TXTValue(keyAuth)
	fqdn := dnsutil.ChallengeFQDN(domain)
	zone, err := p.resolveZone(fqdn)
	if err != nil {
		return fmt.Errorf("rfc2136: %w", err)
	}
	msg, err := p.updateMessage(zone, fqdn, txtValue, false)
	if err != nil {
		return fmt.Errorf("rfc2136: %w", err)
	}
	return p.send(msg)
}

func (p *provider) updateMessage(zone, fqdn, value string, insert bool) (*mdns.Msg, error) {
	msg := new(mdns.Msg)
	msg.SetUpdate(mdns.Fqdn(zone))
	rr := &mdns.TXT{
		Hdr: mdns.RR_Header{
			Name:   mdns.Fqdn(fqdn),
			Rrtype: mdns.TypeTXT,
			Class:  mdns.ClassINET,
			Ttl:    uint32(p.ttl),
		},
		Txt: []string{value},
	}
	if insert {
		// 与原 lego 实现一致：先删除同名 TXT 记录集，避免遗留旧 challenge 值。
		msg.RemoveRRset([]mdns.RR{rr})
		msg.Insert([]mdns.RR{rr})
	} else {
		msg.Remove([]mdns.RR{rr})
	}
	if p.tsigKey != "" {
		if p.tsigSecret == "" {
			return nil, errors.New("tsig_key is set but tsig_secret is empty")
		}
		msg.SetTsig(mdns.Fqdn(p.tsigKey), p.tsigAlg, 300, time.Now().Unix())
	}
	return msg, nil
}

func (p *provider) send(msg *mdns.Msg) error {
	client := &mdns.Client{Timeout: p.timeout}
	if p.tsigKey != "" {
		if p.tsigSecret == "" {
			return errors.New("tsig_key is set but tsig_secret is empty")
		}
		client.TsigSecret = map[string]string{mdns.Fqdn(p.tsigKey): p.tsigSecret}
	}
	resp, _, err := client.Exchange(msg, p.nameserver)
	if err != nil {
		return err
	}
	if resp == nil {
		return errors.New("empty DNS response")
	}
	if resp.Rcode != mdns.RcodeSuccess {
		return fmt.Errorf("DNS update failed: %s", mdns.RcodeToString[resp.Rcode])
	}
	return nil
}

func (p *provider) resolveZone(fqdn string) (string, error) {
	if len(p.zones) > 0 {
		return dnsutil.ZoneName(fqdn, p.zones)
	}
	msg := new(mdns.Msg)
	msg.SetQuestion(mdns.Fqdn(fqdn), mdns.TypeSOA)
	client := &mdns.Client{Timeout: p.timeout}
	resp, _, err := client.Exchange(msg, p.nameserver)
	if err != nil {
		return "", fmt.Errorf("SOA lookup: %w", err)
	}
	if resp == nil || resp.Rcode != mdns.RcodeSuccess {
		return "", errors.New("SOA lookup failed")
	}
	best := ""
	for _, answer := range resp.Answer {
		if soa, ok := answer.(*mdns.SOA); ok {
			name := dnsutil.Fqdn(soa.Hdr.Name)
			if strings.HasSuffix(dnsutil.Fqdn(fqdn), "."+name) || dnsutil.Fqdn(fqdn) == name {
				if len(name) > len(best) {
					best = name
				}
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("no SOA found for %s", fqdn)
	}
	return best, nil
}

func supportedAlgorithm(alg string) bool {
	switch mdns.Fqdn(alg) {
	case mdns.HmacMD5, mdns.HmacSHA1, mdns.HmacSHA224, mdns.HmacSHA256, mdns.HmacSHA384, mdns.HmacSHA512:
		return true
	default:
		return false
	}
}

func splitList(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}
