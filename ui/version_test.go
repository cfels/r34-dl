package ui

import (
	"strings"
	"testing"

	"moxiu/r34-dl/conf"
)

func TestSanitizeFieldRejectsTerminalEscapes(t *testing.T) {
	got := sanitizeField("\x1b]0;owned\x07v1.4 ; rm -rf /", maxFieldChars)
	if strings.ContainsRune(got, 0x1b) {
		t.Fatalf("escape byte survived sanitizing: %q", got)
	}
	for _, r := range got {
		if r < 0x20 || r > 0x7e {
			t.Fatalf("control rune %q survived sanitizing", r)
		}
		if r == ' ' || r == ';' || r == '&' || r == '|' || r == '$' || r == '`' {
			t.Fatalf("shell metacharacter %q survived sanitizing", r)
		}
	}
	if len(got) > maxFieldChars {
		t.Fatalf("sanitized field is %d chars, want at most %d", len(got), maxFieldChars)
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
	if got := shortCommit("abc"); got != "abc" {
		t.Errorf("shortCommit = %q, want abc", got)
	}
	if got := shortCommit("\x1b[0m"); got != "0m" {
		t.Errorf("shortCommit = %q, want 0m", got)
	}
}

func TestVersionInfoPullsReleaseInfoWhenBuildHasNoVersion(t *testing.T) {
	m := Model{
		version: versionInfo{commit: "33425d7"},
		release: versionInfo{version: "1.3", commit: "ad6949c"},
	}
	got := m.versionInfo()
	if got.version != "1.3" {
		t.Errorf("version = %q, want the release tag when the build carries none", got.version)
	}
	if got.commit != "ad6949c" {
		t.Errorf("commit = %q, want the commit of the latest release tag", got.commit)
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
	if !strings.Contains(got, "ver: 1.3 | commit: ad6949c") {
		t.Fatalf("banner version line is wrong:\n%s", got)
	}
	if want := m.versionLine(); !strings.Contains(got, want) {
		t.Fatalf("banner should reuse the search-screen version line")
	}
}
