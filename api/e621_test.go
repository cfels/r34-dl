package api

import (
	"strings"
	"testing"
)

const e621PostsFixture = `{"posts":[` +
	`{"id":6842164,"file":{"width":3840,"height":2160,"ext":"png","size":10235081,` +
	`"url":"https://static1.e621.net/data/a2/22/a222572d66d48214e75eb972d40fc0d5.png"},` +
	`"preview":{"width":455,"height":256,"url":"https://static1.e621.net/data/preview/a2/22/a222572d66d48214e75eb972d40fc0d5.jpg"},` +
	`"sample":{"has":true,"width":1511,"height":850,"url":"https://static1.e621.net/data/sample/a2/22/a222572d66d48214e75eb972d40fc0d5.jpg"},` +
	`"score":{"up":3,"down":0,"total":3},"tags":{"general":["solo","nude"],` +
	`"artist":["zensintemple"],"contributor":[],"copyright":[],"character":["chester_(fortnite)"],` +
	`"species":["domestic_cat"],"invalid":[],"meta":["hi_res"],"lore":[]},` +
	`"rating":"e","duration":null},` +
	`{"id":42,"file":{"width":640,"height":480,"ext":"webm",` +
	`"url":"https://static1.e621.net/data/aa/bb/aabbccdd.webm"},` +
	`"preview":{"width":213,"height":160,"url":"https://static1.e621.net/data/preview/aa/bb/aabbccdd.jpg"},` +
	`"sample":{"has":false,"width":0,"height":0,"url":null},` +
	`"score":{"total":0},"tags":{"general":["animated"],"artist":[],"character":[],"species":[]}}]}`

func TestParseE621Posts(t *testing.T) {
	posts, err := parseE621Posts([]byte(e621PostsFixture))
	if err != nil {
		t.Fatalf("parseE621Posts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("parsed %d posts, want 2", len(posts))
	}

	image := posts[0]
	if image.ID != 6842164 || image.Site != "e621" {
		t.Errorf("post = %+v, want id 6842164 on e621", image)
	}
	if image.FileURL() != "https://static1.e621.net/data/a2/22/a222572d66d48214e75eb972d40fc0d5.png" {
		t.Errorf("file url = %q", image.FileURL())
	}
	if image.PreviewURL() != "https://static1.e621.net/data/sample/a2/22/a222572d66d48214e75eb972d40fc0d5.jpg" {
		t.Errorf("preview url = %q, want the sample", image.PreviewURL())
	}
	if image.Thumb != "https://static1.e621.net/data/preview/a2/22/a222572d66d48214e75eb972d40fc0d5.jpg" {
		t.Errorf("thumb = %q", image.Thumb)
	}
	if image.Image != "a222572d66d48214e75eb972d40fc0d5.png" {
		t.Errorf("image = %q, want the file name so downloads keep their extension", image.Image)
	}
	if image.Width != 3840 || image.Height != 2160 {
		t.Errorf("size = %dx%d", image.Width, image.Height)
	}
	if image.Score.Int() != 3 {
		t.Errorf("score = %d", image.Score.Int())
	}
	if image.PageURL != "https://e621.net/posts/6842164" {
		t.Errorf("page url = %q", image.PageURL)
	}
	want := "solo nude domestic_cat chester_(fortnite) zensintemple hi_res"
	if image.Tags != want {
		t.Errorf("tags = %q, want %q", image.Tags, want)
	}

	video := posts[1]
	if video.Video {
		t.Error("e621 videos should keep their own file extension instead of the generic video path")
	}
	if video.SampleURL != "" || video.PreviewURL() != video.FileURL() {
		t.Errorf("sample = %q, preview = %q, want the file fallback", video.SampleURL, video.PreviewURL())
	}
	if video.Image != "aabbccdd.webm" {
		t.Errorf("image = %q", video.Image)
	}
}

func TestParseE621PostsEdgeCases(t *testing.T) {
	for _, payload := range []string{"", "null"} {
		posts, err := parseE621Posts([]byte(payload))
		if err != nil {
			t.Errorf("parseE621Posts(%q): %v", payload, err)
		}
		if len(posts) != 0 {
			t.Errorf("parseE621Posts(%q) = %v, want no posts", payload, posts)
		}
	}

	if _, err := parseE621Posts([]byte("<html>nope</html>")); err == nil {
		t.Error("malformed payload should report an error")
	}
}

func TestParseE621Count(t *testing.T) {
	count, err := parseE621Count([]byte(`{"count":240001,"capped":true}`))
	if err != nil {
		t.Fatalf("parseE621Count: %v", err)
	}
	if count != 240001 {
		t.Errorf("count = %d, want 240001", count)
	}
	if _, err := parseE621Count([]byte(`{"count":"nope"}`)); err == nil {
		t.Error("bad count should report an error")
	}
}

func TestParseE621TagNames(t *testing.T) {
	payloads := []string{
		`[{"id":660640,"name":"canid","post_count":1742362},{"id":1068,"name":"canine","post_count":1684543}]`,
		`{"tags":[{"id":660640,"name":"canid","post_count":1742362},{"id":1068,"name":"canine","post_count":1684543}]}`,
	}
	for _, payload := range payloads {
		names, err := parseE621TagNames([]byte(payload))
		if err != nil {
			t.Fatalf("parseE621TagNames(%q): %v", payload, err)
		}
		if len(names) != 2 || names[0] != "canid" || names[1] != "canine" {
			t.Errorf("parseE621TagNames(%q) = %v", payload, names)
		}
	}

	for _, payload := range []string{"", "null", "[]"} {
		names, err := parseE621TagNames([]byte(payload))
		if err != nil {
			t.Errorf("parseE621TagNames(%q): %v", payload, err)
		}
		if len(names) != 0 {
			t.Errorf("parseE621TagNames(%q) = %v, want none", payload, names)
		}
	}
}

func TestE621StatusErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{401, `{"success":false,"reason":"Invalid API key"}`, "-e621k"},
		{429, "", "rate limiting"},
		{422, `{"reason":"Invalid tags"}`, "Invalid tags"},
	}
	for _, c := range cases {
		err := e621StatusError(c.status, []byte(c.body))
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("status %d error = %q, want it to mention %q", c.status, err, c.want)
		}
	}
}

func TestE621MediaFileName(t *testing.T) {
	if got := mediaFileName("https://static1.e621.net/data/a2/22/hash.png?download=1"); got != "hash.png" {
		t.Errorf("mediaFileName = %q, want hash.png", got)
	}
	if got := mediaFileName(""); got != "" {
		t.Errorf("mediaFileName of an empty url = %q, want empty", got)
	}
}
