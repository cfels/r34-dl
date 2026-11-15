package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"moxiu/r34-dl/safe"
)

const (
	e621BaseURL    = "https://e621.net"
	e621MaxLimit   = 320
	e621MaxPage    = 750
	e621RequestGap = 500 * time.Millisecond
	e621UserAgent  = "r34-dl/1.0 (https://github.com/cfels/r34-dl)"
)

// E621Client talks to the e621 API, which wants a login plus an account API key.
type E621Client struct {
	http   *http.Client
	login  string
	apiKey string

	// e621 allows two requests per second and asks clients to stay well below it,
	// so every request is spaced out and serialized here.
	mu   sync.Mutex
	last time.Time
}

func NewE621Client(login, apiKey string) *E621Client {
	return &E621Client{
		http:   newHTTPClient(),
		login:  strings.TrimSpace(login),
		apiKey: strings.TrimSpace(apiKey),
	}
}

func (c *E621Client) Name() string { return "e621" }

func (c *E621Client) authenticated() bool {
	return c.login != "" && c.apiKey != ""
}

func (c *E621Client) userAgent() string {
	if login := safe.Tag(c.login); login != "" {
		return fmt.Sprintf("r34-dl/1.0 (by %s on e621)", login)
	}
	return e621UserAgent
}

func (c *E621Client) wait() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gap := e621RequestGap - time.Since(c.last); gap > 0 {
		time.Sleep(gap)
	}
	c.last = time.Now()
}

func (c *E621Client) get(endpoint string, q url.Values) ([]byte, error) {
	c.wait()
	rawURL := e621BaseURL + endpoint
	if encoded := q.Encode(); encoded != "" {
		rawURL += "?" + encoded
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, requestError("couldn't build request", err)
	}
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Accept", "application/json")
	if c.authenticated() {
		req.SetBasicAuth(c.login, c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, requestError("request failed", err)
	}
	defer resp.Body.Close()
	body, err := readLimited(resp.Body)
	if err != nil {
		return nil, requestError("couldn't read response", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, e621StatusError(resp.StatusCode, body)
	}
	return body, nil
}

func e621StatusError(status int, body []byte) error {
	reason := e621Reason(body)
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("e621 auth err: login or api key rejected | run: r34-dl -e621k")
	case http.StatusForbidden:
		if reason == "" {
			reason = "the request was blocked"
		}
		return fmt.Errorf("e621 refused the request: %s", reason)
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return fmt.Errorf("e621 is rate limiting, slow down and retry")
	}
	if reason != "" {
		return fmt.Errorf("e621 returned %d: %s", status, reason)
	}
	return fmt.Errorf("e621 returned %d", status)
}

func e621Reason(body []byte) string {
	var payload struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return ""
	}
	if payload.Reason != "" {
		return safe.Text(payload.Reason)
	}
	return safe.Text(payload.Message)
}

func (c *E621Client) Ping() error {
	q := url.Values{}
	q.Set("limit", "1")
	_, err := c.get("/posts.json", q)
	return err
}

func (c *E621Client) CountPosts(tags string) (int, error) {
	q := url.Values{}
	if tags = strings.TrimSpace(tags); tags != "" {
		q.Set("tags", tags)
	}
	body, err := c.get("/posts/count.json", q)
	if err != nil {
		return 0, err
	}
	return parseE621Count(body)
}

func parseE621Count(body []byte) (int, error) {
	var payload struct {
		Count FlexInt `json:"count"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, fmt.Errorf("couldn't parse response: %w", err)
	}
	return payload.Count.Int(), nil
}

func (c *E621Client) SearchPosts(tags string, limit, page int) ([]Post, error) {
	if page < 0 {
		page = 0
	}
	if page >= e621MaxPage {
		return []Post{}, nil
	}
	if limit <= 0 || limit > e621MaxLimit {
		limit = e621MaxLimit
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", strconv.Itoa(page+1))
	if tags = strings.TrimSpace(tags); tags != "" {
		q.Set("tags", tags)
	}
	body, err := c.get("/posts.json", q)
	if err != nil {
		return nil, err
	}
	return parseE621Posts(body)
}

func (c *E621Client) Autocomplete(prefix string) ([]string, error) {
	prefix = strings.TrimSpace(safe.Tag(prefix))
	if prefix == "" {
		return nil, nil
	}
	q := url.Values{}
	q.Set("search[name_matches]", prefix+"*")
	q.Set("search[order]", "count")
	q.Set("limit", strconv.Itoa(maxPredictions+2))
	body, err := c.get("/tags.json", q)
	if err != nil {
		return nil, err
	}
	return parseE621TagNames(body)
}

type e621File struct {
	URL    string `json:"url"`
	Ext    string `json:"ext"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type e621Preview struct {
	URL string `json:"url"`
}

type e621Sample struct {
	Has    bool   `json:"has"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type e621Score struct {
	Total FlexInt `json:"total"`
}

// e621 groups tags by category; the rest of the app wants one flat tag string.
type e621Tags struct {
	General     []string `json:"general"`
	Artist      []string `json:"artist"`
	Contributor []string `json:"contributor"`
	Copyright   []string `json:"copyright"`
	Character   []string `json:"character"`
	Species     []string `json:"species"`
	Invalid     []string `json:"invalid"`
	Meta        []string `json:"meta"`
	Lore        []string `json:"lore"`
}

func (t e621Tags) list() []string {
	groups := [][]string{
		t.General, t.Species, t.Character, t.Copyright,
		t.Artist, t.Meta, t.Lore, t.Contributor, t.Invalid,
	}
	tags := make([]string, 0, len(t.General)+len(t.Artist)+len(t.Character)+len(t.Species))
	for _, group := range groups {
		for _, tag := range group {
			if tag = strings.TrimSpace(safe.Tag(tag)); tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

type e621Post struct {
	ID      int         `json:"id"`
	File    e621File    `json:"file"`
	Preview e621Preview `json:"preview"`
	Sample  e621Sample  `json:"sample"`
	Score   e621Score   `json:"score"`
	Tags    e621Tags    `json:"tags"`
}

func (p e621Post) post() Post {
	width, height := p.File.Width, p.File.Height
	if width <= 0 || height <= 0 {
		width, height = p.Sample.Width, p.Sample.Height
	}
	sampleURL := ""
	if p.Sample.Has {
		sampleURL = p.Sample.URL
	}
	return sanitizePost(Post{
		ID:        p.ID,
		Tags:      strings.Join(p.Tags.list(), " "),
		Image:     mediaFileName(p.File.URL),
		Width:     width,
		Height:    height,
		Score:     p.Score.Total,
		SampleURL: sampleURL,
		FileURL_:  p.File.URL,
		Thumb:     p.Preview.URL,
		Site:      "e621",
		PageURL:   fmt.Sprintf("%s/posts/%d", e621BaseURL, p.ID),
	})
}

func mediaFileName(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Path == "" {
		return ""
	}
	return path.Base(parsed.Path)
}

func parseE621Posts(body []byte) ([]Post, error) {
	if len(body) == 0 || string(body) == "null" {
		return []Post{}, nil
	}
	var payload struct {
		Posts []e621Post `json:"posts"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("couldn't parse response: %w", err)
	}
	posts := make([]Post, 0, len(payload.Posts))
	for _, raw := range payload.Posts {
		posts = append(posts, raw.post())
	}
	return posts, nil
}

type e621Tag struct {
	Name string `json:"name"`
}

func parseE621TagNames(body []byte) ([]string, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var entries []e621Tag
	if err := json.Unmarshal(body, &entries); err != nil {
		var payload struct {
			Tags []e621Tag `json:"tags"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("couldn't parse response: %w", err)
		}
		entries = payload.Tags
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if name := strings.TrimSpace(safe.Tag(entry.Name)); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}
