package dl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"moxiu/r34-dl/api"
)

const downloadUserAgent = "r34-dl/viewer"

const ffmpegDownloadTimeout = 2 * time.Hour

type Result struct {
	Post api.Post
	Path string
	Err  error
}

type Downloader struct {
	http    *http.Client
	workers int
	outDir  string
}

func New(outDir string, workers int) *Downloader {
	if workers <= 0 {
		workers = 4
	}
	return &Downloader{
		http:    newHTTPClient(),
		workers: workers,
		outDir:  outDir,
	}
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   15 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          32,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
	}
}

func (d *Downloader) DownloadAll(posts []api.Post) <-chan Result {
	results := make(chan Result, len(posts))

	if err := os.MkdirAll(d.outDir, 0o755); err != nil {
		go func() {
			results <- Result{Err: fmt.Errorf("creating output dir: %w", err)}
			close(results)
		}()
		return results
	}

	jobs := make(chan api.Post)
	var wg sync.WaitGroup

	for i := 0; i < d.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for post := range jobs {
				results <- d.downloadOne(post)
			}
		}()
	}

	go func() {
		for _, p := range posts {
			jobs <- p
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

func (d *Downloader) downloadOne(post api.Post) Result {
	stream, err := api.Media(post)
	if err != nil {
		return Result{Post: post, Err: err}
	}
	if !validMediaURL(stream.URL) {
		return Result{Post: post, Err: fmt.Errorf("unusable media url for post %d", post.ID)}
	}
	outPath := filepath.Join(d.outDir, outputName(post))
	if stream.IsHLS() {
		return d.downloadHLS(post, stream, outPath)
	}
	return d.downloadFile(post, stream, outPath)
}

func validMediaURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func outputName(post api.Post) string {
	if post.Video {
		return fmt.Sprintf("%s_%d.mp4", sanitizeFileName(post.Site), post.ID)
	}
	return fmt.Sprintf("%d_%s", post.ID, sanitizeFileName(post.Image))
}

func sanitizeFileName(raw string) string {
	base := filepath.Base(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	name := strings.Trim(b.String(), "._")
	if name == "" {
		return "file"
	}
	return name
}

func (d *Downloader) downloadFile(post api.Post, stream api.Stream, outPath string) Result {
	req, err := http.NewRequest(http.MethodGet, stream.URL, nil)
	if err != nil {
		return Result{Post: post, Err: err}
	}
	req.Header.Set("User-Agent", downloadUserAgent)
	if stream.Referer != "" {
		req.Header.Set("Referer", stream.Referer)
	}
	if stream.Cookie != "" {
		req.Header.Set("Cookie", stream.Cookie)
	}

	resp, err := d.http.Do(req)
	if err != nil {
		return Result{Post: post, Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return Result{Post: post, Err: fmt.Errorf("status %d for post %d", resp.StatusCode, post.ID)}
	}

	tmp := outPath + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return Result{Post: post, Err: err}
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return Result{Post: post, Err: err}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return Result{Post: post, Err: err}
	}
	if err := os.Rename(tmp, outPath); err != nil {
		os.Remove(tmp)
		return Result{Post: post, Err: err}
	}
	return Result{Post: post, Path: outPath}
}

func (d *Downloader) downloadHLS(post api.Post, stream api.Stream, outPath string) Result {
	binary, err := exec.LookPath("ffmpeg")
	if err != nil {
		return Result{Post: post, Err: errors.New("ffmpeg is required to save this site's videos")}
	}

	tmp := strings.TrimSuffix(outPath, ".mp4") + ".part.mp4"
	ctx, cancel := context.WithTimeout(context.Background(), ffmpegDownloadTimeout)
	defer cancel()

	args := []string{"-loglevel", "error", "-nostdin", "-y", "-user_agent", downloadUserAgent}
	var headers strings.Builder
	if stream.Cookie != "" {
		fmt.Fprintf(&headers, "Cookie: %s\r\n", stream.Cookie)
	}
	if stream.Referer != "" {
		fmt.Fprintf(&headers, "Referer: %s\r\n", stream.Referer)
	}
	if headers.Len() > 0 {
		args = append(args, "-headers", headers.String())
	}
	args = append(args, "-i", stream.URL, "-c", "copy", "-movflags", "+faststart", tmp)

	cmd := exec.CommandContext(ctx, binary, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		os.Remove(tmp)
		msg := lastLine(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return Result{Post: post, Err: fmt.Errorf("ffmpeg: %s", msg)}
	}
	if err := os.Rename(tmp, outPath); err != nil {
		os.Remove(tmp)
		return Result{Post: post, Err: err}
	}
	return Result{Post: post, Path: outPath}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}
