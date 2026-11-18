// Package dnsutil 提供 DNS-01 provider 共用的域名与 TXT 处理函数。
package dnsutil

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
)

// Fqdn 保证域名以点结尾。
func Fqdn(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}

// UnFqdn 去掉结尾的点。
func UnFqdn(name string) string {
	return strings.TrimSuffix(strings.TrimSpace(name), ".")
}

// ChallengeFQDN 返回 _acme-challenge.<domain>.。
func ChallengeFQDN(domain string) string {
	domain = UnFqdn(domain)
	if strings.HasPrefix(domain, "_acme-challenge.") {
		return Fqdn(domain)
	}
	return Fqdn("_acme-challenge." + domain)
}

// ZoneName 从候选 zone 列表中选出最长的后缀匹配。
// 传入的 zone 可以带或不带结尾点。
func ZoneName(fqdn string, zones []string) (string, error) {
	name := strings.ToLower(Fqdn(fqdn))
	best := ""
	for _, zone := range zones {
		zone = strings.ToLower(Fqdn(zone))
		if zone == "." || zone == "" {
			continue
		}
		if name == zone || strings.HasSuffix(name, "."+zone) {
			if len(zone) > len(best) {
				best = zone
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("no matching DNS zone for %s", fqdn)
	}
	return best, nil
}

// RelativeName 返回 fqdn 相对于 zone 的记录名。
// 例如 _acme-challenge.example.com. 在 example.com. 下返回 _acme-challenge。
func RelativeName(fqdn, zone string) (string, error) {
	name := strings.ToLower(Fqdn(fqdn))
	zone = strings.ToLower(Fqdn(zone))
	if name == zone {
		return "@", nil
	}
	suffix := "." + zone
	if !strings.HasSuffix(name, suffix) {
		return "", fmt.Errorf("%s is not inside zone %s", fqdn, zone)
	}
	return strings.TrimSuffix(name, suffix), nil
}

// TXTValue 返回 DNS-01 挑战的 TXT 记录值：base64url(sha256(keyAuth))，无 padding。
// keyAuth 是 ACME 的 key authorization（token.thumbprint），不能直接写入 DNS。
func TXTValue(keyAuth string) string {
	sum := sha256.Sum256([]byte(keyAuth))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// QuoteTXT 返回带双引号的 TXT 值。
func QuoteTXT(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		return value
	}
	return `"` + value + `"`
}

// UnquoteTXT 去掉 TXT 值两侧的引号。
func UnquoteTXT(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		return value[1 : len(value)-1]
	}
	return value
}

// ContainsTXT 判断 TXT 值列表是否包含目标值（忽略引号与大小写）。
func ContainsTXT(values []string, target string) bool {
	target = UnquoteTXT(target)
	for _, value := range values {
		if strings.EqualFold(UnquoteTXT(value), target) {
			return true
		}
	}
	return false
}

// RemoveTXT 返回删除目标值后的列表。
func RemoveTXT(values []string, target string) []string {
	target = UnquoteTXT(target)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.EqualFold(UnquoteTXT(value), target) {
			continue
		}
		out = append(out, value)
	}
	return out
}

// SortStrings 返回排序后的副本。
func SortStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
