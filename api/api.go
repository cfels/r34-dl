package api

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"moxiu/r34-dl/safe"
)

const maxAPIBody = 8 << 20

var secretQueryRe = regexp.MustCompile(`(?i)\b(api_key|user_id|token|password|secret|key)=[^&\s"']+`)

type redactedError struct {
	err  error
	text string
}

func (e redactedError) Error() string { return e.text }
func (e redactedError) Unwrap() error { return e.err }

func redactSecrets(s string) string {
	if !strings.Contains(s, "=") {
		return s
	}
	return secretQueryRe.ReplaceAllString(s, "$1=***")
}

func requestError(prefix string, err error) error {
	return redactedError{err: err, text: fmt.Sprintf("%s: %s", prefix, redactSecrets(err.Error()))}
}

type Client interface {
	SearchPosts(tags string, limit, page int) ([]Post, error)
	CountPosts(tags string) (int, error)
	Autocomplete(prefix string) ([]string, error)
	Name() string
}

type FlexString string

func (f *FlexString) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = FlexString(s)
		return nil
	}
	*f = FlexString(string(data))
	return nil
}

func (f FlexString) String() string {
	return string(f)
}

type FlexInt int

func (f *FlexInt) UnmarshalJSON(data []byte) error {
	s := string(data)
	switch s {
	case "true":
		*f = 1
		return nil
	case "false", "null":
		*f = 0
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		n, err := strconv.Atoi(str)
		if err != nil {
			return fmt.Errorf("FlexInt: cannot parse %q: %w", str, err)
		}
		*f = FlexInt(n)
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*f = FlexInt(n)
	return nil
}

func (f FlexInt) Int() int {
	return int(f)
}

type Post struct {
	ID        int        `json:"id"`
	Tags      string     `json:"tags"`
	Image     string     `json:"image"`
	Directory FlexString `json:"directory"`
	Owner     string     `json:"owner"`
	Width     int        `json:"width"`
	Height    int        `json:"height"`
	Sample    FlexInt    `json:"sample"`
	Score     FlexInt    `json:"score,omitempty"`
	SampleURL string     `json:"sample_url"`
	FileURL_  string     `json:"-"`

	Site     string `json:"-"`
	PageURL  string `json:"-"`
	Thumb    string `json:"-"`
	Title    string `json:"-"`
	Duration string `json:"-"`
	Video    bool   `json:"-"`
}

func (p Post) FileURL() string {
	if p.FileURL_ != "" {
		return p.FileURL_
	}
	if p.Thumb != "" {
		return p.Thumb
	}
	return fmt.Sprintf("https://safebooru.org/images/%s/%s", p.Directory.String(), p.Image)
}

func (p Post) PreviewURL() string {
	if p.SampleURL != "" {
		return p.SampleURL
	}
	return p.FileURL()
}

type postCountXML struct {
	XMLName xml.Name `xml:"posts"`
	Count   int      `xml:"count,attr"`
}

type tagCountXML struct {
	XMLName xml.Name `xml:"tags"`
	Tags    []struct {
		Name  string `xml:"name,attr"`
		Count int    `xml:"count,attr"`
	} `xml:"tag"`
}

func simpleTagQuery(tags string) string {
	tag := strings.TrimSpace(tags)
	if tag == "" || strings.HasPrefix(tag, "-") {
		return ""
	}
	if strings.ContainsAny(tag, " \t*?%") {
		return ""
	}
	return tag
}

func parseTagCount(body []byte, tag string) (int, bool) {
	var payload tagCountXML
	if err := xml.Unmarshal(body, &payload); err != nil {
		return 0, false
	}
	for _, entry := range payload.Tags {
		if strings.EqualFold(entry.Name, tag) {
			return entry.Count, true
		}
	}
	return 0, false
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

func readLimited(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxAPIBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAPIBody {
		return nil, fmt.Errorf("response exceeded %d MiB", maxAPIBody>>20)
	}
	return data, nil
}

func sanitizePost(p Post) Post {
	p.Tags = safe.List(p.Tags)
	p.Image = safe.URLText(p.Image)
	p.Directory = FlexString(safe.Tag(p.Directory.String()))
	p.Owner = safe.Tag(p.Owner)
	p.FileURL_ = safe.URLText(p.FileURL_)
	p.SampleURL = safe.URLText(p.SampleURL)
	p.PageURL = safe.URLText(p.PageURL)
	p.Thumb = safe.URLText(p.Thumb)
	p.Title = safe.Text(p.Title)
	p.Duration = safe.Limit(p.Duration, 16)
	if p.FileURL_ != "" && !safe.MediaURL(p.FileURL_) {
		p.FileURL_ = ""
	}
	if p.SampleURL != "" && !safe.MediaURL(p.SampleURL) {
		p.SampleURL = ""
	}
	if p.Thumb != "" && !safe.MediaURL(p.Thumb) {
		p.Thumb = ""
	}
	if p.PageURL != "" && !safe.MediaURL(p.PageURL) {
		p.PageURL = ""
	}
	return p
}

type autoEntry struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

func fetchAutocomplete(client *http.Client, endpoint, prefix, userAgent string) ([]string, error) {
	q := url.Values{}
	q.Set("q", prefix)
	req, err := http.NewRequest(http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("couldn't build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status %d", resp.StatusCode)
	}
	body, err := readLimited(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("couldn't read response: %w", err)
	}
	return parseAutocomplete(body)
}

func parseAutocomplete(body []byte) ([]string, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var entries []autoEntry
	if err := json.Unmarshal(body, &entries); err == nil {
		tags := make([]string, 0, len(entries))
		for _, e := range entries {
			tag := safe.Tag(e.Value)
			if tag == "" {
				tag = stripTagCount(e.Label)
			}
			if tag != "" {
				tags = append(tags, tag)
			}
		}
		return tags, nil
	}
	var plain []string
	if err := json.Unmarshal(body, &plain); err == nil {
		tags := make([]string, 0, len(plain))
		for _, tag := range plain {
			if tag = safe.Tag(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
		return tags, nil
	}
	return nil, fmt.Errorf("couldn't parse response")
}

func stripTagCount(label string) string {
	label = safe.Tag(label)
	open := strings.LastIndex(label, " (")
	if open < 0 || !strings.HasSuffix(label, ")") {
		return label
	}
	count := label[open+2 : len(label)-1]
	for _, r := range count {
		if r < '0' || r > '9' {
			return label
		}
	}
	return strings.TrimSpace(label[:open])
}

const maxPredictions = 8

func matchPredictions(candidates []string, prefix string, max int) []string {
	prefix = strings.ToLower(safe.Tag(prefix))
	if prefix == "" || max <= 0 {
		return nil
	}
	seen := make(map[string]bool, len(candidates))
	out := make([]string, 0, max)
	for _, candidate := range candidates {
		candidate = strings.Join(strings.Fields(safe.Tag(candidate)), " ")
		key := strings.ToLower(candidate)
		if candidate == "" || seen[key] {
			continue
		}
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		seen[key] = true
		out = append(out, candidate)
		if len(out) == max {
			break
		}
	}
	return out
}

func suggestionFields(body []byte) []string {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	ordered := make([]string, 0, len(payload))
	for key := range payload {
		if _, err := strconv.Atoi(key); err == nil {
			ordered = append(ordered, key)
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, _ := strconv.Atoi(ordered[i])
		right, _ := strconv.Atoi(ordered[j])
		return left < right
	})
	out := make([]string, 0, len(ordered))
	for _, key := range ordered {
		var value string
		if err := json.Unmarshal(payload[key], &value); err == nil {
			if value = safe.Tag(value); value != "" {
				out = append(out, value)
			}
		}
	}
	rest := make([]string, 0, len(payload))
	for key := range payload {
		if _, err := strconv.Atoi(key); err == nil {
			continue
		}
		rest = append(rest, key)
	}
	sort.Strings(rest)
	for _, key := range rest {
		for _, value := range rawStrings(payload[key]) {
			if value = safe.Tag(value); value != "" {
				out = append(out, value)
			}
		}
	}
	return out
}

func rawStrings(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	var objects []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objects); err != nil {
		return nil
	}
	out := make([]string, 0, len(objects))
	for _, object := range objects {
		for _, field := range []string{"value", "name", "text", "title", "username"} {
			var value string
			if err := json.Unmarshal(object[field], &value); err == nil && value != "" {
				out = append(out, value)
				break
			}
		}
	}
	return out
}

type SafebooruClient struct {
	http *http.Client
}

func NewSafebooruClient() *SafebooruClient {
	return &SafebooruClient{http: newHTTPClient()}
}

func (c *SafebooruClient) Name() string { return "safebooru" }

func (c *SafebooruClient) Autocomplete(prefix string) ([]string, error) {
	return fetchAutocomplete(c.http, "https://safebooru.org/autocomplete.php", prefix, "r34-dl/safebooru-client")
}

func (c *SafebooruClient) CountPosts(tags string) (int, error) {
	if tag := simpleTagQuery(tags); tag != "" {
		if count, err := c.tagCount(tag); err == nil {
			return count, nil
		}
	}
	return c.postCount(tags)
}

func (c *SafebooruClient) tagCount(tag string) (int, error) {
	const base = "https://safebooru.org/index.php"
	q := url.Values{}
	q.Set("page", "dapi")
	q.Set("s", "tag")
	q.Set("q", "index")
	q.Set("name", tag)
	req, err := http.NewRequest(http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return 0, fmt.Errorf("couldn't build request: %w", err)
	}
	req.Header.Set("User-Agent", "r34-dl/safebooru-client")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bad status %d", resp.StatusCode)
	}
	body, err := readLimited(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("couldn't read response: %w", err)
	}
	count, ok := parseTagCount(body, tag)
	if !ok {
		return 0, fmt.Errorf("safebooru did not report a post count for %q", tag)
	}
	return count, nil
}

func (c *SafebooruClient) postCount(tags string) (int, error) {
	const base = "https://safebooru.org/index.php"
	q := url.Values{}
	q.Set("page", "dapi")
	q.Set("s", "post")
	q.Set("q", "index")
	q.Set("limit", "1")
	if tags != "" {
		q.Set("tags", tags)
	}
	req, err := http.NewRequest(http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return 0, fmt.Errorf("couldn't build request: %w", err)
	}
	req.Header.Set("User-Agent", "r34-dl/safebooru-client")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bad status %d", resp.StatusCode)
	}
	body, err := readLimited(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("couldn't read response: %w", err)
	}
	var pc postCountXML
	if err := xml.Unmarshal(body, &pc); err != nil {
		return 0, fmt.Errorf("couldn't parse response: %w", err)
	}
	return pc.Count, nil
}

func (c *SafebooruClient) SearchPosts(tags string, limit, page int) ([]Post, error) {
	const base = "https://safebooru.org/index.php"
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := url.Values{}
	q.Set("page", "dapi")
	q.Set("s", "post")
	q.Set("q", "index")
	q.Set("json", "1")
	q.Set("limit", fmt.Sprintf("%d", limit))
	q.Set("pid", fmt.Sprintf("%d", page))
	if tags != "" {
		q.Set("tags", tags)
	}
	req, err := http.NewRequest(http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("couldn't build request: %w", err)
	}
	req.Header.Set("User-Agent", "r34-dl/safebooru-client")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status %d", resp.StatusCode)
	}
	body, err := readLimited(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("couldn't read response: %w", err)
	}
	if len(body) == 0 || string(body) == "null" {
		return []Post{}, nil
	}
	var posts []Post
	if err := json.Unmarshal(body, &posts); err != nil {
		return nil, fmt.Errorf("couldn't parse response: %w", err)
	}
	for i := range posts {
		posts[i] = sanitizePost(posts[i])
	}
	return posts, nil
}

type Rule34Client struct {
	http   *http.Client
	apiKey string
	userID string
}

func NewRule34Client(userID, apiKey string) *Rule34Client {
	return &Rule34Client{
		http:   newHTTPClient(),
		apiKey: apiKey,
		userID: userID,
	}
}

func (c *Rule34Client) Name() string { return "rule34" }

func (c *Rule34Client) Autocomplete(prefix string) ([]string, error) {
	return fetchAutocomplete(c.http, "https://api.rule34.xxx/autocomplete.php", prefix, "r34-dl/rule34-client")
}

type r34Post struct {
	ID        int        `json:"id"`
	Tags      string     `json:"tags"`
	Image     string     `json:"image"`
	Directory FlexString `json:"directory"`
	Owner     string     `json:"owner"`
	Width     int        `json:"width"`
	Height    int        `json:"height"`
	Sample    FlexInt    `json:"sample"`
	Score     FlexInt    `json:"score"`
	SampleURL string     `json:"sample_url"`
	FileURL   string     `json:"file_url"`
}

func (c *Rule34Client) Ping() error {
	const base = "https://api.rule34.xxx/index.php"
	q := url.Values{}
	q.Set("page", "dapi")
	q.Set("s", "post")
	q.Set("q", "index")
	q.Set("limit", "1")
	q.Set("user_id", c.userID)
	q.Set("api_key", c.apiKey)
	req, err := http.NewRequest(http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return requestError("couldn't build request", err)
	}
	req.Header.Set("User-Agent", "r34-dl/rule34-client")
	resp, err := c.http.Do(req)
	if err != nil {
		return requestError("request failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status %d", resp.StatusCode)
	}
	body, err := readLimited(resp.Body)
	if err != nil {
		return requestError("couldn't read response", err)
	}
	if len(body) > 0 && body[0] == '"' {
		var msg string
		_ = json.Unmarshal(body, &msg)
		if strings.Contains(msg, "Missing authentication") || strings.Contains(msg, "authentication") {
			return fmt.Errorf("r34 auth err: missing api key! | run: r34-dl --add-api-key")
		}
		return fmt.Errorf("r34 err: %s", msg)
	}
	return nil
}

func (c *Rule34Client) CountPosts(tags string) (int, error) {
	if tag := simpleTagQuery(tags); tag != "" {
		if count, err := c.tagCount(tag); err == nil {
			return count, nil
		}
	}
	return c.postCount(tags)
}

func (c *Rule34Client) tagCount(tag string) (int, error) {
	const base = "https://api.rule34.xxx/index.php"
	q := url.Values{}
	q.Set("page", "dapi")
	q.Set("s", "tag")
	q.Set("q", "index")
	q.Set("name", tag)
	if c.userID != "" && c.apiKey != "" {
		q.Set("user_id", c.userID)
		q.Set("api_key", c.apiKey)
	}
	req, err := http.NewRequest(http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return 0, requestError("couldn't build request", err)
	}
	req.Header.Set("User-Agent", "r34-dl/rule34-client")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, requestError("request failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bad status %d", resp.StatusCode)
	}
	body, err := readLimited(resp.Body)
	if err != nil {
		return 0, requestError("couldn't read response", err)
	}
	count, ok := parseTagCount(body, tag)
	if !ok {
		return 0, fmt.Errorf("rule34 did not report a post count for %q", tag)
	}
	return count, nil
}

func (c *Rule34Client) postCount(tags string) (int, error) {
	const base = "https://api.rule34.xxx/index.php"
	q := url.Values{}
	q.Set("page", "dapi")
	q.Set("s", "post")
	q.Set("q", "index")
	q.Set("limit", "1")
	if tags != "" {
		q.Set("tags", tags)
	}
	if c.userID != "" && c.apiKey != "" {
		q.Set("user_id", c.userID)
		q.Set("api_key", c.apiKey)
	}
	req, err := http.NewRequest(http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return 0, requestError("couldn't build request", err)
	}
	req.Header.Set("User-Agent", "r34-dl/rule34-client")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, requestError("request failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bad status %d", resp.StatusCode)
	}
	body, err := readLimited(resp.Body)
	if err != nil {
		return 0, requestError("couldn't read response", err)
	}
	if len(body) > 0 && body[0] == '"' {
		var msg string
		_ = json.Unmarshal(body, &msg)
		if strings.Contains(msg, "Missing authentication") || strings.Contains(msg, "authentication") {
			return 0, fmt.Errorf("r34 auth err: missing api key! | run: r34-dl --add-api-key")
		}
		return 0, fmt.Errorf("r34 err: %s", msg)
	}
	var pc postCountXML
	if err := xml.Unmarshal(body, &pc); err != nil {
		return 0, fmt.Errorf("couldn't parse response: %w", err)
	}
	return pc.Count, nil
}

func (c *Rule34Client) SearchPosts(tags string, limit, page int) ([]Post, error) {
	const base = "https://api.rule34.xxx/index.php"
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := url.Values{}
	q.Set("page", "dapi")
	q.Set("s", "post")
	q.Set("q", "index")
	q.Set("json", "1")
	q.Set("limit", fmt.Sprintf("%d", limit))
	q.Set("pid", fmt.Sprintf("%d", page))
	if tags != "" {
		q.Set("tags", tags)
	}
	if c.userID != "" && c.apiKey != "" {
		q.Set("user_id", c.userID)
		q.Set("api_key", c.apiKey)
	}
	req, err := http.NewRequest(http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, requestError("couldn't build request", err)
	}
	req.Header.Set("User-Agent", "r34-dl/rule34-client")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, requestError("request failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status %d", resp.StatusCode)
	}
	body, err := readLimited(resp.Body)
	if err != nil {
		return nil, requestError("couldn't read response", err)
	}
	if len(body) == 0 || string(body) == "null" || string(body) == "[]" {
		return []Post{}, nil
	}
	if len(body) > 0 && body[0] == '"' {
		var msg string
		_ = json.Unmarshal(body, &msg)
		if strings.Contains(msg, "Missing authentication") || strings.Contains(msg, "authentication") {
			return nil, fmt.Errorf("r34 auth err: missing api key! | run: r34-dl -apik")
		}
		return nil, fmt.Errorf("r34 err: %s", msg)
	}
	var raw []r34Post
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("couldn't parse response: %w", err)
	}
	posts := make([]Post, 0, len(raw))
	for _, r := range raw {
		p := sanitizePost(Post{
			ID:        r.ID,
			Tags:      r.Tags,
			Image:     r.Image,
			Directory: r.Directory,
			Owner:     r.Owner,
			Width:     r.Width,
			Height:    r.Height,
			Sample:    r.Sample,
			Score:     r.Score,
			SampleURL: r.SampleURL,
			FileURL_:  r.FileURL,
		})
		posts = append(posts, p)
	}
	return posts, nil
}
