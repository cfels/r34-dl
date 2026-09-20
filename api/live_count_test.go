package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"moxiu/r34-dl/conf"
)

func autocompleteCount(body []byte, tag string) (int, bool) {
	var entries []autoEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return 0, false
	}
	for _, entry := range entries {
		if !strings.EqualFold(entry.Value, tag) {
			continue
		}
		open := strings.LastIndex(entry.Label, " (")
		if open < 0 || !strings.HasSuffix(entry.Label, ")") {
			return 0, false
		}
		count, err := strconv.Atoi(entry.Label[open+2 : len(entry.Label)-1])
		if err != nil {
			return 0, false
		}
		return count, true
	}
	return 0, false
}

func liveBooruCredentials(t *testing.T) (string, string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("locating home: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cfg, err := conf.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	return cfg.UserID, cfg.APIKey
}

func liveBody(t *testing.T, rawURL string) []byte {
	t.Helper()
	client := newSiteHTTPClient()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("User-Agent", browserUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("fetch %s: %v", rawURL, err)
	}
	defer resp.Body.Close()
	body, err := readLimited(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", rawURL, err)
	}
	return body
}

func TestLiveTagCountsMatchTheWebsite(t *testing.T) {
	if os.Getenv("R34_LIVE") == "" {
		t.Skip("set R34_LIVE=1 to run")
	}
	userID, apiKey := liveBooruCredentials(t)
	if apiKey == "" {
		t.Skip("rule34 needs stored api credentials")
	}
	for _, tag := range []string{"remielle_dan", "jane_doe_(zenless_zone_zero)"} {
		client := NewRule34Client(userID, apiKey)
		count, err := client.CountPosts(tag)
		if err != nil {
			t.Errorf("rule34 count for %s: %v", tag, err)
			continue
		}
		body := liveBody(t, "https://api.rule34.xxx/autocomplete.php?q="+tag)
		want, ok := autocompleteCount(body, tag)
		if !ok {
			t.Errorf("rule34 autocomplete did not report %s", tag)
			continue
		}
		t.Logf("rule34 %s: counter=%d website=%d", tag, count, want)
		if count != want {
			t.Errorf("rule34 %s: counter = %d, website shows %d", tag, count, want)
		}
	}
	for _, tag := range []string{"touhou", "remielle_dan"} {
		client := NewSafebooruClient()
		count, err := client.CountPosts(tag)
		if err != nil {
			t.Errorf("safebooru count for %s: %v", tag, err)
			continue
		}
		body := liveBody(t, "https://safebooru.org/autocomplete.php?q="+tag)
		want, ok := autocompleteCount(body, tag)
		if !ok {
			t.Errorf("safebooru autocomplete did not report %s", tag)
			continue
		}
		t.Logf("safebooru %s: counter=%d website=%d", tag, count, want)
		if count != want {
			t.Errorf("safebooru %s: counter = %d, website shows %d", tag, count, want)
		}
	}
}

func TestLiveVideoCountsMatchTheWebsite(t *testing.T) {
	if os.Getenv("R34_LIVE") == "" {
		t.Skip("set R34_LIVE=1 to run")
	}
	query := "remielle dan"

	ph := NewPornHubClient()
	phCount, err := ph.CountPosts(query)
	if err != nil {
		t.Errorf("pornhub count: %v", err)
	} else {
		page := string(liveBody(t, ph.searchURL(query, 0)))
		raw := firstMatch(regexp.MustCompile(`"resultsText":"Results"\s*,\s*"resultsCount":(\d+)`), page)
		want, convErr := strconv.Atoi(raw)
		if raw == "" || convErr != nil {
			t.Errorf("pornhub page did not show a result count")
		} else {
			t.Logf("pornhub %q: counter=%d website=%d", query, phCount, want)
			if phCount != want {
				t.Errorf("pornhub %q: counter = %d, website shows %d", query, phCount, want)
			}
		}
	}

	xv := NewXVideosClient()
	xvCount, err := xv.CountPosts(query)
	if err != nil {
		t.Errorf("xvideos count: %v", err)
	} else {
		page := string(liveBody(t, xv.searchURL(query, 0)))
		raw := firstMatch(regexp.MustCompile(`<span class="sub">\s*\(([\d,]+)\s+results?\)`), page)
		want := digitsOnly(raw)
		if raw == "" {
			t.Errorf("xvideos page did not show a result count")
		} else {
			t.Logf("xvideos %q: counter=%d website=%d", query, xvCount, want)
			if xvCount != want {
				t.Errorf("xvideos %q: counter = %d, website shows %d", query, xvCount, want)
			}
		}
	}

	xh := NewXHamsterClient()
	xhCount, err := xh.CountPosts(query)
	if err != nil {
		t.Errorf("xhamster count: %v", err)
	} else {
		page := string(liveBody(t, xh.searchURL(query, 0)))
		raw := firstMatch(regexp.MustCompile(`"resultCount":"(\d+)`), page)
		want := digitsOnly(raw)
		if raw == "" {
			t.Errorf("xhamster page did not show a result count")
		} else {
			t.Logf("xhamster %q: counter=%d website=%d", query, xhCount, want)
			if xhCount != want {
				t.Errorf("xhamster %q: counter = %d, website shows %d", query, xhCount, want)
			}
		}
	}
}
