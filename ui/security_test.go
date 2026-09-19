package ui

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"strings"
	"testing"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
	"moxiu/r34-dl/safe"
)

const escapePayload = "\x1b]52;c;aGFja2Vk\x07\x1b[2J"

func TestPostLineStripsRemoteEscapes(t *testing.T) {
	title := api.Post{ID: 1, Site: "xvideos", Video: true, Title: escapePayload + "title", Duration: escapePayload + "12:34"}
	line := renderPostLine(title, 120, false)
	if strings.Contains(line, "\x1b]52;c;") || strings.Contains(line, "\x1b[2J") || strings.Contains(line, "\x07") {
		t.Errorf("titled row leaked an escape sequence: %q", line)
	}

	tags := api.Post{ID: 2, Site: "rule34", Tags: "safe " + escapePayload + " tags"}
	line = renderPostLine(tags, 120, false)
	if strings.Contains(line, "\x1b]52;c;") || strings.Contains(line, "\x1b[2J") || strings.Contains(line, "\x07") {
		t.Errorf("tag row leaked an escape sequence: %q", line)
	}
}

func TestViewStripsRemoteEscapes(t *testing.T) {
	m := NewModel(testClients(stubClient{}), conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "", 30)
	m.width, m.height = 100, 30
	m.state = stateList
	m.posts = []api.Post{
		{ID: 1, Tags: "safe " + escapePayload + " tags"},
		{ID: 2, Title: escapePayload + "video", Video: true},
	}
	if view := m.View(); strings.Contains(view, "\x1b]52;c;") || strings.Contains(view, "\x07") {
		t.Errorf("list view leaked an escape sequence: %q", view)
	}

	m.state = stateSearch
	m.err = "r34 err: " + escapePayload + "boom"
	if view := m.View(); strings.Contains(view, "\x1b]52;c;") || strings.Contains(view, "\x07") {
		t.Errorf("search view leaked an escape sequence: %q", view)
	}

	m.err = `request failed: Get "https://api.rule34.xxx/index.php?api_key=SUPERSECRET123&user_id=777": dial tcp: timeout`
	if view := m.View(); strings.Contains(view, "SUPERSECRET123") || strings.Contains(view, "user_id=777") {
		t.Errorf("search view leaked credentials: %q", view)
	}

	m.state = stateViewer
	m.viewerErr = escapePayload + "ffmpeg: bad"
	if view := m.View(); strings.Contains(view, "\x1b]52;c;") || strings.Contains(view, "\x07") {
		t.Errorf("viewer error leaked an escape sequence: %q", view)
	}
}

func TestQueryRenderKeepsCursorAligned(t *testing.T) {
	m := NewModel(testClients(stubClient{}), conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "", 30)
	m.width, m.height = 100, 30
	m.state = stateSearch
	m.query = "cat" + escapePayload
	m.inputCursor = len([]rune(m.query))
	view := m.View()
	if strings.Contains(view, "\x1b]52;c;") || strings.Contains(view, "\x07") {
		t.Errorf("query render leaked an escape sequence: %q", view)
	}
}

func TestNoticeAndErrorPathsStripRemoteEscapes(t *testing.T) {
	m := NewModel(testClients(stubClient{}), conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "", 30)
	m.width, m.height = 100, 30
	m.state = stateList

	updated, _ := m.Update(singleDownloadMsg{err: errors.New("ffmpeg: " + escapePayload + "boom")})
	m = updated.(Model)
	if view := m.View(); strings.Contains(view, "\x1b]52;c;") || strings.Contains(view, "\x07") {
		t.Errorf("download notice leaked an escape sequence: %q", view)
	}

	updated, _ = m.Update(singleDownloadMsg{path: escapePayload + "/tmp/1.mp4"})
	m = updated.(Model)
	if view := m.View(); strings.Contains(view, "\x1b]52;c;") || strings.Contains(view, "\x07") {
		t.Errorf("saved notice leaked an escape sequence: %q", view)
	}

	updated, _ = m.Update(loadMoreMsg{err: errors.New(escapePayload + "page failed")})
	m = updated.(Model)
	if view := m.View(); strings.Contains(view, "\x1b]52;c;") || strings.Contains(view, "\x07") {
		t.Errorf("load-more notice leaked an escape sequence: %q", view)
	}

	updated, _ = m.Update(searchResultMsg{err: errors.New("r34 err: " + escapePayload)})
	m = updated.(Model)
	m.state = stateSearch
	if view := m.View(); strings.Contains(view, "\x1b]52;c;") || strings.Contains(view, "\x07") {
		t.Errorf("search error leaked an escape sequence: %q", view)
	}
}

func pngWithDims(width, height uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	chunk := func(typ string, data []byte) []byte {
		out := make([]byte, 0, 12+len(data))
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(data)))
		out = append(out, size[:]...)
		out = append(out, typ...)
		out = append(out, data...)
		crc := crc32.NewIEEE()
		crc.Write([]byte(typ))
		crc.Write(data)
		var sum [4]byte
		binary.BigEndian.PutUint32(sum[:], crc.Sum32())
		return append(out, sum[:]...)
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], width)
	binary.BigEndian.PutUint32(ihdr[4:], height)
	ihdr[8], ihdr[9] = 8, 6
	buf.Write(chunk("IHDR", ihdr))
	buf.Write(chunk("IDAT", []byte{0x78, 0x9c, 0x63, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01}))
	buf.Write(chunk("IEND", nil))
	return buf.Bytes()
}

func TestRenderImageRejectsOversizedDecodes(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"huge square", pngWithDims(40000, 40000)},
		{"long side", pngWithDims(200000, 2)},
		{"tall side", pngWithDims(2, 200000)},
		{"not an image", []byte("not an image at all")},
	}
	for _, c := range cases {
		msg := renderImage(c.data, 100, 30)()
		fetched, ok := msg.(imageFetchedMsg)
		if !ok {
			t.Errorf("%s: got %T, want imageFetchedMsg", c.name, msg)
			continue
		}
		if fetched.err == nil {
			t.Errorf("%s: oversized image was accepted", c.name)
		}
	}
}

func TestRenderImageAcceptsNormalImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	msg := renderImage(buf.Bytes(), 100, 30)()
	switch got := msg.(type) {
	case imageRenderedMsg:
	case imageFetchedMsg:
		t.Fatalf("normal image rejected: %v", got.err)
	default:
		t.Fatalf("got %T, want a rendered or fetched message", msg)
	}
}

func TestPreviewSizePolicy(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		if previewTooLarge(format, 4044, 6000) {
			t.Errorf("%s 4044x6000 should stay previewable", format)
		}
	}
	if !previewTooLarge("jpeg", 10000, 10000) {
		t.Error("10000x10000 jpeg should exceed the decode budget")
	}
	if !previewTooLarge("png", 20000, 20000) || !previewTooLarge("png", 2, 20000) {
		t.Error("images past the side limit should be rejected")
	}
}

func TestImageFetchRejectsUnsupportedScheme(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "concat:/etc/passwd", "/etc/passwd"} {
		msg := fetchImage(raw)()
		fetched, ok := msg.(imageFetchedMsg)
		if !ok {
			t.Fatalf("got %T, want imageFetchedMsg", msg)
		}
		if fetched.err == nil {
			t.Errorf("fetchImage(%q) was allowed", raw)
		}
	}
}

func TestVideoPlayerRejectsNonMediaSchemes(t *testing.T) {
	for _, raw := range []string{"concat:/etc/passwd", "file:///etc/passwd", "/etc/passwd", "data:text/plain;base64,aGk="} {
		post := api.Post{ID: 3, Image: "3.mp4", FileURL_: raw}
		if !isVideo(post) {
			t.Fatalf("fixture %q should be treated as video", raw)
		}
		vp, err := startVideoPlayer(post, 80, 24, videoOptions{})
		if err == nil {
			vp.stop()
			t.Errorf("startVideoPlayer(%q) started, want rejection", raw)
		}
	}
}

func TestVideoPlayerRefusesPrivateHostsWithoutHatch(t *testing.T) {
	t.Setenv(safe.AllowPrivateHostsEnv, "0")
	for _, raw := range []string{"http://127.0.0.1:8080/4.mp4", "http://169.254.169.254/4.mp4", "http://10.1.2.3/4.mp4"} {
		post := api.Post{ID: 4, Image: "4.mp4", FileURL_: raw}
		vp, err := startVideoPlayer(post, 80, 24, videoOptions{})
		if err == nil {
			vp.stop()
			t.Errorf("startVideoPlayer(%q) reached a non-public host", raw)
		}
	}
}

func TestImageFetchRefusesPrivateHostsWithoutHatch(t *testing.T) {
	t.Setenv(safe.AllowPrivateHostsEnv, "0")
	for _, raw := range []string{"http://127.0.0.1:9/4.jpg", "http://192.168.0.10/4.jpg"} {
		msg := fetchImage(raw)()
		fetched, ok := msg.(imageFetchedMsg)
		if !ok {
			t.Fatalf("got %T, want imageFetchedMsg", msg)
		}
		if fetched.err == nil {
			t.Errorf("fetchImage(%q) reached a non-public host", raw)
		}
	}
}

func TestUpdateCheckCanBeDisabled(t *testing.T) {
	t.Setenv(safe.SkipUpdateCheckEnv, "1")
	msg := fetchReleaseInfo()()
	release, ok := msg.(releaseInfoMsg)
	if !ok {
		t.Fatalf("got %T, want releaseInfoMsg", msg)
	}
	if release.err == nil {
		t.Error("update check should be skipped when opted out")
	}
	if line := VersionLine(); !strings.Contains(line, "ver:") {
		t.Errorf("VersionLine = %q, want the local version when the check is skipped", line)
	}
}
