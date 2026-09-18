package main

import (
	"testing"

	"moxiu/r34-dl/api"
)

type pagedClient struct {
	pages map[int][]api.Post
	asked []int
}

func (c *pagedClient) SearchPosts(tags string, limit, page int) ([]api.Post, error) {
	c.asked = append(c.asked, page)
	return c.pages[page], nil
}

func (c *pagedClient) CountPosts(tags string) (int, error) { return 0, nil }
func (c *pagedClient) Name() string                        { return "paged" }
func (c *pagedClient) Autocomplete(prefix string) ([]string, error) {
	return nil, nil
}

func TestFetchBulkPostsWalksPages(t *testing.T) {
	client := &pagedClient{pages: map[int][]api.Post{
		0: {{ID: 1}, {ID: 2}},
		1: {{ID: 3}, {ID: 4}},
		2: {{ID: 5}},
	}}
	posts, err := fetchBulkPosts(client, "tags", 4)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(posts) != 4 {
		t.Fatalf("got %d posts, want 4", len(posts))
	}
	if len(client.asked) != 2 {
		t.Errorf("asked pages %v, want two pages", client.asked)
	}
}

func TestFetchBulkPostsStopsOnEmptyPage(t *testing.T) {
	client := &pagedClient{pages: map[int][]api.Post{
		0: {{ID: 1}},
	}}
	posts, err := fetchBulkPosts(client, "tags", 10)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(posts) != 1 {
		t.Errorf("got %d posts, want 1", len(posts))
	}
	if len(client.asked) != 2 {
		t.Errorf("asked pages %v, want a second request before giving up", client.asked)
	}
}

func TestFetchBulkPostsSkipsRepeats(t *testing.T) {
	client := &pagedClient{pages: map[int][]api.Post{
		0: {{ID: 1}, {ID: 2}},
		1: {{ID: 1}, {ID: 2}},
	}}
	posts, err := fetchBulkPosts(client, "tags", 5)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(posts) != 2 {
		t.Errorf("got %d posts, want the repeats dropped", len(posts))
	}
	if len(client.asked) > 3 {
		t.Errorf("asked pages %v, the walk should stop early", client.asked)
	}
}
