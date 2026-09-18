package dl

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"moxiu/r34-dl/api"
)

type stubTransport struct {
	status int
	body   string
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
