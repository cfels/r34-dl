package api

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"moxiu/r34-dl/safe"
)

const MediaProtocols = "http,tcp,crypto"

var playlistURI = regexp.MustCompile(`URI="([^"]*)"`)

func MediaEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		// FFmpeg honors no_proxy even when -http_proxy is passed explicitly.
		if !strings.EqualFold(key, "no_proxy") {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func (s Stream) Headers(site, rawURL string) http.Header {
	headers := make(http.Header)
	if !MediaHostAllowed(site, rawURL) || !strings.HasPrefix(rawURL, "https://") {
		return headers
	}
	if s.Cookie != "" && validHeaderValue(s.Cookie) {
		headers.Set("Cookie", s.Cookie)
	}
	if s.Referer != "" && validHeaderValue(s.Referer) && safe.MediaURL(s.Referer) {
		headers.Set("Referer", s.Referer)
	}
	return headers
}

func validHeaderValue(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

type MediaRelay struct {
	URL   string
	Proxy string

	server *http.Server
	client *http.Client
	stream Stream
	site   string
	prefix string
	mu     sync.Mutex
	urls   map[string]string
	paths  map[string]string
}

func NewMediaRelay(stream Stream, site string) (*MediaRelay, error) {
	if !safe.PublicMediaURL(stream.URL) {
		return nil, fmt.Errorf("refusing unsupported or non-public media URL")
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	relay := &MediaRelay{
		Proxy:  "http://" + listener.Addr().String(),
		client: newSiteHTTPClient(),
		stream: stream,
		site:   site,
		prefix: "/" + hex.EncodeToString(token[:]) + "/",
		urls:   make(map[string]string),
		paths:  make(map[string]string),
	}
	relay.client.Timeout = 0
	relay.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if !safe.PublicMediaURL(req.URL.String()) {
			return fmt.Errorf("refusing non-public media redirect")
		}
		req.Header.Del("Cookie")
		req.Header.Del("Referer")
		for key, values := range stream.Headers(site, req.URL.String()) {
			req.Header[key] = values
		}
		return nil
	}
	relay.URL = relay.mediaURL(stream.URL)
	relay.server = &http.Server{Handler: relay, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute}
	go func() { _ = relay.server.Serve(listener) }()
	return relay, nil
}

func (r *MediaRelay) Close() {
	_ = r.server.Close()
	r.client.CloseIdleConnections()
}

func (r *MediaRelay) mediaURL(rawURL string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if localPath := r.paths[rawURL]; localPath != "" {
		return r.Proxy + localPath
	}
	u, _ := url.Parse(rawURL)
	ext := path.Ext(u.Path)
	if len(ext) > 10 || strings.ContainsAny(ext, "/\\") {
		ext = ""
	}
	localPath := fmt.Sprintf("%s%d%s", r.prefix, len(r.urls), ext)
	r.urls[localPath] = rawURL
	r.paths[rawURL] = localPath
	return r.Proxy + localPath
}

func (r *MediaRelay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if "http://"+req.Host != r.Proxy {
		http.Error(w, "invalid media origin", http.StatusForbidden)
		return
	}
	r.mu.Lock()
	rawURL := r.urls[req.URL.Path]
	r.mu.Unlock()
	if rawURL == "" || !safe.PublicMediaURL(rawURL) {
		http.Error(w, "invalid media destination", http.StatusForbidden)
		return
	}
	upstream, err := http.NewRequestWithContext(req.Context(), req.Method, rawURL, nil)
	if err != nil {
		http.Error(w, "invalid media request", http.StatusBadRequest)
		return
	}
	upstream.Header = r.stream.Headers(r.site, rawURL)
	upstream.Header.Set("User-Agent", "r34-dl/viewer")
	if value := req.Header.Get("Range"); value != "" {
		upstream.Header.Set("Range", value)
	}
	resp, err := r.client.Do(upstream)
	if err != nil {
		http.Error(w, "media request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		w.WriteHeader(resp.StatusCode)
		return
	}
	reader := bufio.NewReader(resp.Body)
	header, _ := reader.Peek(7)
	if req.Method != http.MethodHead && string(header) == "#EXTM3U" {
		body, err := io.ReadAll(io.LimitReader(reader, maxSiteBody+1))
		if err != nil || len(body) > maxSiteBody {
			http.Error(w, "playlist exceeds size limit", http.StatusBadGateway)
			return
		}
		playlist, err := r.rewritePlaylist(string(body), resp.Request.URL)
		if err != nil {
			http.Error(w, "invalid playlist destination", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, playlist)
		return
	}
	for _, key := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if value := resp.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if req.Method != http.MethodHead {
		_, _ = io.Copy(w, reader)
	}
}

func (r *MediaRelay) rewritePlaylist(body string, base *url.URL) (string, error) {
	resolve := func(reference string) (string, error) {
		u, err := url.Parse(reference)
		if err != nil {
			return "", err
		}
		rawURL := base.ResolveReference(u).String()
		if !safe.MediaURL(rawURL) {
			return "", fmt.Errorf("unsupported playlist URL")
		}
		return r.mediaURL(rawURL), nil
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			resolved, err := resolve(line)
			if err != nil {
				return "", err
			}
			lines[i] = resolved
			continue
		}
		var resolveErr error
		lines[i] = playlistURI.ReplaceAllStringFunc(line, func(attribute string) string {
			resolved, err := resolve(attribute[5 : len(attribute)-1])
			if err != nil {
				resolveErr = err
				return ""
			}
			return `URI="` + resolved + `"`
		})
		if resolveErr != nil {
			return "", resolveErr
		}
	}
	return strings.Join(lines, "\n"), nil
}
