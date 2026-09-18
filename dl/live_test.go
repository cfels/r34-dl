package dl

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"moxiu/r34-dl/api"
)

func requireLive(t *testing.T) {
	t.Helper()
	if os.Getenv("R34_LIVE") == "" {
		t.Skip("set R34_LIVE=1 to hit the real sites")
	}
}

func durationSeconds(duration string) int {
	parts := strings.Split(duration, ":")
	if len(parts) < 2 {
		return 1 << 30
	}
	total := 0
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return 1 << 30
		}
		total = total*60 + n
	}
	return total
}

func shortestPost(posts []api.Post) api.Post {
	best := posts[0]
	for _, post := range posts[1:] {
		if durationSeconds(post.Duration) < durationSeconds(best.Duration) {
			best = post
		}
	}
	return best
}

func checkSaved(t *testing.T, result Result) {
	t.Helper()
	if result.Err != nil {
		t.Fatalf("download: %v", result.Err)
	}
	info, err := os.Stat(result.Path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	t.Logf("saved %s (%d bytes)", result.Path, info.Size())
	out, err := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=duration,format_name",
		"-of", "default=nw=1", result.Path).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	t.Logf("ffprobe: %s", strings.ReplaceAll(string(out), "\n", " "))
}

func TestLiveDownloadXVideos(t *testing.T) {
	requireLive(t)
	posts, err := api.NewXVideosClient().SearchPosts("test", 1, 0)
	if err != nil || len(posts) == 0 {
		t.Fatalf("search: %v (%d posts)", err, len(posts))
	}
	checkSaved(t, New(t.TempDir(), 1).downloadOne(posts[0]))
}

func TestLiveDownloadPornHub(t *testing.T) {
	requireLive(t)
	posts, err := api.NewPornHubClient().SearchPosts("test", 20, 0)
	if err != nil || len(posts) == 0 {
		t.Fatalf("search: %v (%d posts)", err, len(posts))
	}
	post := shortestPost(posts)
	t.Logf("downloading #%d (%s) %q", post.ID, post.Duration, post.Title)
	checkSaved(t, New(t.TempDir(), 1).downloadOne(post))
}

func TestLiveDownloadXHamster(t *testing.T) {
	requireLive(t)
	posts, err := api.NewXHamsterClient().SearchPosts("test", 20, 0)
	if err != nil || len(posts) == 0 {
		t.Fatalf("search: %v (%d posts)", err, len(posts))
	}
	post := shortestPost(posts)
	t.Logf("downloading #%d (%s) %q", post.ID, post.Duration, post.Title)
	checkSaved(t, New(t.TempDir(), 1).downloadOne(post))
}
