package api

import (
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return string(data)
}

func TestParsePornHubSearch(t *testing.T) {
	page := fixture(t, "pornhub_search.html")
	posts := parsePornHubSearch([]byte(page))
	if len(posts) != 2 {
		t.Fatalf("parsed %d posts, want 2", len(posts))
	}
	for _, post := range posts {
		if post.Site != "pornhub" || !post.Video {
			t.Errorf("post %d is not tagged as a pornhub video: %+v", post.ID, post)
		}
		if post.ID == 0 {
			t.Error("post is missing an id")
		}
		if !strings.HasPrefix(post.PageURL, "https://www.pornhub.com/view_video.php?viewkey=") {
			t.Errorf("post %d has unexpected page url %q", post.ID, post.PageURL)
		}
		if post.Title == "" || strings.Contains(post.Title, "&#") {
			t.Errorf("post %d has a raw or empty title %q", post.ID, post.Title)
		}
		if !strings.HasPrefix(post.Thumb, "https://") {
			t.Errorf("post %d has thumbnail %q", post.ID, post.Thumb)
		}
		if post.FileURL() != post.Thumb {
			t.Errorf("post %d should preview its thumbnail, got %q", post.ID, post.FileURL())
		}
		if !strings.Contains(post.Duration, ":") {
			t.Errorf("post %d has duration %q", post.ID, post.Duration)
		}
	}
}

func TestParsePornHubCount(t *testing.T) {
	page := fixture(t, "pornhub_search.html")
	raw := firstMatch(phCountRe, page)
	if raw == "" {
		t.Fatal("pornhub fixture has no result count")
	}
	if digitsOnly(raw) == 0 {
		t.Errorf("count %q did not parse", raw)
	}
}

func TestSuggestionFieldsReadsPornHubPayload(t *testing.T) {
	payload := []byte(`{"0":"Big Ass","1":"Big Boobs","isDdBannedWord":"false",` +
		`"popularSearches":["sex hd","busty"],"tags":[{"name":"Big Tits"},{"value":"Big Dick"}]}`)
	got := matchPredictions(suggestionFields(payload), "big", maxPredictions)
	want := []string{"Big Ass", "Big Boobs", "Big Tits", "Big Dick"}
	if len(got) != len(want) {
		t.Fatalf("predictions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("predictions = %v, want %v", got, want)
		}
	}
}

func TestParseXVideosTags(t *testing.T) {
	page := `<a href="/tags/big-ass">Big Ass</a><a href="/tags/big-tits">Big Tits</a>` +
		`<a href="/tags/big-ass">dup</a><a href="/channels/x">chan</a><a href="/tags/09">09</a>`
	got := matchPredictions(parseXVideosTags([]byte(page)), "big", maxPredictions)
	want := []string{"big ass", "big tits"}
	if len(got) != len(want) {
		t.Fatalf("predictions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("predictions = %v, want %v", got, want)
		}
	}
}

func TestParseXHamsterSuggestions(t *testing.T) {
	page := `<script>window.__data={"search":{"searchVideoSuggestions":{"tags":[` +
		`{"modelName":"suggestModel","text":"Big Ass","plainText":"Big Ass"},` +
		`{"modelName":"suggestModel","text":"Big &amp; Bold","plainText":"Big & Bold"}]},` +
		`"other":{"tags":[{"text":"ignored"}]}}};</script>`
	got := matchPredictions(parseXHamsterSuggestions([]byte(page)), "big", maxPredictions)
	want := []string{"Big Ass", "Big & Bold"}
	if len(got) != len(want) {
		t.Fatalf("predictions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("predictions = %v, want %v", got, want)
		}
	}

	if tags := parseXHamsterSuggestions([]byte("<html>nothing here</html>")); tags != nil {
		t.Errorf("page without suggestions returned %v", tags)
	}
}

func TestMatchPredictionsKeepsPrefixAndDropsDuplicates(t *testing.T) {
	got := matchPredictions([]string{"Big Ass", " big   ass ", "big tits", "spanking", ""}, "BIG", 8)
	want := []string{"Big Ass", "big tits"}
	if len(got) != len(want) {
		t.Fatalf("predictions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("predictions = %v, want %v", got, want)
		}
	}
	if got := matchPredictions([]string{"x"}, "", 8); got != nil {
		t.Errorf("empty prefix returned %v", got)
	}
	if got := matchPredictions([]string{"big tits"}, "big", 1); len(got) != 1 {
		t.Errorf("limit was ignored: %v", got)
	}
}

func TestParsePornHubMediaPicksBestStream(t *testing.T) {
	streams, err := parsePornHubMedia([]byte(fixture(t, "pornhub_video.html")))
	if err != nil {
		t.Fatalf("parse media: %v", err)
	}
	if len(streams) < 2 {
		t.Fatalf("parsed %d streams, want several qualities", len(streams))
	}
	media := streams[0]
	if media.Format != "hls" {
		t.Errorf("format = %q, want hls", media.Format)
	}
	if !strings.Contains(media.VideoURL, "master.m3u8") {
		t.Errorf("video url = %q", media.VideoURL)
	}
	if strings.Contains(media.VideoURL, `\/`) {
		t.Errorf("video url still has json escapes: %q", media.VideoURL)
	}
	if media.Height < 1080 {
		t.Errorf("first stream height = %d, want the best quality first", media.Height)
	}
	for i := 1; i < len(streams); i++ {
		if streams[i].Height > streams[i-1].Height {
			t.Errorf("streams are not sorted best first: %d then %d", streams[i-1].Height, streams[i].Height)
		}
	}
}

func TestParseXVideosSearch(t *testing.T) {
	page := fixture(t, "xvideos_search.html")
	posts := parseXVideosSearch([]byte(page))
	if len(posts) != 2 {
		t.Fatalf("parsed %d posts, want 2", len(posts))
	}
	for _, post := range posts {
		if post.Site != "xvideos" || !post.Video {
			t.Errorf("post %d is not tagged as an xvideos video: %+v", post.ID, post)
		}
		if !strings.HasPrefix(post.PageURL, "https://www.xvideos.com/video.") {
			t.Errorf("post %d has unexpected page url %q", post.ID, post.PageURL)
		}
		if !strings.HasPrefix(post.Thumb, "https://") {
			t.Errorf("post %d has thumbnail %q", post.ID, post.Thumb)
		}
		if post.Title == "" || post.Duration == "" {
			t.Errorf("post %d is missing title %q or duration %q", post.ID, post.Title, post.Duration)
		}
	}
}

func TestParseXVideosCount(t *testing.T) {
	page := fixture(t, "xvideos_search.html")
	raw := firstMatch(xvCountRe, page)
	if raw == "" {
		t.Fatal("xvideos fixture has no result count")
	}
	if digitsOnly(strings.ReplaceAll(raw, ",", "")) < 2 {
		t.Errorf("count %q did not parse", raw)
	}
}

func TestXVideosStreamPrefersHLS(t *testing.T) {
	page := fixture(t, "xvideos_video.html")
	stream := xvideosStreamURL(page)
	if stream == "" {
		t.Fatal("no stream found in the xvideos fixture")
	}
	if !strings.HasSuffix(stream, "hls.m3u8") {
		t.Errorf("stream = %q, want the hls playlist", stream)
	}
}

func TestParseXHamsterSearch(t *testing.T) {
	page := fixture(t, "xhamster_search.html")
	posts := parseXHamsterSearch([]byte(page))
	if len(posts) != 2 {
		t.Fatalf("parsed %d posts, want 2", len(posts))
	}
	for _, post := range posts {
		if post.Site != "xhamster" || !post.Video {
			t.Errorf("post %d is not tagged as an xhamster video: %+v", post.ID, post)
		}
		if !strings.HasPrefix(post.PageURL, "https://xhamster.com/videos/") {
			t.Errorf("post %d has unexpected page url %q", post.ID, post.PageURL)
		}
		if !strings.HasPrefix(post.Thumb, "https://") {
			t.Errorf("post %d has thumbnail %q", post.ID, post.Thumb)
		}
		if post.Title == "" || strings.Contains(post.Title, "&#") {
			t.Errorf("post %d has a raw or empty title %q", post.ID, post.Title)
		}
	}
}

func TestParseXHamsterCount(t *testing.T) {
	page := fixture(t, "xhamster_search.html")
	raw := firstMatch(xhCountRe, page)
	if raw == "" {
		t.Fatal("xhamster fixture has no result count")
	}
	if digitsOnly(raw) == 0 {
		t.Errorf("count %q did not parse", raw)
	}
}

func TestXHamsterStreamPrefersH264(t *testing.T) {
	page := fixture(t, "xhamster_video.html")
	stream := xhamsterStreamURL(page)
	if stream == "" {
		t.Fatal("no stream found in the xhamster fixture")
	}
	if !strings.HasSuffix(stream, "_TPL_.h264.mp4.m3u8") {
		t.Errorf("stream = %q, want the h264 playlist", stream)
	}
	if !strings.Contains(stream, "media=hls4") {
		t.Errorf("stream = %q, want the hls4 playlist", stream)
	}
}

func TestMediaFallsBackToImageURL(t *testing.T) {
	post := Post{ID: 7, Image: "7.jpg", Directory: "123"}
	stream, err := Media(post)
	if err != nil {
		t.Fatalf("media: %v", err)
	}
	if stream.URL != "https://safebooru.org/images/123/7.jpg" {
		t.Errorf("url = %q", stream.URL)
	}
	if stream.IsHLS() {
		t.Error("an image should not look like hls")
	}
}

func TestMediaWithoutStreamerFails(t *testing.T) {
	if _, err := Media(Post{ID: 1, Site: "nowhere", Video: true}); err == nil {
		t.Error("video posts without a site streamer should fail loudly")
	}
}

func TestSiteHelpers(t *testing.T) {
	if len(SiteNames()) != 5 || SiteNames()[0] != "safebooru" {
		t.Errorf("site names = %v", SiteNames())
	}
	if !KnownSite("xhamster") || KnownSite("nope") {
		t.Error("KnownSite is wrong")
	}
	for _, name := range []string{"rule34", "pornhub", "xvideos", "xhamster"} {
		if !IsAdultSite(name) {
			t.Errorf("%s should be age restricted", name)
		}
	}
	if IsAdultSite("safebooru") {
		t.Error("safebooru is safe")
	}
}

func TestStreamerNames(t *testing.T) {
	for _, streamer := range []Streamer{NewPornHubClient(), NewXVideosClient(), NewXHamsterClient()} {
		named, ok := streamer.(Client)
		if !ok {
			t.Fatalf("%T should also be a Client", streamer)
		}
		if !IsAdultSite(named.Name()) {
			t.Errorf("%s should be listed as adult", named.Name())
		}
	}
}
