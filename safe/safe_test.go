package safe

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestPublicDialContextRefusesInternalAddresses(t *testing.T) {
	t.Setenv(AllowPrivateHostsEnv, "")
	dial := PublicDialContext(&net.Dialer{})
	blocked := []string{
		"127.0.0.1:80",
		"[::1]:80",
		"169.254.169.254:80",
		"10.0.0.1:80",
		"192.168.1.1:80",
	}
	for _, addr := range blocked {
		conn, err := dial(context.Background(), "tcp", addr)
		if err == nil {
			conn.Close()
			t.Errorf("dial %s was allowed", addr)
			continue
		}
		if !strings.Contains(err.Error(), "non-public") {
			t.Errorf("dial %s error = %v, want a policy refusal", addr, err)
		}
	}
}

func TestPublicDialContextFailsClosedOnLookupErrors(t *testing.T) {
	t.Setenv(AllowPrivateHostsEnv, "")
	dial := PublicDialContext(&net.Dialer{})
	_, err := dial(context.Background(), "tcp", "host.invalid:80")
	if err == nil {
		t.Error("unresolvable host was dialled")
	}
	if strings.Contains(err.Error(), "non-public") {
		t.Errorf("resolver failure reported as a policy refusal: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dial(ctx, "tcp", "example.org:80"); err == nil {
		t.Error("cancelled context should abort the dial")
	}
}

func TestPublicDialContextEscapeHatch(t *testing.T) {
	t.Setenv(AllowPrivateHostsEnv, "1")
	dial := PublicDialContext(&net.Dialer{})
	_, err := dial(context.Background(), "tcp", "127.0.0.1:1")
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "non-public") {
		t.Errorf("escape hatch did not bypass the policy: %v", err)
	}
}

func TestProxyHostsParsing(t *testing.T) {
	for _, key := range proxyEnvKeys {
		t.Setenv(key, "")
	}
	t.Setenv("HTTPS_PROXY", "http://user:secret@10.0.0.5:8080")
	t.Setenv("HTTP_PROXY", "proxy.corp:3128")
	t.Setenv("ALL_PROXY", "http://10.0.0.5:8080")
	hosts := ProxyHosts()
	if len(hosts) != 2 {
		t.Fatalf("ProxyHosts = %v, want two unique hosts", hosts)
	}
	for _, want := range []string{"10.0.0.5:8080", "proxy.corp:3128"} {
		found := false
		for _, got := range hosts {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("ProxyHosts = %v, missing %q", hosts, want)
		}
	}
	for _, got := range hosts {
		if strings.Contains(got, "secret") {
			t.Errorf("ProxyHosts leaked credentials: %v", hosts)
		}
	}
}

func TestPublicDialContextAllowsConfiguredProxy(t *testing.T) {
	t.Setenv(AllowPrivateHostsEnv, "")
	for _, key := range proxyEnvKeys {
		t.Setenv(key, "")
	}
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	dial := PublicDialContext(&net.Dialer{})
	_, err := dial(context.Background(), "tcp", "127.0.0.1:9")
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "non-public") {
		t.Errorf("loopback proxy was refused by the policy: %v", err)
	}
	if got := ProxyHosts(); len(got) != 1 || got[0] != "127.0.0.1:9" {
		t.Errorf("ProxyHosts = %v, want [127.0.0.1:9]", got)
	}
}

func TestPublicDialContextStillBlocksNonProxyAddrs(t *testing.T) {
	t.Setenv(AllowPrivateHostsEnv, "")
	for _, key := range proxyEnvKeys {
		t.Setenv(key, "")
	}
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	dial := PublicDialContext(&net.Dialer{})
	for _, addr := range []string{"127.0.0.1:8080", "10.0.0.1:80", "169.254.169.254:80"} {
		if _, err := dial(context.Background(), "tcp", addr); err == nil {
			t.Errorf("dial %s was allowed", addr)
		} else if !strings.Contains(err.Error(), "non-public") {
			t.Errorf("dial %s error = %v, want a policy refusal", addr, err)
		}
	}
}

func TestTextStripsTerminalEscapes(t *testing.T) {
	cases := []string{
		"\x1b]52;c;aGFja2Vk\x07clipboard",
		"\x1b[2Jclear",
		"\x1b_Ga=T,f=100;kitty",
		"a\x00b\x7fc",
		"\u009bC1\xc2\x85",
		"line\nbreak\ttab",
	}
	for _, raw := range cases {
		got := Text(raw)
		for _, r := range got {
			if r == 0x1b || r == 0x07 || r == 0 || r == 0x7f || r == 0x9b {
				t.Fatalf("Text(%q) = %q, leaked control rune %U", raw, got, r)
			}
		}
	}
}

func TestTextKeepsPrintableContent(t *testing.T) {
	if got := Text("  big tits  "); got != "big tits" {
		t.Errorf("Text = %q, want trimmed content", got)
	}
	if got := Text("café_日本語-1"); got != "café_日本語-1" {
		t.Errorf("Text = %q, want unicode preserved", got)
	}
	if got := Text("line\nbreak"); got != "line break" {
		t.Errorf("Text = %q, want newline folded to space", got)
	}
}

func TestTextRedactsCredentialParams(t *testing.T) {
	raw := `request failed: Get "https://api.rule34.xxx/index.php?api_key=SUPERSECRET&json=1&user_id=777": dial tcp: timeout`
	got := Text(raw)
	for _, secret := range []string{"SUPERSECRET", "777"} {
		if strings.Contains(got, secret) {
			t.Errorf("Text leaked %q: %q", secret, got)
		}
	}
	if !strings.Contains(got, "api.rule34.xxx") || !strings.Contains(got, "dial tcp") {
		t.Errorf("Text dropped useful context: %q", got)
	}
	if !strings.Contains(got, "api_key=***") {
		t.Errorf("Text = %q, want the parameter name kept with a masked value", got)
	}
}

func TestURLTextKeepsTokens(t *testing.T) {
	raw := "https://hls-cdn77.xvideos-cdn.com/bQy0VnVce7mR8KNVJLHmfA==,1789855198/x.m3u8?token=abc123&validfrom=1789851198"
	if got := URLText(raw); got != raw {
		t.Errorf("URLText rewrote a media url: %q", got)
	}
	if got := Limit(raw, MaxURLRunes); got != raw {
		t.Errorf("Limit rewrote a media url: %q", got)
	}
}

func TestTextRespectsLimit(t *testing.T) {
	if got := Limit("abcdef", 3); got != "abc" {
		t.Errorf("Limit = %q, want abc", got)
	}
	if got := Limit("abc", 0); got != "" {
		t.Errorf("Limit with max 0 = %q, want empty", got)
	}
}

func TestMaskPreservesRuneCount(t *testing.T) {
	raw := "\x1b]52;c;x\x07abc"
	got := Mask(raw)
	if len([]rune(got)) != len([]rune(raw)) {
		t.Fatalf("Mask(%q) = %q, rune count changed", raw, got)
	}
	for _, r := range got {
		if r == 0x1b || r == 0x07 {
			t.Fatalf("Mask(%q) = %q, leaked %U", raw, got, r)
		}
	}
}

func TestTextKeepsZeroWidthJoiners(t *testing.T) {
	if got := Text("a\u200db"); got != "a\u200db" {
		t.Errorf("Text dropped the zero-width joiner: %q", got)
	}
	if got := Text("a\ufeffb"); got != "ab" {
		t.Errorf("Text kept the BOM: %q", got)
	}
	if got := Text("a\u200bb"); got != "ab" {
		t.Errorf("Text kept the zero-width space: %q", got)
	}
}

func TestPublicMediaURLBlocksInternalTargets(t *testing.T) {
	t.Setenv(AllowPrivateHostsEnv, "")
	blocked := []string{
		"http://127.0.0.1:8080/x.mp4",
		"http://[::1]:8080/x.mp4",
		"http://10.0.0.5/x.mp4",
		"http://172.16.4.4/x.mp4",
		"http://192.168.1.1/x.mp4",
		"http://169.254.169.254/latest/meta-data/",
		"http://100.64.0.1/x.mp4",
		"http://0.0.0.0/x.mp4",
		"http://240.0.0.1/x.mp4",
		"http://198.18.0.1/x.mp4",
		"http://[fe80::1]/x.mp4",
		"http://[fd00::1]/x.mp4",
		"file:///etc/passwd",
		"concat:/etc/passwd",
	}
	for _, raw := range blocked {
		if PublicMediaURL(raw) {
			t.Errorf("PublicMediaURL(%q) = true, want false", raw)
		}
	}
	allowed := []string{
		"https://1.1.1.1/x.mp4",
		"https://8.8.8.8/x.mp4",
		"https://example.org/x.mp4",
		"https://video-nss.xhcdn.com/x.m3u8",
	}
	for _, raw := range allowed {
		if !PublicMediaURL(raw) {
			t.Errorf("PublicMediaURL(%q) = false, want true", raw)
		}
	}
}

func TestPublicMediaURLHonoursEscapeHatch(t *testing.T) {
	t.Setenv(AllowPrivateHostsEnv, "1")
	if !PublicMediaURL("http://127.0.0.1:8080/x.mp4") {
		t.Error("escape hatch should allow loopback media urls")
	}
	t.Setenv(AllowPrivateHostsEnv, "0")
	if PublicMediaURL("http://127.0.0.1:8080/x.mp4") {
		t.Error("loopback should be blocked once the hatch is off")
	}
	if PublicMediaURL("concat:/etc/passwd") {
		t.Error("escape hatch must not allow non-http schemes")
	}
}

func TestMediaURL(t *testing.T) {
	allowed := []string{
		"https://cdn.example.org/v/1.mp4",
		"http://example.org/a/b.jpg?x=1#frag",
	}
	for _, raw := range allowed {
		if !MediaURL(raw) {
			t.Errorf("MediaURL(%q) = false, want true", raw)
		}
	}
	blocked := []string{
		"",
		"file:///etc/passwd",
		"concat:/etc/passwd|file:///etc/passwd",
		"subfile,,start,0,end,0,,:/etc/passwd",
		"data:text/plain;base64,aGk=",
		"gopher://example.org/1",
		"https://user:pass@example.org/x.mp4",
		"https://",
		"/etc/passwd",
		"clip.mp4",
	}
	for _, raw := range blocked {
		if MediaURL(raw) {
			t.Errorf("MediaURL(%q) = true, want false", raw)
		}
	}
}

func TestHostIn(t *testing.T) {
	allowed := []string{"https://ev-h.phncdn.com/v/1.mp4", "https://www.pornhub.com/x", "https://pornhub.com"}
	for _, raw := range allowed {
		if !HostIn(Host(raw), []string{"pornhub.com", "phncdn.com"}) {
			t.Errorf("Host(%q) should match the allow list", raw)
		}
	}
	blocked := []string{"https://evil.example/x", "https://pornhub.com.evil.example/x", "https://notphncdn.com/x"}
	for _, raw := range blocked {
		if HostIn(Host(raw), []string{"pornhub.com", "phncdn.com"}) {
			t.Errorf("Host(%q) must not match the allow list", raw)
		}
	}
}
