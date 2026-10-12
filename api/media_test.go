package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"moxiu/r34-dl/safe"
)

type mediaTransport func(*http.Request) (*http.Response, error)

func (fn mediaTransport) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func mediaResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}
}

func TestStreamHeadersRejectInjectionAndInsecureTransport(t *testing.T) {
	stream := Stream{Cookie: "session=abc\r\nX-Injected: yes", Referer: "https://www.pornhub.com/\r\nX-Injected: yes"}
	if got := stream.Headers("pornhub", "https://ev-h.phncdn.com/video.mp4"); len(got) != 0 {
		t.Fatalf("injected headers were accepted: %v", got)
	}
	stream = Stream{Cookie: "session=abc", Referer: "https://www.pornhub.com/video"}
	for _, target := range []string{"http://ev-h.phncdn.com/video.mp4", "https://evil.example/video.mp4"} {
		if got := stream.Headers("pornhub", target); len(got) != 0 {
			t.Errorf("credentials were forwarded to %q: %v", target, got)
		}
	}
}

func TestMediaRelayRewritesPlaylistReferences(t *testing.T) {
	t.Setenv(safe.AllowPrivateHostsEnv, "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/start.m3u8":
			http.Redirect(w, req, "/nested/master.m3u8", http.StatusFound)
		case "/nested/master.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"../key\"\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:1,\nsegment.ts\n")
		default:
			io.WriteString(w, req.URL.Path)
		}
	}))
	defer server.Close()
	relay, err := NewMediaRelay(Stream{URL: server.URL + "/start.m3u8"}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	resp, err := http.Get(relay.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), server.URL) {
		t.Fatalf("playlist bypasses relay: %s", body)
	}
	var targets []string
	for _, match := range playlistURI.FindAllStringSubmatch(string(body), -1) {
		targets = append(targets, match[1])
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, relay.Proxy) {
			targets = append(targets, line)
		}
	}
	if len(targets) != 3 {
		t.Fatalf("rewrote %d references, want 3: %s", len(targets), body)
	}
	for i, target := range targets {
		resp, err := http.Get(target)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		want := []string{"/key", "/nested/init.mp4", "/nested/segment.ts"}[i]
		if string(data) != want {
			t.Errorf("reference %d resolved to %q, want %q", i, data, want)
		}
	}
	base, _ := url.Parse(server.URL + "/master.m3u8")
	for _, target := range []string{"file:///etc/passwd", "crypto:file:///etc/passwd", "tcp://127.0.0.1:22"} {
		if _, err := relay.rewritePlaylist("#EXTM3U\n"+target+"\n", base); err == nil {
			t.Errorf("accepted %q", target)
		}
	}
}

func TestMediaRelayWithholdsCredentialsOnRedirect(t *testing.T) {
	t.Setenv(safe.AllowPrivateHostsEnv, "1")
	relay, err := NewMediaRelay(Stream{URL: "https://ev-h.phncdn.com/video.mp4", Cookie: "session=secret", Referer: "https://www.pornhub.com/video"}, "pornhub")
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	hits := 0
	relay.client.Transport = mediaTransport(func(req *http.Request) (*http.Response, error) {
		hits++
		if hits == 1 {
			if req.Header.Get("Cookie") == "" {
				t.Error("approved media host lost its cookie")
			}
			resp := mediaResponse(req, http.StatusFound, "")
			resp.Header.Set("Location", "https://evil.example/video.mp4")
			return resp, nil
		}
		if req.Header.Get("Cookie") != "" || req.Header.Get("Referer") != "" {
			t.Error("credentials leaked on redirect")
		}
		return mediaResponse(req, http.StatusOK, "video"), nil
	})
	resp, err := http.Get(relay.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if hits != 2 || resp.StatusCode != http.StatusOK {
		t.Fatalf("hits=%d status=%d", hits, resp.StatusCode)
	}
}

func TestMediaRelayBlocksPrivateRedirectAndPlaylistSegment(t *testing.T) {
	t.Setenv(safe.AllowPrivateHostsEnv, "")
	var privateHits atomic.Int32
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { privateHits.Add(1) }))
	defer private.Close()
	relay, err := NewMediaRelay(Stream{URL: "https://1.1.1.1/master.m3u8"}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	relay.client.Transport = mediaTransport(func(req *http.Request) (*http.Response, error) {
		resp := mediaResponse(req, http.StatusFound, "")
		resp.Header.Set("Location", private.URL+"/secret")
		return resp, nil
	})
	resp, err := http.Get(relay.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("private redirect: status=%d", resp.StatusCode)
	}
	relay.client.Transport = mediaTransport(func(req *http.Request) (*http.Response, error) {
		return mediaResponse(req, http.StatusOK, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\n"+private.URL+"/secret.ts\n#EXT-X-ENDLIST\n"), nil
	})
	if _, err := exec.LookPath("ffprobe"); err == nil {
		t.Setenv("no_proxy", "*")
		t.Setenv("NO_PROXY", "*")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", MediaProtocols, "-http_proxy", relay.Proxy, relay.URL)
		cmd.Env = MediaEnv()
		if out, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("private playlist played: %s", out)
		}
		cmd = exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", MediaProtocols, "-http_proxy", relay.Proxy, private.URL+"/secret.ts")
		cmd.Env = MediaEnv()
		if out, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("external request bypassed media relay: %s", out)
		}
	} else {
		t.Log("ffprobe unavailable; checking playlist destination directly")
	}
	target := relay.mediaURL(private.URL + "/secret.ts")
	resp, err = http.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || privateHits.Load() != 0 {
		t.Fatalf("private segment: status=%d hits=%d", resp.StatusCode, privateHits.Load())
	}
	resp, err = http.Get(relay.Proxy + "/unknown")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatal("unauthenticated relay request accepted")
	}
}

func TestMediaRelayVerifiesTLSCertificates(t *testing.T) {
	t.Setenv(safe.AllowPrivateHostsEnv, "1")
	var hits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits.Add(1)
	}))
	defer server.Close()
	relay, err := NewMediaRelay(Stream{URL: server.URL + "/video.mp4"}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	resp, err := http.Get(relay.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || hits.Load() != 0 {
		t.Fatalf("untrusted TLS certificate accepted: status=%d hits=%d", resp.StatusCode, hits.Load())
	}
}
