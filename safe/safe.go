package safe

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const (
	MaxTextRunes = 300
	MaxTagRunes  = 96
	MaxURLRunes  = 2048
	MaxListRunes = 1200

	dialPerAddressTimeout = 15 * time.Second

	AllowPrivateHostsEnv = "R34_DL_ALLOW_PRIVATE_HOSTS"
	SkipUpdateCheckEnv   = "R34_DL_NO_UPDATE_CHECK"
)

var proxyEnvKeys = []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"}

var secretQueryRe = regexp.MustCompile(`(?i)\b(api_key|user_id|token|password|secret|key)=[^&\s"']+`)

func Text(s string) string {
	return redactSecrets(Limit(s, MaxTextRunes))
}

func redactSecrets(s string) string {
	if !strings.Contains(s, "=") {
		return s
	}
	return secretQueryRe.ReplaceAllString(s, "$1=***")
}

func Tag(s string) string {
	return Limit(s, MaxTagRunes)
}

func URLText(s string) string {
	return Limit(s, MaxURLRunes)
}

func List(s string) string {
	return Limit(s, MaxListRunes)
}

func Limit(s string, max int) string {
	if s == "" || max <= 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	seen := 0
	for _, r := range s {
		if seen >= max {
			break
		}
		seen++
		if printable(r) {
			b.WriteRune(r)
			continue
		}
		switch r {
		case '\n', '\r', '\t':
			b.WriteRune(' ')
		}
	}
	return strings.TrimSpace(b.String())
}

func Mask(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if printable(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteRune(' ')
	}
	return b.String()
}

func printable(r rune) bool {
	switch {
	case r == 0:
		return false
	case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		return false
	case r == 0x200c, r == 0x200d:
		return true
	case unicode.Is(unicode.Cf, r), unicode.Is(unicode.Cs, r):
		return false
	}
	return true
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func AllowPrivateHosts() bool {
	return truthy(os.Getenv(AllowPrivateHostsEnv))
}

func SkipUpdateCheck() bool {
	return truthy(os.Getenv(SkipUpdateCheckEnv))
}

func ProxyHosts() []string {
	seen := make(map[string]bool, len(proxyEnvKeys))
	hosts := make([]string, 0, len(proxyEnvKeys))
	for _, key := range proxyEnvKeys {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		if !strings.Contains(raw, "://") {
			raw = "http://" + raw
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			continue
		}
		host := strings.ToLower(parsed.Host)
		if seen[host] {
			continue
		}
		seen[host] = true
		hosts = append(hosts, host)
	}
	return hosts
}

func isProxyAddress(addr string, proxies []string) bool {
	if len(proxies) == 0 {
		return false
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	host = strings.ToLower(host)
	for _, proxy := range proxies {
		proxyHost, proxyPort, err := net.SplitHostPort(proxy)
		if err != nil {
			proxyHost, proxyPort = proxy, ""
		}
		if strings.ToLower(proxyHost) != host {
			continue
		}
		if proxyPort == "" || proxyPort == port {
			return true
		}
	}
	return false
}

func MediaURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return false
	}
	return parsed.Host != "" && parsed.User == nil
}

func PublicMediaURL(raw string) bool {
	if !MediaURL(raw) {
		return false
	}
	if AllowPrivateHosts() {
		return true
	}
	host := Host(raw)
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return publicIP(ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return true
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return false
		}
	}
	return true
}

func publicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	v4 := ip.To4()
	if v4 == nil {
		return true
	}
	switch {
	case v4[0] == 0, v4[0] >= 240:
		return false
	case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127:
		return false
	case v4[0] == 192 && v4[1] == 0 && v4[2] == 0:
		return false
	case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19):
		return false
	}
	return true
}

func PublicDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	if dialer == nil {
		dialer = &net.Dialer{Timeout: dialPerAddressTimeout}
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if AllowPrivateHosts() {
			return dialer.DialContext(ctx, network, addr)
		}
		if isProxyAddress(addr, ProxyHosts()) {
			return dialer.DialContext(ctx, network, addr)
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if ip := net.ParseIP(host); ip != nil {
			if !publicIP(ip) {
				return nil, fmt.Errorf("refusing to connect to non-public address %s", host)
			}
			return dialer.DialContext(ctx, network, addr)
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve %s: %w", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("cannot resolve %s: no addresses returned", host)
		}
		var lastErr error
		for _, ip := range ips {
			if !publicIP(ip) {
				lastErr = fmt.Errorf("refusing to connect to non-public address %s (%s)", host, ip)
				continue
			}
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

func Host(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.Trim(parsed.Hostname(), "."))
}

func HostIn(host string, suffixes []string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "."), "."))
	if host == "" {
		return false
	}
	for _, suffix := range suffixes {
		suffix = strings.ToLower(strings.Trim(suffix, "."))
		if suffix == "" {
			continue
		}
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
