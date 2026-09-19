package ui

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
)

func probeRate(stream api.Stream) (float64, int, string) {
	args := []string{"-v", "error", "-select_streams", "v:0", "-show_entries",
		"stream=avg_frame_rate,r_frame_rate,nb_frames,duration", "-of", "default=nw=1"}
	if isHTTP(stream.URL) {
		args = append(args, "-user_agent", videoUserAgent)
		var h strings.Builder
		if stream.Cookie != "" {
			fmt.Fprintf(&h, "Cookie: %s\r\n", stream.Cookie)
		}
		if stream.Referer != "" {
			fmt.Fprintf(&h, "Referer: %s\r\n", stream.Referer)
		}
		if h.Len() > 0 {
			args = append(args, "-headers", h.String())
		}
	}
	args = append(args, stream.URL)
	out, err := exec.Command("ffprobe", args...).CombinedOutput()
	if err != nil {
		return 0, 0, "probe failed"
	}
	info := parseSourceInfo(string(out))
	frames := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "nb_frames=") {
			fmt.Sscanf(strings.TrimPrefix(line, "nb_frames="), "%d", &frames)
		}
	}
	return info.fps, frames, strings.ReplaceAll(strings.TrimSpace(string(out)), "\n", " ")
}

func measurePlayer(t *testing.T, post api.Post, warmup, seconds float64) (int, time.Duration, string, error) {
	t.Helper()
	vp, err := startVideoPlayer(post, 80, 24, videoOptions{})
	if err != nil {
		return 0, 0, "", err
	}
	defer vp.stop()
	measureFrom := time.Now().Add(time.Duration(warmup * float64(time.Second)))
	measureTo := measureFrom.Add(time.Duration(seconds * float64(time.Second)))
	window := measureTo.Sub(measureFrom)
	frames := 0
	for {
		select {
		case _, ok := <-vp.frames:
			if !ok {
				return frames, window, vp.fpsLabel(), vp.takeErr()
			}
			now := time.Now()
			if now.Before(measureFrom) {
				continue
			}
			if now.After(measureTo) {
				return frames, window, vp.fpsLabel(), nil
			}
			frames++
		case <-time.After(time.Until(measureTo)):
			return frames, window, vp.fpsLabel(), nil
		}
	}
}

func TestLiveRule34FPSDistribution(t *testing.T) {
	if os.Getenv("R34_LIVE") == "" {
		t.Skip("set R34_LIVE=1 to run")
	}
	cfg, err := conf.Load()
	if err != nil {
		t.Fatalf("conf: %v", err)
	}
	rule := api.NewRule34Client(cfg.UserID, cfg.APIKey)
	var videos []api.Post
	for page := 0; page < 3 && len(videos) < 45; page++ {
		posts, err := rule.SearchPosts("video", 30, page)
		if err != nil {
			t.Fatalf("search page %d: %v", page, strings.ReplaceAll(err.Error(), cfg.APIKey, "***"))
		}
		for _, p := range posts {
			if isVideo(p) {
				videos = append(videos, p)
			}
		}
	}
	t.Logf("collected %d rule34 videos", len(videos))

	type result struct {
		post  api.Post
		rate  float64
		frame int
		note  string
	}
	results := make([]result, len(videos))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, p := range videos {
		wg.Add(1)
		go func(i int, p api.Post) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			stream, err := api.Media(p)
			if err != nil {
				results[i] = result{post: p, note: "media: " + err.Error()}
				return
			}
			rate, frames, note := probeRate(stream)
			results[i] = result{post: p, rate: rate, frame: frames, note: note}
		}(i, p)
	}
	wg.Wait()

	tally := map[int]int{}
	var over30, over60 []result
	for i, r := range results {
		t.Logf("[%2d] #%d %.2ffps %s", i+1, r.post.ID, r.rate, r.note)
		if r.rate <= 0 {
			continue
		}
		tally[int(r.rate+0.5)]++
		if r.rate > 31 {
			over30 = append(over30, r)
		}
		if r.rate > 61 {
			over60 = append(over60, r)
		}
	}
	keys := make([]int, 0, len(tally))
	for k := range tally {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, k := range keys {
		t.Logf("rate %dfps: %d videos", k, tally[k])
	}
	t.Logf("above 30fps: %d, above 60fps: %d", len(over30), len(over60))

	if len(over30) > 0 {
		pick := over30[0]
		frames, elapsed, label, err := measurePlayer(t, pick.post, 2, 6)
		t.Logf("preview of #%d (%.2ffps source): %d frames in %v (%.1f fps effective) label=%q err=%v",
			pick.post.ID, pick.rate, frames, elapsed.Round(time.Millisecond),
			float64(frames)/elapsed.Seconds(), label, err)
	}
	if len(over60) > 0 {
		pick := over60[0]
		frames, elapsed, label, err := measurePlayer(t, pick.post, 2, 6)
		t.Logf("preview of #%d (%.2ffps source): %d frames in %v (%.1f fps effective) label=%q err=%v",
			pick.post.ID, pick.rate, frames, elapsed.Round(time.Millisecond),
			float64(frames)/elapsed.Seconds(), label, err)
	}
}
