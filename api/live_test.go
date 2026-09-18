package api

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func liveClients() []Client {
	return []Client{NewPornHubClient(), NewXVideosClient(), NewXHamsterClient()}
}

func TestLiveSiteSearchAndStreams(t *testing.T) {
	if os.Getenv("R34_LIVE") == "" {
		t.Skip("set R34_LIVE=1 to run")
	}
	for _, client := range liveClients() {
		posts, err := client.SearchPosts("big test", 5, 0)
		if err != nil {
			t.Errorf("%s search: %v", client.Name(), err)
			continue
		}
		t.Logf("%s: %d posts", client.Name(), len(posts))
		if len(posts) == 0 {
			t.Errorf("%s returned no posts", client.Name())
			continue
		}
		first := posts[0]
		t.Logf("   #%d title=%q thumb=%v duration=%q page=%s", first.ID, first.Title, first.Thumb != "", first.Duration, first.PageURL)
		count, err := client.CountPosts("big test")
		t.Logf("   count=%d err=%v", count, err)
		stream, err := Media(first)
		if err != nil {
			t.Errorf("%s stream: %v", client.Name(), err)
			continue
		}
		t.Logf("   hls=%v cookie=%v url=%.110s", stream.IsHLS(), stream.Cookie != "", stream.URL)
		if err := probeStream(stream); err != nil {
			t.Errorf("%s playback: %v", client.Name(), err)
		} else {
			t.Logf("   ffmpeg decoded 3s ok")
		}
	}
}

func probeStream(stream Stream) error {
	var headers strings.Builder
	if stream.Cookie != "" {
		headers.WriteString("Cookie: " + stream.Cookie + "\r\n")
	}
	if stream.Referer != "" {
		headers.WriteString("Referer: " + stream.Referer + "\r\n")
	}
	args := []string{"-loglevel", "error", "-nostdin", "-user_agent", browserUserAgent}
	if headers.Len() > 0 {
		args = append(args, "-headers", headers.String())
	}
	args = append(args, "-i", stream.URL, "-t", "3", "-f", "null", "-")
	cmd := exec.Command("ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}
