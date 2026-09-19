package api

import (
	"strings"
	"testing"
)

func hasControlRunes(s string) bool {
	for _, r := range s {
		if r == 0x1b || r == 0x07 || r == 0x00 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return true
		}
	}
	return false
}

func TestSanitizePostDropsRemoteEscapes(t *testing.T) {
	got := sanitizePost(Post{
		ID:        1,
		Tags:      "safe \x1b]52;c;aGFja2Vk\x07 tags",
		Image:     "\x1b]0;owned\x07x.jpg",
		Directory: FlexString("\x1b[2Jdir"),
		Owner:     "\x1b[31mevil",
		Title:     "esc\x1b]52;c;x\x07",
		Duration:  "\x1b[2J12:34",
		FileURL_:  "https://cdn.example.org/v/1.mp4",
		PageURL:   "https://example.org/page",
		Thumb:     "https://example.org/t.jpg",
	})
	for name, value := range map[string]string{
		"tags":      got.Tags,
		"image":     got.Image,
		"directory": got.Directory.String(),
		"owner":     got.Owner,
		"title":     got.Title,
		"duration":  got.Duration,
		"file_url":  got.FileURL_,
		"page_url":  got.PageURL,
		"thumb":     got.Thumb,
	} {
		if hasControlRunes(value) {
			t.Errorf("%s = %q, still holds control runes", name, value)
		}
	}
	if got.FileURL_ != "https://cdn.example.org/v/1.mp4" {
		t.Errorf("file_url = %q, want the https url kept", got.FileURL_)
	}
}

func TestSanitizePostDropsUnsafeSchemes(t *testing.T) {
	for _, raw := range []string{
		"concat:/etc/passwd",
		"file:///etc/passwd",
		"subfile,,start,0,end,0,,:/etc/passwd",
		"data:text/plain;base64,aGk=",
	} {
		got := sanitizePost(Post{ID: 2, Image: "x.mp4", FileURL_: raw, Thumb: raw, PageURL: raw})
		if got.FileURL_ != "" || got.Thumb != "" || got.PageURL != "" {
			t.Errorf("sanitizePost(%q) kept file_url=%q thumb=%q page=%q", raw, got.FileURL_, got.Thumb, got.PageURL)
		}
		if url := got.FileURL(); strings.HasPrefix(url, "concat") || strings.HasPrefix(url, "file:") {
			t.Errorf("post %d still resolves to %q", got.ID, url)
		}
	}
}

func TestMediaHostAllowed(t *testing.T) {
	cases := []struct {
		site string
		url  string
		want bool
	}{
		{"pornhub", "https://ev-h.phncdn.com/v/1.mp4", true},
		{"pornhub", "https://www.pornhub.com/x", true},
		{"pornhub", "https://evil.example/x", false},
		{"pornhub", "https://phncdn.com.evil.example/x", false},
		{"xvideos", "https://cdn.xvideos-cdn.com/x", true},
		{"xhamster", "https://xhcdn.com/x", true},
		{"safebooru", "https://safebooru.org/x", false},
		{"pornhub", "file:///etc/passwd", false},
		{"", "https://www.pornhub.com/x", false},
	}
	for _, c := range cases {
		if got := MediaHostAllowed(c.site, c.url); got != c.want {
			t.Errorf("MediaHostAllowed(%q, %q) = %v, want %v", c.site, c.url, got, c.want)
		}
	}
}

func TestStreamForWithholdsCookieOffSite(t *testing.T) {
	onSite := streamFor("pornhub", "https://ev-h.phncdn.com/v/1.mp4", "session=abc", "https://www.pornhub.com/view_video.php?viewkey=1")
	if onSite.Cookie != "session=abc" {
		t.Errorf("same-site stream lost its cookie: %+v", onSite)
	}
	offSite := streamFor("pornhub", "https://evil.example/steal", "session=abc", "https://www.pornhub.com/view_video.php?viewkey=1")
	if offSite.Cookie != "" {
		t.Errorf("off-site stream kept the cookie: %+v", offSite)
	}
	if offSite.Referer == "" {
		t.Error("referer should still be forwarded")
	}
}

func TestVideoSiteParsersStripEscapes(t *testing.T) {
	page := `<div id="video_123" data-id="123" title="esc&#27;]52;c;x&#7;ape" href="/video.abc">` +
		`<span class="duration">12:34</span></div>`
	posts := parseXVideosSearch([]byte(page))
	if len(posts) != 1 {
		t.Fatalf("parseXVideosSearch returned %d posts, want 1", len(posts))
	}
	if hasControlRunes(posts[0].Title) || hasControlRunes(posts[0].Duration) {
		t.Errorf("xvideos parser leaked control runes: %+v", posts[0])
	}
	if posts[0].Title == "" || !strings.Contains(posts[0].Title, "esc") {
		t.Errorf("title = %q, want readable text", posts[0].Title)
	}
}

func TestPredictionFieldsStripEscapes(t *testing.T) {
	body := []byte(`{"0":"\u001b]52;c;x\u0007cat_girl","tags":[{"value":"\u001b[31mdog"}]}`)
	for _, tag := range suggestionFields(body) {
		if hasControlRunes(tag) {
			t.Errorf("suggestionFields leaked %q", tag)
		}
	}
	got, err := parseAutocomplete([]byte(`[{"value":"\u001b]52;c;x\u0007cat_girl"}]`))
	if err != nil {
		t.Fatalf("parseAutocomplete: %v", err)
	}
	if len(got) != 1 || hasControlRunes(got[0]) {
		t.Errorf("parseAutocomplete = %q, want sanitised tag", got)
	}
	matched := matchPredictions([]string{"\x1b]52;c;x\x07cat"}, "cat", 5)
	for _, tag := range matched {
		if hasControlRunes(tag) {
			t.Errorf("matchPredictions leaked %q", tag)
		}
	}
}
