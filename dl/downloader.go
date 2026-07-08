package dl

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"moxiu/r34-dl/api"
)

type Result struct {
	Post api.Post
	Path string
	Err  error
}

type Downloader struct {
	http    *http.Client
	workers int
	outDir  string
}

func New(outDir string, workers int) *Downloader {
	if workers <= 0 {
		workers = 4
	}
	return &Downloader{
		http:    &http.Client{Timeout: 30 * time.Second},
		workers: workers,
		outDir:  outDir,
	}
}

func (d *Downloader) DownloadAll(posts []api.Post) <-chan Result {
	results := make(chan Result, len(posts))

	if err := os.MkdirAll(d.outDir, 0o755); err != nil {
		go func() {
			results <- Result{Err: fmt.Errorf("creating output dir: %w", err)}
			close(results)
		}()
		return results
	}

	jobs := make(chan api.Post)
	var wg sync.WaitGroup

	for i := 0; i < d.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for post := range jobs {
				results <- d.downloadOne(post)
			}
		}()
	}

	go func() {
		for _, p := range posts {
			jobs <- p
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

func (d *Downloader) downloadOne(post api.Post) Result {
	url := post.FileURL()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return Result{Post: post, Err: err}
	}
	req.Header.Set("User-Agent", "r34-dl/safebooru-client")

	resp, err := d.http.Do(req)
	if err != nil {
		return Result{Post: post, Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Result{Post: post, Err: fmt.Errorf("status %d for post %d", resp.StatusCode, post.ID)}
	}

	filename := fmt.Sprintf("%d_%s", post.ID, post.Image)
	outPath := filepath.Join(d.outDir, filename)

	f, err := os.Create(outPath)
	if err != nil {
		return Result{Post: post, Err: err}
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return Result{Post: post, Err: err}
	}

	return Result{Post: post, Path: outPath}
}
