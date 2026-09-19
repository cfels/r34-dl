package dl

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/safe"
)

type captureTransport struct {
	last *http.Request
	body string
}

type redirectTransport struct {
	location string
	hits     int
}

type stallingTransport struct{}

func (s *stallingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	reader, _ := io.Pipe()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       reader,
		Header:     http.Header{},
		Request:    req,
	}, nil
}

func (r *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.hits++
	return &http.Response{
		StatusCode: http.StatusFound,
		Status:     "302 Found",
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     http.Header{"Location": []string{r.location}},
		Request:    req,
	}, nil
}

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.last = req
	body := c.body
	if body == "" {
		body = "media"
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

func TestDownloadFileWithholdsCookieOffSite(t *testing.T) {
	transport := &captureTransport{}
	d := New(t.TempDir(), 1)
	d.http = &http.Client{Transport: transport}
	post := api.Post{ID: 1, Site: "pornhub", Video: true}
	stream := api.Stream{
		URL:     "https://evil.example/v/1.mp4",
		Cookie:  "session=abc",
		Referer: "https://www.pornhub.com/view_video.php?viewkey=1",
	}
	if res := d.downloadFile(post, stream, filepath.Join(t.TempDir(), "1.mp4")); res.Err != nil {
		t.Fatalf("downloadFile: %v", res.Err)
	}
	if got := transport.last.Header.Get("Cookie"); got != "" {
		t.Errorf("off-site stream sent cookie %q", got)
	}
	if got := transport.last.Header.Get("Referer"); got != "" {
		t.Errorf("off-site stream sent referer %q", got)
	}
}

func TestDownloadFileKeepsCookieOnSite(t *testing.T) {
	transport := &captureTransport{}
	d := New(t.TempDir(), 1)
	d.http = &http.Client{Transport: transport}
	post := api.Post{ID: 1, Site: "pornhub", Video: true}
	stream := api.Stream{
		URL:     "https://ev-h.phncdn.com/v/1.mp4",
		Cookie:  "session=abc",
		Referer: "https://www.pornhub.com/view_video.php?viewkey=1",
	}
	if res := d.downloadFile(post, stream, filepath.Join(t.TempDir(), "1.mp4")); res.Err != nil {
		t.Fatalf("downloadFile: %v", res.Err)
	}
	if got := transport.last.Header.Get("Cookie"); got != "session=abc" {
		t.Errorf("same-site stream sent cookie %q, want session=abc", got)
	}
	if got := transport.last.Header.Get("Referer"); got == "" {
		t.Error("same-site stream should forward the referer")
	}
}

func TestDownloadOneRejectsPrivateHosts(t *testing.T) {
	if safe.AllowPrivateHosts() {
		t.Skipf("%s is set, private hosts are allowed in this environment", safe.AllowPrivateHostsEnv)
	}
	for _, raw := range []string{
		"http://127.0.0.1:8080/x.mp4",
		"http://169.254.169.254/x.mp4",
		"http://[::1]/x.mp4",
		"http://10.1.2.3/x.mp4",
	} {
		post := api.Post{ID: 9, Site: "rule34", Image: "9.mp4", FileURL_: raw}
		if res := New(t.TempDir(), 1).downloadOne(post); res.Err == nil {
			t.Errorf("downloadOne(%q) reached a non-public host", raw)
		} else if res.Path != "" {
			t.Errorf("downloadOne(%q) wrote %q", raw, res.Path)
		}
	}
}

func TestDownloadFileEnforcesSizeLimit(t *testing.T) {
	t.Setenv(maxDownloadMBEnv, "1")
	transport := &captureTransport{body: strings.Repeat("a", 2<<20)}
	d := New(t.TempDir(), 1)
	d.http = &http.Client{Transport: transport}
	post := api.Post{ID: 3, Site: "rule34", Video: true}
	stream := api.Stream{URL: "https://cdn.example.org/v/3.mp4"}
	outPath := filepath.Join(t.TempDir(), "3_3.mp4")
	res := d.downloadFile(post, stream, outPath)
	if res.Err == nil {
		t.Fatal("oversized download was accepted")
	}
	if !strings.Contains(res.Err.Error(), "limit") {
		t.Errorf("error = %v, want a size limit message", res.Err)
	}
	if _, err := os.Stat(outPath); err == nil {
		t.Error("partial file should not be published")
	}
	entries, err := os.ReadDir(filepath.Dir(outPath))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestPartNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		name := partName("/tmp/out/1_1.mp4", true)
		if seen[name] {
			t.Fatalf("partName repeated %q", name)
		}
		seen[name] = true
		if !strings.HasSuffix(name, ".mp4") {
			t.Errorf("partName %q dropped the extension ffmpeg needs", name)
		}
	}
}

func TestPartNameSurvivesRandomFailure(t *testing.T) {
	restore := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	defer func() { randRead = restore }()

	first := partName("/tmp/out/1_1.mp4", true)
	second := partName("/tmp/out/1_1.mp4", true)
	if first == "" || !strings.HasSuffix(first, ".mp4") {
		t.Errorf("partName = %q, want a usable name", first)
	}
	if !strings.Contains(first, ".part-") {
		t.Errorf("partName = %q, want the part suffix", first)
	}
	if first != second && !strings.HasSuffix(second, ".mp4") {
		t.Errorf("second partName = %q, want a usable name", second)
	}
}

func TestDownloadRefusesPrivateRedirectTargets(t *testing.T) {
	if safe.AllowPrivateHosts() {
		t.Skipf("%s is set, private hosts are allowed in this environment", safe.AllowPrivateHostsEnv)
	}
	transport := &redirectTransport{location: "http://127.0.0.1:9/x.mp4"}
	d := New(t.TempDir(), 1)
	client := newHTTPClient()
	client.Transport = transport
	d.http = client
	post := api.Post{ID: 5, Site: "rule34", Video: true}
	res := d.downloadFile(post, api.Stream{URL: "https://cdn.example.org/v/5.mp4"}, filepath.Join(t.TempDir(), "5.mp4"))
	if res.Err == nil {
		t.Fatal("redirect to a loopback address was followed")
	}
	if transport.hits != 1 {
		t.Errorf("transport saw %d requests, want 1 (no redirect follow)", transport.hits)
	}
}

func TestMaxDownloadBytesClampsAbsurdValues(t *testing.T) {
	t.Setenv(maxDownloadMBEnv, "9999999999999")
	if got, want := maxDownloadBytes(), int64(maxDownloadMBLimit)<<20; got != want {
		t.Errorf("maxDownloadBytes = %d, want %d", got, want)
	}
	t.Setenv(maxDownloadMBEnv, "0")
	if got := maxDownloadBytes(); got != 0 {
		t.Errorf("0 should disable the cap, got %d", got)
	}
	t.Setenv(maxDownloadMBEnv, "not-a-number")
	if got, want := maxDownloadBytes(), int64(defaultMaxDownloadMB)<<20; got != want {
		t.Errorf("invalid value = %d, want the default %d", got, want)
	}
}

func TestDownloadReportsStalls(t *testing.T) {
	restore := downloadStallTimeout
	downloadStallTimeout = 50 * time.Millisecond
	defer func() { downloadStallTimeout = restore }()

	post := api.Post{ID: 6, Site: "rule34", Video: true}
	outPath := filepath.Join(t.TempDir(), "6_6.mp4")
	d := New(t.TempDir(), 1)
	d.http = &http.Client{Transport: &stallingTransport{}}
	res := d.downloadFile(post, api.Stream{URL: "https://cdn.example.org/v/6.mp4"}, outPath)
	if res.Err == nil {
		t.Fatal("stalled download should fail")
	}
	if !strings.Contains(res.Err.Error(), "stalled") {
		t.Errorf("error = %v, want a stall message", res.Err)
	}
	if _, err := os.Stat(outPath); err == nil {
		t.Error("stalled download should not publish a file")
	}
}

func TestDownloadOneRejectsNonMediaSchemes(t *testing.T) {
	for _, raw := range []string{"concat:/etc/passwd", "file:///etc/passwd", "data:text/plain;base64,aGk=", "/etc/passwd"} {
		post := api.Post{ID: 2, Site: "rule34", Image: "2.mp4", FileURL_: raw}
		if res := New(t.TempDir(), 1).downloadOne(post); res.Err == nil {
			t.Errorf("downloadOne(%q) succeeded, want rejection", raw)
		} else if res.Path != "" {
			t.Errorf("downloadOne(%q) wrote %q", raw, res.Path)
		}
	}
}

func TestHLSArgsWhitelistsProtocols(t *testing.T) {
	stream := api.Stream{
		URL:     "https://ev-h.phncdn.com/hls/1.m3u8",
		Cookie:  "session=abc",
		Referer: "https://www.pornhub.com/view_video.php?viewkey=1",
	}
	args := hlsArgs(stream, api.Post{ID: 1, Site: "pornhub", Video: true}, "/tmp/out.part.mp4")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-protocol_whitelist "+mediaProtocolWhitelist) {
		t.Errorf("hls args missing protocol whitelist: %s", joined)
	}
	if strings.Contains(mediaProtocolWhitelist, "file") || strings.Contains(mediaProtocolWhitelist, "concat") {
		t.Errorf("protocol whitelist %q allows local protocols", mediaProtocolWhitelist)
	}
	if args[len(args)-1] != "/tmp/out.part.mp4" {
		t.Errorf("output must be the last argument: %s", joined)
	}
	offSite := stream
	offSite.URL = "https://evil.example/hls/1.m3u8"
	args = hlsArgs(offSite, api.Post{ID: 1, Site: "pornhub", Video: true}, "/tmp/out.part.mp4")
	if strings.Contains(strings.Join(args, " "), "session=abc") {
		t.Errorf("hls args leaked the cookie off-site: %s", strings.Join(args, " "))
	}
}
