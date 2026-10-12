package dl

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/safe"
)

type stubTransport struct {
	status int
	body   string
}

func TestHLSDownloadThroughRelay(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	t.Setenv(safe.AllowPrivateHostsEnv, "1")
	dir := t.TempDir()
	playlist := filepath.Join(dir, "video.m3u8")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=30:duration=2", "-c:v", "libx264", "-preset", "ultrafast", "-crf", "0", "-f", "hls", "-hls_time", "1", playlist)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate HLS fixture: %v: %s", err, out)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer server.Close()
	post := api.Post{ID: 1, Video: true}
	stream := api.Stream{URL: server.URL + "/video.m3u8"}
	for _, limit := range []string{"0", "1"} {
		t.Run("limit_"+limit, func(t *testing.T) {
			t.Setenv(maxDownloadMBEnv, limit)
			outDir := t.TempDir()
			outPath := filepath.Join(outDir, "video.mp4")
			result := New(outDir, 1).downloadHLS(post, stream, outPath)
			if limit == "0" {
				if result.Err != nil {
					t.Fatalf("HLS download: %v", result.Err)
				}
				info, err := os.Stat(outPath)
				if err != nil || info.Size() < 1<<20 {
					t.Fatalf("HLS output missing or too small: %v", err)
				}
			} else {
				if result.Err == nil || !strings.Contains(result.Err.Error(), "limit") {
					t.Fatalf("oversized HLS download: %v", result.Err)
				}
				entries, err := os.ReadDir(outDir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("failed HLS download left files: %v, %v", entries, err)
				}
			}
		})
	}
}

func (s stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

func stubDownloader(dir string, transport stubTransport) *Downloader {
	downloader := New(dir, 1)
	downloader.http = &http.Client{Transport: transport}
	return downloader
}

func TestOutputNameForVideoPosts(t *testing.T) {
	got := outputName(api.Post{ID: 42, Site: "pornhub", Video: true})
	if got != "pornhub_42.mp4" {
		t.Errorf("outputName = %q", got)
	}
}

func TestOutputNameForImages(t *testing.T) {
	got := outputName(api.Post{ID: 7, Image: "7.jpg"})
	if got != "7_7.jpg" {
		t.Errorf("outputName = %q", got)
	}
}

func TestOutputNameCannotEscapeOutDir(t *testing.T) {
	got := outputName(api.Post{ID: 7, Image: "../../etc/passwd"})
	if strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") {
		t.Errorf("outputName = %q, want a flat file name", got)
	}
	if filepath.Join("/tmp/out", got) != filepath.Join("/tmp/out", filepath.Base(got)) {
		t.Errorf("outputName %q escapes the download dir", got)
	}
}

func TestOutputNameForEmptyImage(t *testing.T) {
	got := outputName(api.Post{ID: 9})
	if got != "9_file" {
		t.Errorf("outputName = %q", got)
	}
}

func TestDownloadWritesTheFile(t *testing.T) {
	dir := t.TempDir()
	downloader := stubDownloader(dir, stubTransport{body: "image-bytes"})
	result := downloader.downloadOne(api.Post{
		ID:       3,
		Image:    "3.jpg",
		FileURL_: "https://safebooru.org/images/3/3.jpg",
	})
	if result.Err != nil {
		t.Fatalf("download: %v", result.Err)
	}
	if result.Path != filepath.Join(dir, "3_3.jpg") {
		t.Errorf("path = %q", result.Path)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("reading download: %v", err)
	}
	if string(data) != "image-bytes" {
		t.Errorf("download = %q", data)
	}
	if _, err := os.Stat(result.Path + ".part"); err == nil {
		t.Error("temporary file was left behind")
	}
}

func TestDownloadFailsOnBadStatus(t *testing.T) {
	downloader := stubDownloader(t.TempDir(), stubTransport{status: http.StatusNotFound})
	result := downloader.downloadOne(api.Post{ID: 4, Image: "4.jpg", FileURL_: "https://safebooru.org/images/4/4.jpg"})
	if result.Err == nil {
		t.Fatal("expected a failure for a 404")
	}
	if !strings.Contains(result.Err.Error(), "404") {
		t.Errorf("error = %v, want the status in it", result.Err)
	}
}

func TestHLSWithoutFfmpegFails(t *testing.T) {
	t.Setenv("PATH", "")
	downloader := New(t.TempDir(), 1)
	result := downloader.downloadHLS(
		api.Post{ID: 5, Site: "xhamster", Video: true},
		api.Stream{URL: "https://example.com/video.m3u8"},
		filepath.Join(t.TempDir(), "xhamster_5.mp4"),
	)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "ffmpeg") {
		t.Errorf("error = %v, want a missing ffmpeg message", result.Err)
	}
}

func TestDownloadAllReportsEveryPost(t *testing.T) {
	posts := []api.Post{
		{ID: 1, Image: "1.jpg", FileURL_: "https://safebooru.org/images/1/1.jpg"},
		{ID: 2, Image: "2.jpg", FileURL_: "https://safebooru.org/images/2/2.jpg"},
	}
	downloader := stubDownloader(t.TempDir(), stubTransport{body: "bytes"})
	downloader.workers = 2
	seen := 0
	for result := range downloader.DownloadAll(posts) {
		if result.Err != nil {
			t.Errorf("post %d failed: %v", result.Post.ID, result.Err)
		}
		seen++
	}
	if seen != len(posts) {
		t.Errorf("got %d results, want %d", seen, len(posts))
	}
}
