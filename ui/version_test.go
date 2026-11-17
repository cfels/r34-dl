package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
)

func TestVersionFieldsRejectTerminalEscapes(t *testing.T) {
	for _, raw := range []string{"\x1b]0;owned\x07v1.4", "1.4 ; rm -rf /", "1.4\n", strings.Repeat("1", maxFieldChars+1)} {
		if taggedVersion(raw) != "" || shortCommit(raw) != "" {
			t.Fatal("invalid version metadata was accepted")
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"v1.3":  "1.3",
		"V1.4":  "1.4",
		"1.5":   "1.5",
		"vv1.6": "1.6",
		"":      "",
	}
	for in, want := range cases {
		if got := normalizeVersion(in); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTaggedVersion(t *testing.T) {
	cases := map[string]string{
		"v1.3":                                   "1.3",
		"1.3.1":                                  "1.3.1",
		"v1.4-rc1":                               "1.4-rc1",
		"(devel)":                                "",
		"":                                       "",
		".1":                                     "",
		"0.0.0-20260715184921-33425d71daa3dirty": "",
		"v1.3.1-0.20260715184921-33425d71daa3":   "",
	}
	for in, want := range cases {
		if got := taggedVersion(in); got != want {
			t.Errorf("taggedVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShortCommit(t *testing.T) {
	if got := shortCommit("33425d7ff9e2c1b0a1"); got != "33425d7" {
		t.Errorf("shortCommit = %q, want 33425d7", got)
	}
	if got := shortCommit("abc"); got != "" {
		t.Errorf("shortCommit = %q, want an empty invalid commit", got)
	}
	if got := shortCommit("\x1b[0m"); got != "" {
		t.Errorf("shortCommit = %q, want an empty invalid commit", got)
	}
}

func TestVersionInfoDoesNotClaimAnUninstalledRelease(t *testing.T) {
	m := Model{
		version: versionInfo{commit: "33425d7"},
		release: versionInfo{version: "1.3", commit: "ad6949c"},
	}
	got := m.versionInfo()
	if got.version != "dev" {
		t.Errorf("version = %q, want dev for an untagged build", got.version)
	}
	if got.commit != "33425d7" {
		t.Errorf("commit = %q, want the installed build's commit", got.commit)
	}
}

func TestVersionInfoPrefersStampedBuild(t *testing.T) {
	m := Model{
		version: versionInfo{version: "1.4", commit: "aaaaaaa"},
		release: versionInfo{version: "1.3", commit: "dae6e6e"},
	}
	got := m.versionInfo()
	if got.version != "1.4" || got.commit != "aaaaaaa" {
		t.Errorf("versionInfo = %+v, want the local build metadata", got)
	}
}

func TestVersionInfoFallsBackToBuildCommitWhenReleaseIsUnknown(t *testing.T) {
	m := Model{version: versionInfo{commit: "33425d7"}}
	got := m.versionInfo()
	if got.version != "dev" || got.commit != "33425d7" {
		t.Errorf("versionInfo = %+v, want dev plus the build commit", got)
	}
}

func TestVersionInfoFallsBackWhenOffline(t *testing.T) {
	got := Model{}.versionInfo()
	if got.version != "dev" || got.commit != "unknown" {
		t.Fatalf("versionInfo = %+v, want dev/unknown", got)
	}
	if line := (Model{}).versionLine(); !strings.Contains(line, "ver: dev | commit: unknown") {
		t.Fatalf("versionLine = %q", line)
	}
}

func TestSearchViewShowsVersionLine(t *testing.T) {
	client := &recordingClient{}
	m := NewModel(testClients(client), conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "", 30)
	m.width, m.height = 100, 30
	m.state = stateSearch
	m.release = versionInfo{version: "1.3", commit: "dae6e6e"}

	view := m.View()
	if !strings.Contains(view, "ver: ") || !strings.Contains(view, "commit: ") {
		t.Fatalf("search view is missing the version line:\n%s", view)
	}
	if strings.Contains(view, "v1.1") {
		t.Fatalf("hardcoded version is still in the banner:\n%s", view)
	}
}

func TestBannerTextShowsArtAndVersionLine(t *testing.T) {
	m := Model{release: versionInfo{version: "1.3", commit: "ad6949c"}}
	got := m.bannerText()
	if !strings.Contains(got, "░▒▓█") {
		t.Fatalf("banner art is missing:\n%s", got)
	}
	if !strings.Contains(got, "ver: dev | commit: unknown") {
		t.Fatalf("banner version line is wrong:\n%s", got)
	}
	if want := m.versionLine(); !strings.Contains(got, want) {
		t.Fatalf("banner should reuse the search-screen version line")
	}
}

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		latest  string
		current string
		want    bool
	}{
		{"1.4", "1.3", true},
		{"1.3", "1.3", false},
		{"1.3", "1.4", false},
		{"1.10", "1.9", true},
		{"2", "1.9.9", true},
		{"1.4", "1.4-rc1", true},
		{"1.4-rc1", "1.4", false},
		{"1.4-rc2", "1.4-rc1", true},
		{"1.4.1", "1.4", true},
		{"1.4.0-rc.10", "1.4.0-rc.2", true},
		{"1.4.0-rc.2", "1.4.0-rc.10", false},
		{"1.4.0+build.2", "1.4.0+build.1", false},
		{"1.4.0-2", "1.4.0-alpha", false},
		{"1.4.0-alpha.1", "1.4.0-alpha", true},
		{"1.4broken", "1.3", false},
		{"1.4", "unknown", false},
	}
	for _, c := range cases {
		if got := versionNewer(c.latest, c.current); got != c.want {
			t.Errorf("versionNewer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestVersionLabel(t *testing.T) {
	cases := map[versionInfo]string{
		{version: "1.3", commit: "33425d7"}: "1.3",
		{version: "dev", commit: "33425d7"}: "33425d7",
		{version: "dev"}:                    "dev",
		{commit: "33425d7"}:                 "33425d7",
		{}:                                  "unknown",
	}
	for info, want := range cases {
		if got := info.label(); got != want {
			t.Errorf("versionInfo%+v label = %q, want %q", info, got, want)
		}
	}
}

func TestReleaseOutdatedForTaggedBuilds(t *testing.T) {
	cases := []struct {
		local   versionInfo
		release versionInfo
		want    bool
	}{
		{versionInfo{version: "1.3"}, versionInfo{version: "1.3"}, false},
		{versionInfo{version: "1.2"}, versionInfo{version: "1.3"}, true},
		{versionInfo{version: "1.4"}, versionInfo{version: "1.3"}, false},
		{versionInfo{version: "1.4-rc1"}, versionInfo{version: "1.4"}, true},
		{versionInfo{}, versionInfo{version: "1.4"}, false},
		{versionInfo{version: "1.3"}, versionInfo{}, false},
	}
	for _, c := range cases {
		if got := releaseOutdated(context.Background(), nil, c.local, c.release); got != c.want {
			t.Errorf("releaseOutdated(%+v, %+v) = %v, want %v", c.local, c.release, got, c.want)
		}
	}
}

func TestReleaseOutdatedForDevBuildsUsesCompare(t *testing.T) {
	old := compareCommitAPI
	compareCommitAPI = "https://api.github.com/repos/" + releasesRepo + "/compare/"
	t.Cleanup(func() { compareCommitAPI = old })

	release := versionInfo{version: "1.4", commit: "bbbbbbb"}
	cases := []struct {
		name   string
		commit string
		status string
		code   int
		want   bool
	}{
		{name: "release ahead", commit: "aaaaaaa", status: "ahead", code: http.StatusOK, want: true},
		{name: "local ahead", commit: "ccccccc", status: "behind", code: http.StatusOK},
		{name: "identical", commit: "ddddddd", status: "identical", code: http.StatusOK},
		{name: "diverged", commit: "eeeeeee", status: "diverged", code: http.StatusOK},
		{name: "unknown commit", commit: "fffffff", code: http.StatusNotFound},
		{name: "same commit", commit: "bbbbbbb", status: "ahead", code: http.StatusOK},
	}
	for _, c := range cases {
		transport := &stubGithubTransport{status: c.status, code: c.code}
		client := &http.Client{Transport: transport}
		local := versionInfo{version: "dev", commit: c.commit}
		if got := releaseOutdated(context.Background(), client, local, release); got != c.want {
			t.Errorf("%s: releaseOutdated(%+v, %+v) = %v, want %v", c.name, local, release, got, c.want)
		}
		if c.want && !strings.Contains(transport.url, local.commit+"..."+release.commit) {
			t.Errorf("%s: compared %q, want the local and release commits", c.name, transport.url)
		}
	}
}

type stubGithubTransport struct {
	status string
	code   int
	url    string
}

func (s *stubGithubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.url = req.URL.String()
	body := fmt.Sprintf(`{"status":%q}`, s.status)
	return &http.Response{
		StatusCode: s.code,
		Status:     http.StatusText(s.code),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

func TestReleaseOutdatedIgnoresDevBuildWithoutReleaseCommit(t *testing.T) {
	local := versionInfo{version: "dev", commit: "aaaaaaa"}
	if releaseOutdated(context.Background(), nil, local, versionInfo{version: "1.4"}) {
		t.Error("a dev build should not be called outdated without a release commit to compare")
	}
	if releaseOutdated(context.Background(), nil, versionInfo{version: "dev"}, versionInfo{version: "1.4", commit: "bbbbbbb"}) {
		t.Error("a build without a commit should not be called outdated")
	}
}

func TestUpdateNoticeIsYellowAndNamesBothVersions(t *testing.T) {
	if got := updateStyle.GetForeground(); got != mochaYellow {
		t.Errorf("update notice colour = %v, want the yellow accent", got)
	}
	notice := Model{
		version:  versionInfo{version: "1.3", commit: "aaaaaaa"},
		release:  versionInfo{version: "1.4", commit: "bbbbbbb"},
		outdated: true,
	}.updateNoticeLine()
	if notice == "" {
		t.Fatal("outdated build produced no notice")
	}
	for _, want := range []string{"1.4", "1.3", releasesPageURL} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q is missing %q", notice, want)
		}
	}
}

func TestUpdateNoticeHiddenWhenCurrent(t *testing.T) {
	m := Model{
		version: versionInfo{version: "1.4", commit: "bbbbbbb"},
		release: versionInfo{version: "1.4", commit: "bbbbbbb"},
	}
	if got := m.updateNoticeLine(); got != "" {
		t.Errorf("up to date build should not be nagged, got %q", got)
	}
	if strings.Contains(m.bannerText(), "new version available") {
		t.Error("banner should not carry an update notice when up to date")
	}
}

func TestUpdateNoticeRejectsTerminalEscapesFromReleaseTag(t *testing.T) {
	release := versionInfo{version: taggedVersion("\x1b]0;owned\x07v9.9"), commit: "bbbbbbb"}
	notice := updateNoticeText(versionInfo{version: "1.3"}, release, true)
	if strings.ContainsRune(notice, 0x1b) || strings.ContainsRune(notice, 0x07) {
		t.Fatalf("escape sequence survived into notice: %q", notice)
	}
}

func TestSearchViewShowsUpdateNotice(t *testing.T) {
	client := &recordingClient{}
	m := NewModel(testClients(client), conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "", 30)
	m.width, m.height = 120, 30
	m.state = stateSearch
	m.version = versionInfo{version: "1.3", commit: "aaaaaaa"}
	m.release = versionInfo{version: "1.4", commit: "bbbbbbb"}
	m.outdated = true

	view := m.View()
	if !strings.Contains(view, "new version available: 1.4") {
		t.Fatalf("search view is missing the update notice:\n%s", view)
	}
}

func TestListFooterShowsUpdateNotice(t *testing.T) {
	client := &recordingClient{}
	m := NewModel(testClients(client), conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "", 30)
	m.width, m.height = 120, 30
	m.state = stateList
	m.posts = []api.Post{{ID: 1, Tags: "cat"}}
	m.release = versionInfo{version: "1.4", commit: "bbbbbbb"}
	m.outdated = true

	if view := m.View(); !strings.Contains(view, "update available") {
		t.Fatalf("list view is missing the update notice:\n%s", view)
	}
	m.outdated = false
	if view := m.View(); strings.Contains(view, "update available") {
		t.Fatalf("list view should hide the update notice when up to date:\n%s", view)
	}
}

func TestReleaseInfoMsgDrivesUpdateNotice(t *testing.T) {
	client := &recordingClient{}
	m := NewModel(testClients(client), conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "", 30)
	updated, _ := m.Update(releaseInfoMsg{info: versionInfo{version: "1.4", commit: "bbbbbbb"}, outdated: true})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("update returned %T, want ui.Model", updated)
	}
	if !got.outdated || got.release.version != "1.4" {
		t.Errorf("model release state = %+v outdated=%v, want the release flagged as outdated", got.release, got.outdated)
	}
}

type githubTransportFunc func(*http.Request) (*http.Response, error)

func (fn githubTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestUpdateClientRejectsUntrustedRedirects(t *testing.T) {
	for _, target := range []string{
		"http://api.github.com/repos/cfels/r34-dl/releases/latest",
		"https://api.github.com.evil.example/releases/latest",
		"https://evil.example/releases/latest",
		"http://127.0.0.1/private",
		"https://user@api.github.com/releases/latest",
	} {
		t.Run(target, func(t *testing.T) {
			hits := 0
			client := newReleaseClient()
			client.Transport = githubTransportFunc(func(req *http.Request) (*http.Response, error) {
				hits++
				return &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {target}},
					Body:       io.NopCloser(strings.NewReader("")),
					Request:    req,
				}, nil
			})
			if _, err := githubField(context.Background(), client, latestReleaseAPI, "tag_name"); err == nil || hits != 1 {
				t.Fatalf("unsafe redirect followed: hits=%d err=%v", hits, err)
			}
		})
	}
}

func TestGithubFieldRejectsUntrustedEndpoints(t *testing.T) {
	client := &http.Client{Transport: githubTransportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("untrusted endpoint reached the transport")
		return nil, errors.New("unreachable")
	})}
	for _, endpoint := range []string{"http://api.github.com/", "https://evil.example/", "file:///private"} {
		if _, err := githubField(context.Background(), client, endpoint, "tag_name"); err == nil {
			t.Fatalf("accepted update endpoint %q", endpoint)
		}
	}
}

func TestGithubFieldBoundsResponseSize(t *testing.T) {
	for _, size := range []int{128 << 10, maxBodyBytes + 1} {
		client := &http.Client{Transport: githubTransportFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"tag_name":"v1.4","body":"` + strings.Repeat("a", size) + `"}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
		})}
		got, err := githubField(context.Background(), client, latestReleaseAPI, "tag_name")
		if size < maxBodyBytes && (err != nil || got != "v1.4") {
			t.Fatalf("normal release body rejected: %v", err)
		}
		if size > maxBodyBytes && (err == nil || !strings.Contains(err.Error(), "limit")) {
			t.Fatalf("oversized release body accepted: %v", err)
		}
	}
}

func TestGithubFieldHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &http.Client{Transport: githubTransportFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})}
	if _, err := githubField(ctx, client, latestReleaseAPI, "tag_name"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not propagated: %v", err)
	}
}

func TestVersionInfoOnlyUsesReleaseTagForMatchingCommit(t *testing.T) {
	got := mergeVersionInfo(versionInfo{commit: "abcdef1"}, versionInfo{version: "1.4", commit: "abcdef123456"})
	if got.version != "1.4" || got.commit != "abcdef1" {
		t.Fatalf("matching build metadata = %+v", got)
	}
	got = mergeVersionInfo(versionInfo{version: "1.3"}, versionInfo{version: "1.4", commit: "abcdef1"})
	if got.version != "1.3" || got.commit != "unknown" {
		t.Fatalf("release commit was attributed to a different installed build: %+v", got)
	}
}
