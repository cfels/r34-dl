package api

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Client interface {
	SearchPosts(tags string, limit, page int) ([]Post, error)
	CountPosts(tags string) (int, error)
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
	FileURL_  string     `json:"-"`
}

func (p Post) FileURL() string {
	if p.FileURL_ != "" {
		return p.FileURL_
	}
	return fmt.Sprintf("https://safebooru.org/images/%s/%s", p.Directory.String(), p.Image)
}

type postCountXML struct {
	XMLName xml.Name `xml:"posts"`
	Count   int      `xml:"count,attr"`
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

type SafebooruClient struct {
	http *http.Client
}

func NewSafebooruClient() *SafebooruClient {
	return &SafebooruClient{http: newHTTPClient()}
}

func (c *SafebooruClient) Name() string { return "safebooru" }

func (c *SafebooruClient) CountPosts(tags string) (int, error) {
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
		return 0, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", "r34-dl/safebooru-client")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("reading body: %w", err)
	}
	var pc postCountXML
	if err := xml.Unmarshal(body, &pc); err != nil {
		return 0, fmt.Errorf("parsing XML: %w", err)
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
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", "r34-dl/safebooru-client")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}
	if len(body) == 0 || string(body) == "null" {
		return []Post{}, nil
	}
	var posts []Post
	if err := json.Unmarshal(body, &posts); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
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
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", "r34-dl/rule34-client")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading body: %w", err)
	}
	if len(body) > 0 && body[0] == '"' {
		var msg string
		_ = json.Unmarshal(body, &msg)
		return fmt.Errorf("API error: %s", msg)
	}
	return nil
}

func (c *Rule34Client) CountPosts(tags string) (int, error) {
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
		return 0, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", "r34-dl/rule34-client")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("reading body: %w", err)
	}
	if len(body) > 0 && body[0] == '"' {
		var msg string
		_ = json.Unmarshal(body, &msg)
		return 0, fmt.Errorf("rule34 API error: %s", msg)
	}
	var pc postCountXML
	if err := xml.Unmarshal(body, &pc); err != nil {
		return 0, fmt.Errorf("parsing XML: %w", err)
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
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", "r34-dl/rule34-client")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}
	if len(body) == 0 || string(body) == "null" || string(body) == "[]" {
		return []Post{}, nil
	}
	if len(body) > 0 && body[0] == '"' {
		var msg string
		_ = json.Unmarshal(body, &msg)
		return nil, fmt.Errorf("rule34 API error: %s", msg)
	}
	var raw []r34Post
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	posts := make([]Post, 0, len(raw))
	for _, r := range raw {
		p := Post{
			ID:        r.ID,
			Tags:      r.Tags,
			Image:     r.Image,
			Directory: r.Directory,
			Owner:     r.Owner,
			Width:     r.Width,
			Height:    r.Height,
			Sample:    r.Sample,
			Score:     r.Score,
			FileURL_:  r.FileURL,
		}
		posts = append(posts, p)
	}
	return posts, nil
}

func NewClient() Client {
	return NewSafebooruClient()
}
