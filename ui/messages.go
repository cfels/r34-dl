package ui

import (
	"fmt"
	"image"
	"io"
	"net/http"
	"time"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/dl"

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

type videoFrameMsg struct {
	frame image.Image
}

type videoDoneMsg struct {
	err error
}

type resultMsg dl.Result
type doneMsg struct{}
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
		client := &http.Client{Timeout: 30 * time.Second}
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
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("read body: %w", err)}
		}
		return imageFetchedMsg{data: data}
	}
}

func downloadOne(d *dl.Downloader, post api.Post) tea.Cmd {
	return func() tea.Msg {
		ch := d.DownloadAll([]api.Post{post})
		r := <-ch
		if r.Err != nil {
			return singleDownloadMsg{err: r.Err}
		}
		return singleDownloadMsg{path: r.Path}
	}
}

func waitForResult(ch <-chan dl.Result) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return doneMsg{}
		}
		return resultMsg(r)
	}
}
