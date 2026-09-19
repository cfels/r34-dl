package ui

import (
	"fmt"
	"image"
	"io"
	"net"
	"net/http"
	"time"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/dl"
	"moxiu/r34-dl/safe"

	tea "github.com/charmbracelet/bubbletea"
)

type searchResultMsg struct {
	posts []api.Post
	err   error
}

type loadMoreMsg struct {
	posts []api.Post
	page  int
	err   error
}

type totalCountMsg struct {
	count int
	err   error
	gen   int
}

type countTickMsg struct {
	gen int
}

type imageFetchedMsg struct {
	data []byte
	err  error
}

type imageRenderedMsg struct{ s string }

type singleDownloadMsg struct {
	path string
	err  error
}

type videoStartedMsg struct {
	vp *videoPlayer
}

type videoFrameMsg struct {
	vp    *videoPlayer
	frame image.Image
}

type videoRenderedMsg struct {
	vp *videoPlayer
	s  string
}

type videoDoneMsg struct {
	vp  *videoPlayer
	err error
}

type blinkMsg struct{}

func doSearch(client api.Client, query string, limit, page int) tea.Cmd {
	return func() tea.Msg {
		posts, err := client.SearchPosts(query, limit, page)
		return searchResultMsg{posts: posts, err: err}
	}
}

func doLoadMore(client api.Client, query string, limit, page int) tea.Cmd {
	return func() tea.Msg {
		posts, err := client.SearchPosts(query, limit, page)
		return loadMoreMsg{posts: posts, page: page, err: err}
	}
}

func doCountTotal(client api.Client, query string, gen int) tea.Cmd {
	return func() tea.Msg {
		count, err := client.CountPosts(query)
		return totalCountMsg{count: count, err: err, gen: gen}
	}
}

func startCountTicker(gen int) tea.Cmd {
	return tea.Tick(countRefreshInterval, func(t time.Time) tea.Msg {
		return countTickMsg{gen: gen}
	})
}

func startBlink() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return blinkMsg{}
	})
}

func fetchImage(rawURL string) tea.Cmd {
	return func() tea.Msg {
		if !safe.PublicMediaURL(rawURL) {
			return imageFetchedMsg{err: fmt.Errorf("refusing to fetch unsupported or non-public image url")}
		}
		client := &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				Proxy:       http.ProxyFromEnvironment,
				DialContext: safe.PublicDialContext(&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}),
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				if !safe.PublicMediaURL(req.URL.String()) {
					return fmt.Errorf("refusing to follow redirect to %s", req.URL.Host)
				}
				return nil
			},
		}
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("build request: %w", err)}
		}
		req.Header.Set("User-Agent", "r34-dl/viewer")
		resp, err := client.Do(req)
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("fetch: %w", err)}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return imageFetchedMsg{err: fmt.Errorf("fetch: %s", resp.Status)}
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("read body: %w", err)}
		}
		if len(data) > maxImageBytes {
			return imageFetchedMsg{err: fmt.Errorf("image is larger than %d MiB", maxImageBytes>>20)}
		}
		return imageFetchedMsg{data: data}
	}
}

func downloadOne(d *dl.Downloader, post api.Post) tea.Cmd {
	return func() tea.Msg {
		var result dl.Result
		got := false
		for r := range d.DownloadAll([]api.Post{post}) {
			result = r
			got = true
		}
		if !got {
			return singleDownloadMsg{err: fmt.Errorf("download produced no result")}
		}
		if result.Err != nil {
			return singleDownloadMsg{err: result.Err}
		}
		return singleDownloadMsg{path: result.Path}
	}
}
