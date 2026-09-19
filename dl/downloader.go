package dl

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/safe"
)

const downloadUserAgent = "r34-dl/viewer"

const ffmpegDownloadTimeout = 2 * time.Hour

const mediaProtocolWhitelist = "http,https,tcp,tls,crypto"

const (
	defaultMaxDownloadMB = 8192
	maxDownloadMBEnv     = "R34_DL_MAX_DOWNLOAD_MB"
	mediaReadTimeout     = "60000000"
	maxDownloadMBLimit   = 1 << 20
)

var downloadStallTimeout = 60 * time.Second

var randRead = rand.Read

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
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           safe.PublicDialContext(dialer),
			MaxIdleConns:          32,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if !safe.PublicMediaURL(req.URL.String()) {
				return fmt.Errorf("refusing to follow redirect to %s", req.URL.Host)
			}
			return nil
		},
	}
}

func maxDownloadBytes() int64 {
	raw := strings.TrimSpace(os.Getenv(maxDownloadMBEnv))
	if raw == "" {
		return int64(defaultMaxDownloadMB) << 20
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return int64(defaultMaxDownloadMB) << 20
	}
	if n == 0 {
		return 0
	}
	if n > maxDownloadMBLimit {
		n = maxDownloadMBLimit
	}
	return int64(n) << 20
}

func copyWithStallTimeout(dst io.Writer, src io.ReadCloser, limit int64) (int64, error) {
	var stalled atomic.Bool
	timer := time.AfterFunc(downloadStallTimeout, func() {
		stalled.Store(true)
		_ = src.Close()
	})
	defer timer.Stop()
	buf := make([]byte, 64<<10)
	var total int64
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			timer.Reset(downloadStallTimeout)
			if limit > 0 && total+int64(n) > limit {
				return total, fmt.Errorf("download exceeded the %d MiB limit", limit>>20)
			}
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return total, writeErr
			}
			total += int64(n)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			if stalled.Load() {
				return total, fmt.Errorf("download stalled for %s without progress", downloadStallTimeout)
			}
			return total, readErr
		}
	}
}

func partName(outPath string, keepExt bool) string {
	var seed [6]byte
	if _, err := randRead(seed[:]); err != nil {
		binary.BigEndian.PutUint32(seed[:4], uint32(time.Now().UnixNano()))
	}
	suffix := fmt.Sprintf(".part-%d-%s", os.Getpid(), hex.EncodeToString(seed[:]))
	if !keepExt {
		return outPath + suffix
	}
	ext := filepath.Ext(outPath)
	return strings.TrimSuffix(outPath, ext) + suffix + ext
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
	if !safe.PublicMediaURL(stream.URL) {
		return Result{Post: post, Err: fmt.Errorf("refusing non-public media url for post %d (set %s=1 to allow)", post.ID, safe.AllowPrivateHostsEnv)}
	}
	outPath := filepath.Join(d.outDir, outputName(post))
	if stream.IsHLS() {
		return d.downloadHLS(post, stream, outPath)
	}
	return d.downloadFile(post, stream, outPath)
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
	allowedHost := api.MediaHostAllowed(post.Site, stream.URL)
	if stream.Referer != "" && allowedHost {
		req.Header.Set("Referer", stream.Referer)
	}
	if stream.Cookie != "" && allowedHost {
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

	tmp := partName(outPath, false)
	f, err := os.Create(tmp)
	if err != nil {
		return Result{Post: post, Err: err}
	}
	if _, err := copyWithStallTimeout(f, resp.Body, maxDownloadBytes()); err != nil {
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

	tmp := partName(outPath, true)
	ctx, cancel := context.WithTimeout(context.Background(), ffmpegDownloadTimeout)
	defer cancel()

	args := hlsArgs(stream, post, tmp)
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

func hlsArgs(stream api.Stream, post api.Post, out string) []string {
	args := []string{"-loglevel", "error", "-nostdin", "-y", "-user_agent", downloadUserAgent}
	allowedHost := api.MediaHostAllowed(post.Site, stream.URL)
	var headers strings.Builder
	if stream.Cookie != "" && allowedHost {
		fmt.Fprintf(&headers, "Cookie: %s\r\n", stream.Cookie)
	}
	if stream.Referer != "" && allowedHost {
		fmt.Fprintf(&headers, "Referer: %s\r\n", stream.Referer)
	}
	if headers.Len() > 0 {
		args = append(args, "-headers", headers.String())
	}
	args = append(args,
		"-protocol_whitelist", mediaProtocolWhitelist,
		"-rw_timeout", mediaReadTimeout,
		"-i", stream.URL,
		"-c", "copy", "-movflags", "+faststart", out,
	)
	return args
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}
