package api

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"moxiu/r34-dl/safe"
)

const browserUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"

const maxSiteBody = 8 << 20

const streamTimeout = 45 * time.Second

type Stream struct {
	URL     string
	Cookie  string
	Referer string
}

func (s Stream) IsHLS() bool {
	base := s.URL
	if i := strings.IndexAny(base, "?#"); i >= 0 {
		base = base[:i]
	}
	return strings.HasSuffix(strings.ToLower(base), ".m3u8")
}

type Streamer interface {
	Media(p Post) (Stream, error)
}

var siteOrder = []string{"safebooru", "rule34", "pornhub", "xvideos", "xhamster"}

var mediaHostSuffixes = map[string][]string{
	"pornhub":  {"pornhub.com", "pornhub.org", "phncdn.com"},
	"xvideos":  {"xvideos.com", "xvideos.es", "xvideos-cdn.com", "evaxxx.com"},
	"xhamster": {"xhamster.com", "xhamster.desi", "xhcdn.com"},
}

func MediaHostAllowed(site, rawURL string) bool {
	suffixes := mediaHostSuffixes[strings.ToLower(strings.TrimSpace(site))]
	if len(suffixes) == 0 {
		return false
	}
	return safe.HostIn(safe.Host(rawURL), suffixes)
}

func streamFor(site, rawURL, cookie, referer string) Stream {
	if cookie != "" && !MediaHostAllowed(site, rawURL) {
		cookie = ""
	}
	return Stream{URL: rawURL, Cookie: cookie, Referer: referer}
}

func SiteNames() []string {
	names := make([]string, len(siteOrder))
	copy(names, siteOrder)
	return names
}

func KnownSite(name string) bool {
	for _, site := range siteOrder {
		if name == site {
			return true
		}
	}
	return false
}

func IsAdultSite(name string) bool {
	switch name {
	case "rule34", "pornhub", "xvideos", "xhamster":
		return true
	}
	return false
}

func Media(p Post) (Stream, error) {
	if !p.Video {
		return Stream{URL: p.FileURL()}, nil
	}
	streamer := mediaStreamer(p.Site)
	if streamer == nil {
		return Stream{}, fmt.Errorf("no media source for %q", p.Site)
	}
	return streamer.Media(p)
}

func mediaStreamer(site string) Streamer {
	switch site {
	case "pornhub":
		return sharedPornHub
	case "xvideos":
		return sharedXVideos
	case "xhamster":
		return sharedXHamster
	}
	return nil
}

var (
	siteHTTP = newSiteHTTPClient()

	sharedPornHub  = &PornHubClient{http: siteHTTP}
	sharedXVideos  = &XVideosClient{http: siteHTTP}
	sharedXHamster = &XHamsterClient{http: siteHTTP}
)

func newSiteHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           safe.PublicDialContext(dialer),
		MaxIdleConns:          16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: time.Second,
		ForceAttemptHTTP2:     false,
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	return &http.Client{Timeout: streamTimeout, Transport: transport}
}

func NewPornHubClient() *PornHubClient   { return sharedPornHub }
func NewXVideosClient() *XVideosClient   { return sharedXVideos }
func NewXHamsterClient() *XHamsterClient { return sharedXHamster }

func siteGet(client *http.Client, rawURL string) ([]byte, error) {
	return siteGetRef(client, rawURL, "")
}

func siteGetRef(client *http.Client, rawURL, referer string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("couldn't build request: %w", err)
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", req.URL.Host, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSiteBody))
	if err != nil {
		return nil, fmt.Errorf("couldn't read response: %w", err)
	}
	return body, nil
}

func siteGetJSON(client *http.Client, rawURL, referer string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("couldn't build request: %w", err)
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSiteBody))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("couldn't read response: %w", err)
	}
	return body, resp.StatusCode, nil
}

func jsonObjectAt(page string, start int) string {
	if start < 0 || start >= len(page) || page[start] != '{' {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(page); i++ {
		ch := page[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return page[start : i+1]
			}
		}
	}
	return ""
}

func cookieHeader(jar http.CookieJar, rawURL string) string {
	if jar == nil {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	var parts []string
	for _, c := range jar.Cookies(u) {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func digitsOnly(s string) int {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	n, err := strconv.Atoi(b.String())
	if err != nil {
		return 0
	}
	return n
}

func clampPosts(posts []Post, limit int) []Post {
	if limit > 0 && len(posts) > limit {
		return posts[:limit]
	}
	return posts
}

func mediaClient() *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return newSiteHTTPClient()
	}
	client := newSiteHTTPClient()
	client.Jar = jar
	return client
}

type PornHubClient struct {
	http *http.Client

	sessionMu sync.Mutex
	session   *http.Client
	token     string
}

func (c *PornHubClient) Name() string { return "pornhub" }

var (
	phTokenRe          = regexp.MustCompile(`data-token="([^"]+)"`)
	errPornHubBadToken = errors.New("pornhub rejected the search token")
)

func (c *PornHubClient) Autocomplete(prefix string) ([]string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.session == nil {
		c.session = mediaClient()
	}
	if c.token == "" {
		token, err := c.refreshToken(prefix)
		if err != nil {
			return nil, err
		}
		c.token = token
	}
	candidates, err := c.suggest(prefix, c.token)
	if errors.Is(err, errPornHubBadToken) {
		token, refreshErr := c.refreshToken(prefix)
		if refreshErr != nil {
			c.token = ""
			return nil, refreshErr
		}
		c.token = token
		candidates, err = c.suggest(prefix, token)
	}
	if err != nil {
		c.token = ""
		return nil, err
	}
	return matchPredictions(candidates, prefix, maxPredictions), nil
}

func (c *PornHubClient) refreshToken(prefix string) (string, error) {
	body, err := siteGet(c.session, c.searchURL(prefix, 0))
	if err != nil {
		return "", err
	}
	token := firstMatch(phTokenRe, string(body))
	if token == "" {
		return "", fmt.Errorf("pornhub did not expose a search token")
	}
	return token, nil
}

func (c *PornHubClient) suggest(prefix, token string) ([]string, error) {
	q := url.Values{}
	q.Set("q", prefix)
	q.Set("alt", "0")
	q.Set("pornstars", "1")
	q.Set("token", token)
	body, status, err := siteGetJSON(
		c.session,
		"https://www.pornhub.com/api/v1/video/search_autocomplete?"+q.Encode(),
		c.searchURL(prefix, 0),
	)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return nil, errPornHubBadToken
	default:
		return nil, fmt.Errorf("pornhub autocomplete returned %s", http.StatusText(status))
	}
	return suggestionFields(body), nil
}

func (c *PornHubClient) searchURL(query string, page int) string {
	q := url.Values{}
	if query != "" {
		q.Set("search", query)
	}
	q.Set("page", strconv.Itoa(page+1))
	return "https://www.pornhub.com/video/search?" + q.Encode()
}

var (
	phItemRe  = regexp.MustCompile(`(?s)<li class="pcVideoListItem.*?</li>`)
	phCountRe = regexp.MustCompile(`"resultsCount":(\d+)`)
	phIDRe    = regexp.MustCompile(`data-video-id="(\d+)"`)
	phPageRe  = regexp.MustCompile(`href="(/view_video\.php\?viewkey=[^"]+)"`)
	phTitleRe = regexp.MustCompile(`\stitle="([^"]*)"`)
	phThumbRe = regexp.MustCompile(`<img\s+src="(https?://[^"]+)"`)
	phDurRe   = regexp.MustCompile(`<var class="duration">([^<]*)</var>`)
)

func parsePornHubSearch(body []byte) []Post {
	page := string(body)
	items := phItemRe.FindAllString(page, -1)
	posts := make([]Post, 0, len(items))
	for _, item := range items {
		id := digitsOnly(firstMatch(phIDRe, item))
		href := firstMatch(phPageRe, item)
		if id == 0 || href == "" {
			continue
		}
		title := phTitleRe.FindStringSubmatch(item)
		post := Post{
			ID:       id,
			Site:     "pornhub",
			Video:    true,
			PageURL:  safe.URLText("https://www.pornhub.com" + href),
			Thumb:    safe.URLText(firstMatch(phThumbRe, item)),
			Duration: safe.Limit(firstMatch(phDurRe, item), 16),
		}
		if len(title) > 1 {
			post.Title = safe.Text(html.UnescapeString(title[1]))
		}
		posts = append(posts, post)
	}
	return posts
}

func (c *PornHubClient) SearchPosts(tags string, limit, page int) ([]Post, error) {
	body, err := siteGet(c.http, c.searchURL(tags, page))
	if err != nil {
		return nil, err
	}
	return clampPosts(parsePornHubSearch(body), limit), nil
}

func (c *PornHubClient) CountPosts(tags string) (int, error) {
	body, err := siteGet(c.http, c.searchURL(tags, 0))
	if err != nil {
		return 0, err
	}
	count := firstMatch(phCountRe, string(body))
	if count == "" {
		return 0, fmt.Errorf("pornhub did not report a result count")
	}
	return strconv.Atoi(count)
}

type phMedia struct {
	Format   string          `json:"format"`
	Height   int             `json:"height"`
	VideoURL string          `json:"videoUrl"`
	Quality  json.RawMessage `json:"quality"`
}

func parsePornHubMedia(body []byte) ([]phMedia, error) {
	page := string(body)
	start := strings.Index(page, `"mediaDefinitions":`)
	if start < 0 {
		return nil, fmt.Errorf("pornhub page had no media definitions")
	}
	open := strings.Index(page[start:], "[")
	if open < 0 {
		return nil, fmt.Errorf("pornhub page had no media definitions")
	}
	open += start
	depth := 0
	end := -1
	for i := open; i < len(page); i++ {
		switch page[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				end = i + 1
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("pornhub media definitions were truncated")
	}
	var entries []phMedia
	if err := json.Unmarshal([]byte(page[open:end]), &entries); err != nil {
		return nil, fmt.Errorf("couldn't parse pornhub media: %w", err)
	}
	streams := make([]phMedia, 0, len(entries))
	for _, e := range entries {
		if e.VideoURL == "" {
			continue
		}
		e.VideoURL = strings.ReplaceAll(e.VideoURL, `\/`, `/`)
		streams = append(streams, e)
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("pornhub offered no playable stream")
	}
	sort.Slice(streams, func(i, j int) bool { return streams[i].Height > streams[j].Height })
	return streams, nil
}

func (c *PornHubClient) Media(p Post) (Stream, error) {
	if p.PageURL == "" {
		return Stream{}, fmt.Errorf("pornhub post %d has no page url", p.ID)
	}
	var lastErr error
	for attempt := 0; attempt < pornhubResolveAttempts; attempt++ {
		session := mediaClient()
		body, err := siteGet(session, p.PageURL)
		if err != nil {
			lastErr = err
			continue
		}
		entries, err := parsePornHubMedia(body)
		if err != nil {
			return Stream{}, err
		}
		cookie := cookieHeader(session.Jar, p.PageURL)
		stream, ok, retry := pornhubResolve(session, entries, cookie, p.PageURL)
		if ok {
			return stream, nil
		}
		lastErr = fmt.Errorf("pornhub post %d has no playable stream", p.ID)
		if !retry {
			break
		}
	}
	return Stream{}, lastErr
}

const pornhubResolveAttempts = 4

func pornhubResolve(session *http.Client, entries []phMedia, cookie, pageURL string) (Stream, bool, bool) {
	if stream, ok := pornhubProgressive(session, entries, cookie, pageURL); ok {
		return stream, true, false
	}
	for _, entry := range entries {
		if entry.Format != "hls" {
			continue
		}
		reachable, dead := hlsReachable(session, entry.VideoURL, pageURL)
		if reachable {
			return streamFor("pornhub", entry.VideoURL, cookie, pageURL), true, false
		}
		if dead {
			return Stream{}, false, true
		}
	}
	return Stream{}, false, false
}

func pornhubProgressive(session *http.Client, entries []phMedia, cookie, pageURL string) (Stream, bool) {
	for _, entry := range entries {
		if !strings.Contains(entry.VideoURL, "/video/get_media") {
			continue
		}
		body, err := siteGetRef(session, entry.VideoURL, pageURL)
		if err != nil {
			continue
		}
		var formats []phMedia
		if err := json.Unmarshal(body, &formats); err != nil {
			continue
		}
		sort.Slice(formats, func(i, j int) bool { return formats[i].Height > formats[j].Height })
		for _, format := range formats {
			mediaURL := strings.ReplaceAll(format.VideoURL, `\/`, `/`)
			if !safe.MediaURL(mediaURL) || !segmentReachable(session, mediaURL, pageURL) {
				continue
			}
			return streamFor("pornhub", mediaURL, cookie, pageURL), true
		}
	}
	return Stream{}, false
}

func hlsReachable(client *http.Client, masterURL, referer string) (bool, bool) {
	master, err := siteGetRef(client, masterURL, referer)
	if err != nil {
		return false, true
	}
	variant := firstPlaylistEntry(string(master))
	if variant == "" {
		return false, true
	}
	variantURL := resolveReference(masterURL, variant)
	if variantURL == "" {
		return false, true
	}
	playlist, err := siteGetRef(client, variantURL, referer)
	if err != nil {
		return false, false
	}
	segment := firstPlaylistEntry(string(playlist))
	if segment == "" {
		return false, false
	}
	return segmentReachable(client, resolveReference(variantURL, segment), referer), false
}

func firstPlaylistEntry(playlist string) string {
	for _, line := range strings.Split(playlist, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

func resolveReference(base, reference string) string {
	parsedBase, err := url.Parse(base)
	if err != nil {
		return ""
	}
	parsedRef, err := url.Parse(reference)
	if err != nil {
		return ""
	}
	return parsedBase.ResolveReference(parsedRef).String()
}

func segmentReachable(client *http.Client, segmentURL, referer string) bool {
	req, err := http.NewRequest(http.MethodGet, segmentURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Range", "bytes=0-1")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 2)
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
}

type XVideosClient struct {
	http *http.Client

	tagsMu  sync.Mutex
	tags    []string
	fetched time.Time
}

func (c *XVideosClient) Name() string { return "xvideos" }

const xvideosTagTTL = 30 * time.Minute

var xvTagLinkRe = regexp.MustCompile(`href="/tags/([a-z0-9-]+)"`)

func (c *XVideosClient) Autocomplete(prefix string) ([]string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}
	tags, err := c.tagIndex()
	if err != nil {
		return nil, err
	}
	return matchPredictions(tags, prefix, maxPredictions), nil
}

func (c *XVideosClient) tagIndex() ([]string, error) {
	c.tagsMu.Lock()
	defer c.tagsMu.Unlock()
	if len(c.tags) > 0 && time.Since(c.fetched) < xvideosTagTTL {
		return c.tags, nil
	}
	body, err := siteGet(c.http, "https://www.xvideos.com/tags")
	if err != nil {
		return nil, err
	}
	tags := parseXVideosTags(body)
	if len(tags) == 0 {
		return nil, fmt.Errorf("xvideos listed no tags")
	}
	c.tags, c.fetched = tags, time.Now()
	return tags, nil
}

func parseXVideosTags(body []byte) []string {
	matches := xvTagLinkRe.FindAllStringSubmatch(string(body), -1)
	tags := make([]string, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, match := range matches {
		slug := strings.Trim(match[1], "-")
		if slug == "" {
			continue
		}
		name := safe.Tag(strings.Join(strings.Fields(strings.ReplaceAll(slug, "-", " ")), " "))
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		tags = append(tags, name)
	}
	return tags
}

func (c *XVideosClient) searchURL(query string, page int) string {
	q := url.Values{}
	if query != "" {
		q.Set("k", query)
	}
	q.Set("p", strconv.Itoa(page))
	return "https://www.xvideos.com/?" + q.Encode()
}

var (
	xvCountRe = regexp.MustCompile(`<span class="sub">\s*\(([\d,]+)\s+results?\)`)
	xvIDRe    = regexp.MustCompile(`data-id="(\d+)"`)
	xvPageRe  = regexp.MustCompile(`href="(/video\.[^"]+)"`)
	xvTitleRe = regexp.MustCompile(`\stitle="([^"]*)"`)
	xvThumbRe = regexp.MustCompile(`data-src="(https?://[^"]+)"`)
	xvDurRe   = regexp.MustCompile(`<span class="duration">([^<]*)</span>`)
)

func parseXVideosSearch(body []byte) []Post {
	chunks := strings.Split(string(body), `<div id="video_`)
	posts := make([]Post, 0, len(chunks))
	for _, chunk := range chunks[1:] {
		id := digitsOnly(firstMatch(xvIDRe, chunk))
		href := firstMatch(xvPageRe, chunk)
		if id == 0 || href == "" {
			continue
		}
		post := Post{
			ID:       id,
			Site:     "xvideos",
			Video:    true,
			PageURL:  safe.URLText("https://www.xvideos.com" + href),
			Thumb:    safe.URLText(firstMatch(xvThumbRe, chunk)),
			Duration: safe.Limit(firstMatch(xvDurRe, chunk), 16),
		}
		if title := firstMatch(xvTitleRe, chunk); title != "" {
			post.Title = safe.Text(html.UnescapeString(title))
		}
		posts = append(posts, post)
	}
	return posts
}

func (c *XVideosClient) SearchPosts(tags string, limit, page int) ([]Post, error) {
	body, err := siteGet(c.http, c.searchURL(tags, page))
	if err != nil {
		return nil, err
	}
	return clampPosts(parseXVideosSearch(body), limit), nil
}

func (c *XVideosClient) CountPosts(tags string) (int, error) {
	body, err := siteGet(c.http, c.searchURL(tags, 0))
	if err != nil {
		return 0, err
	}
	raw := firstMatch(xvCountRe, string(body))
	if raw == "" {
		return 0, fmt.Errorf("xvideos did not report a result count")
	}
	return digitsOnly(strings.ReplaceAll(raw, ",", "")), nil
}

var (
	xvHLSRe  = regexp.MustCompile(`setVideoHLS\('([^']+)'\)`)
	xvHighRe = regexp.MustCompile(`setVideoUrlHigh\('([^']+)'\)`)
	xvLowRe  = regexp.MustCompile(`setVideoUrlLow\('([^']+)'\)`)
)

func (c *XVideosClient) Media(p Post) (Stream, error) {
	if p.PageURL == "" {
		return Stream{}, fmt.Errorf("xvideos post %d has no page url", p.ID)
	}
	body, err := siteGet(c.http, p.PageURL)
	if err != nil {
		return Stream{}, err
	}
	streamURL := xvideosStreamURL(string(body))
	if streamURL == "" {
		return Stream{}, fmt.Errorf("xvideos post %d has no playable stream", p.ID)
	}
	return Stream{URL: streamURL, Referer: p.PageURL}, nil
}

func xvideosStreamURL(page string) string {
	for _, re := range []*regexp.Regexp{xvHLSRe, xvHighRe, xvLowRe} {
		if match := firstMatch(re, page); match != "" {
			return match
		}
	}
	return ""
}

type XHamsterClient struct {
	http *http.Client

	cacheMu sync.Mutex
	cache   map[string][]string
}

func (c *XHamsterClient) Name() string { return "xhamster" }

const xhSuggestionKey = `"searchVideoSuggestions":`

type xhSuggestion struct {
	Text      string `json:"text"`
	PlainText string `json:"plainText"`
}

func (c *XHamsterClient) Autocomplete(prefix string) ([]string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}
	key := strings.ToLower(prefix)
	if cached, ok := c.cachedSuggestions(key); ok {
		return cached, nil
	}
	body, err := siteGet(c.http, c.searchURL(prefix, 0))
	if err != nil {
		return nil, err
	}
	tags := matchPredictions(parseXHamsterSuggestions(body), prefix, maxPredictions)
	c.storeSuggestions(key, tags)
	return tags, nil
}

func (c *XHamsterClient) cachedSuggestions(key string) ([]string, bool) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	tags, ok := c.cache[key]
	return tags, ok
}

func (c *XHamsterClient) storeSuggestions(key string, tags []string) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if c.cache == nil || len(c.cache) > 128 {
		c.cache = make(map[string][]string)
	}
	c.cache[key] = tags
}

func parseXHamsterSuggestions(body []byte) []string {
	page := string(body)
	start := strings.Index(page, xhSuggestionKey)
	if start < 0 {
		return nil
	}
	raw := jsonObjectAt(page, start+len(xhSuggestionKey))
	if raw == "" {
		return nil
	}
	var payload struct {
		Tags []xhSuggestion `json:"tags"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	tags := make([]string, 0, len(payload.Tags))
	for _, tag := range payload.Tags {
		text := safe.Tag(tag.Text)
		if text == "" {
			text = safe.Tag(tag.PlainText)
		}
		if text != "" {
			tags = append(tags, html.UnescapeString(text))
		}
	}
	return tags
}

func (c *XHamsterClient) searchURL(query string, page int) string {
	base := "https://xhamster.com/search/"
	if query != "" {
		base += url.PathEscape(query)
	}
	return base + "?page=" + strconv.Itoa(page+1)
}

var (
	xhCountRe = regexp.MustCompile(`"resultCount":"([^"]+)"`)
	xhIDRe    = regexp.MustCompile(`data-video-id="(\d+)"`)
	xhPageRe  = regexp.MustCompile(`href="(https://xhamster\.com/videos/[^"]+)"`)
	xhThumbRe = regexp.MustCompile(`thumb-image-container__image"[^>]*\ssrc="([^"]+)"`)
	xhLabelRe = regexp.MustCompile(`aria-label="([^"]*)"`)
	xhDurRe   = regexp.MustCompile(`data-role="video-duration-container".*?(\d{1,3}:\d{2}(?::\d{2})?)`)
)

func parseXHamsterSearch(body []byte) []Post {
	chunks := strings.Split(string(body), "thumb-list__item video-thumb")
	posts := make([]Post, 0, len(chunks))
	for _, chunk := range chunks[1:] {
		id := digitsOnly(firstMatch(xhIDRe, chunk))
		href := firstMatch(xhPageRe, chunk)
		if id == 0 || href == "" {
			continue
		}
		post := Post{
			ID:       id,
			Site:     "xhamster",
			Video:    true,
			PageURL:  safe.URLText(href),
			Thumb:    safe.URLText(firstMatch(xhThumbRe, chunk)),
			Duration: safe.Limit(firstMatch(xhDurRe, chunk), 16),
		}
		if label := firstMatch(xhLabelRe, chunk); label != "" {
			post.Title = safe.Text(html.UnescapeString(label))
		}
		posts = append(posts, post)
	}
	return posts
}

func (c *XHamsterClient) SearchPosts(tags string, limit, page int) ([]Post, error) {
	body, err := siteGet(c.http, c.searchURL(tags, page))
	if err != nil {
		return nil, err
	}
	return clampPosts(parseXHamsterSearch(body), limit), nil
}

func (c *XHamsterClient) CountPosts(tags string) (int, error) {
	body, err := siteGet(c.http, c.searchURL(tags, 0))
	if err != nil {
		return 0, err
	}
	raw := firstMatch(xhCountRe, string(body))
	if raw == "" {
		return 0, fmt.Errorf("xhamster did not report a result count")
	}
	return digitsOnly(raw), nil
}

var (
	xhHLSRe    = regexp.MustCompile(`<link rel="preload" href="(https://[^"]+\.m3u8)"`)
	xhSourceRe = regexp.MustCompile(`"(?:hls|standard)":\{"[^"]+":\{"url":"(https?://[^"]+)"`)
)

func (c *XHamsterClient) Media(p Post) (Stream, error) {
	if p.PageURL == "" {
		return Stream{}, fmt.Errorf("xhamster post %d has no page url", p.ID)
	}
	body, err := siteGet(c.http, p.PageURL)
	if err != nil {
		return Stream{}, err
	}
	streamURL := xhamsterStreamURL(string(body))
	if streamURL == "" {
		return Stream{}, fmt.Errorf("xhamster post %d has no playable stream", p.ID)
	}
	return Stream{URL: streamURL, Referer: p.PageURL}, nil
}

func xhamsterStreamURL(page string) string {
	streamURL := firstMatch(xhHLSRe, page)
	if streamURL == "" {
		streamURL = firstMatch(xhSourceRe, page)
	}
	if streamURL == "" {
		return ""
	}
	streamURL = strings.ReplaceAll(streamURL, `\/`, `/`)
	return strings.Replace(streamURL, ".av1.mp4.m3u8", ".h264.mp4.m3u8", 1)
}
