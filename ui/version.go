package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"moxiu/r34-dl/safe"

	"golang.org/x/mod/semver"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	releasesRepo    = "cfels/r34-dl"
	releasesPageURL = "https://github.com/" + releasesRepo + "/releases/latest"
)

const (
	releaseTimeout = 6 * time.Second
	maxBodyBytes   = 1 << 20
	maxFieldChars  = 40
	commitChars    = 7
)

var (
	buildVersion string
	buildCommit  string

	latestReleaseAPI = "https://api.github.com/repos/" + releasesRepo + "/releases/latest"
	tagCommitAPI     = "https://api.github.com/repos/" + releasesRepo + "/commits/"
	compareCommitAPI = "https://api.github.com/repos/" + releasesRepo + "/compare/"
)

type versionInfo struct {
	version string
	commit  string
}

type releaseInfoMsg struct {
	info     versionInfo
	outdated bool
	err      error
}

func (v versionInfo) label() string {
	switch {
	case v.version != "" && v.version != "dev":
		return v.version
	case v.commit != "":
		return v.commit
	case v.version != "":
		return v.version
	}
	return "unknown"
}

func versionNewer(latest, current string) bool {
	latest, current = semVersion(latest), semVersion(current)
	return semver.IsValid(latest) && semver.IsValid(current) && semver.Compare(latest, current) > 0
}

func semVersion(raw string) string {
	core, suffix := raw, ""
	if i := strings.IndexAny(raw, "-+"); i >= 0 {
		core, suffix = raw[:i], raw[i:]
	}
	for dots := strings.Count(core, "."); dots < 2; dots++ {
		core += ".0"
	}
	return "v" + core + suffix
}

func sameCommit(a, b string) bool {
	if !validCommit(a) || !validCommit(b) {
		return false
	}
	a, b = strings.ToLower(a), strings.ToLower(b)
	return a == b || strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func releaseOutdated(ctx context.Context, client *http.Client, local, release versionInfo) bool {
	if release.version == "" || release.version == "dev" {
		return false
	}
	if local.version != "" && local.version != "dev" {
		return versionNewer(release.version, local.version)
	}
	if local.commit == "" || release.commit == "" || client == nil || sameCommit(local.commit, release.commit) {
		return false
	}
	// Compare's first page includes file patches; only the status is needed here.
	status, err := githubField(ctx, client,
		compareCommitAPI+url.PathEscape(local.commit)+"..."+url.PathEscape(release.commit)+"?page=2&per_page=1", "status")
	if err != nil {
		return false
	}
	return status == "ahead"
}

func normalizeVersion(raw string) string {
	if raw == "dev" {
		return raw
	}
	return taggedVersion(raw)
}

func taggedVersion(raw string) string {
	version := strings.TrimLeft(raw, "vV")
	if len(version) > maxFieldChars || !semver.IsValid(semVersion(version)) || hasPseudoStamp(version) {
		return ""
	}
	return version
}

func hasPseudoStamp(version string) bool {
	run := 0
	for _, r := range version {
		if r >= '0' && r <= '9' {
			run++
			if run >= 14 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}

func shortCommit(raw string) string {
	if !validCommit(raw) {
		return ""
	}
	return strings.ToLower(raw[:commitChars])
}

func validCommit(raw string) bool {
	if len(raw) < commitChars || len(raw) > 40 {
		return false
	}
	for _, r := range raw {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

func localVersionInfo() versionInfo {
	info := versionInfo{
		version: normalizeVersion(buildVersion),
		commit:  shortCommit(buildCommit),
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	if info.version == "" {
		info.version = taggedVersion(bi.Main.Version)
	}
	if info.commit == "" {
		for _, setting := range bi.Settings {
			if setting.Key == "vcs.revision" {
				info.commit = shortCommit(setting.Value)
			}
		}
	}
	return info
}

func githubField(ctx context.Context, client *http.Client, endpoint, field string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host != "api.github.com" || u.User != nil {
		return "", fmt.Errorf("refusing untrusted update endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "r34-dl/version")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("github request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github %s: %s", endpoint, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return "", fmt.Errorf("read github body: %w", err)
	}
	if len(body) > maxBodyBytes {
		return "", fmt.Errorf("github response exceeds size limit")
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("parse github body: %w", err)
	}
	value, _ := payload[field].(string)
	if value == "" {
		return "", fmt.Errorf("github %s: missing %s", endpoint, field)
	}
	return value, nil
}

func fetchReleaseInfo() tea.Cmd {
	return func() tea.Msg {
		if safe.SkipUpdateCheck() {
			return releaseInfoMsg{err: fmt.Errorf("update check disabled")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
		defer cancel()
		client := newReleaseClient()
		defer client.CloseIdleConnections()
		tag, err := githubField(ctx, client, latestReleaseAPI, "tag_name")
		if err != nil {
			return releaseInfoMsg{err: err}
		}
		info := versionInfo{version: taggedVersion(tag)}
		if info.version == "" {
			return releaseInfoMsg{err: fmt.Errorf("latest release tag is not usable")}
		}
		sha, err := githubField(ctx, client, tagCommitAPI+url.PathEscape(tag), "sha")
		if err == nil {
			info.commit = shortCommit(sha)
		}
		return releaseInfoMsg{info: info, outdated: releaseOutdated(ctx, client, localVersionInfo(), info)}
	}
}

func newReleaseClient() *http.Client {
	return &http.Client{
		Timeout: releaseTimeout,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         safe.PublicDialContext(&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}),
			TLSHandshakeTimeout: 5 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many update redirects")
			}
			if req.URL.Scheme != "https" || req.URL.Host != "api.github.com" || req.URL.User != nil {
				return fmt.Errorf("refusing update redirect outside GitHub HTTPS")
			}
			return nil
		},
	}
}

func mergeVersionInfo(local, release versionInfo) versionInfo {
	info := local
	if info.version == "" && sameCommit(info.commit, release.commit) {
		info.version = release.version
	}
	if info.version == "" {
		info.version = "dev"
	}
	if info.commit == "" {
		info.commit = "unknown"
	}
	return info
}

func (m Model) versionInfo() versionInfo {
	return mergeVersionInfo(m.version, m.release)
}

func formatVersionLine(info versionInfo) string {
	return fmt.Sprintf("ver: %s | commit: %s", info.version, info.commit)
}

func (m Model) versionLine() string {
	return dimStyle.Render("  " + formatVersionLine(m.versionInfo()))
}

func updateNoticeText(local, release versionInfo, outdated bool) string {
	if !outdated || release.version == "" {
		return ""
	}
	return fmt.Sprintf("new version available: %s (you have %s) → %s",
		release.version, local.label(), releasesPageURL)
}

func (m Model) updateNoticeLine() string {
	notice := updateNoticeText(m.version, m.release, m.outdated)
	if notice == "" {
		return ""
	}
	return updateStyle.Render("  ⚠ " + notice)
}

func (m Model) bannerText() string {
	text := titleStyle.Render(bannerASCII) + "\n" + m.versionLine()
	if notice := m.updateNoticeLine(); notice != "" {
		text += "\n" + notice
	}
	return text
}

func latestReleaseStatus() (versionInfo, bool) {
	msg, ok := fetchReleaseInfo()().(releaseInfoMsg)
	if !ok || msg.err != nil {
		return versionInfo{}, false
	}
	return msg.info, msg.outdated
}

func latestReleaseInfo() versionInfo {
	info, _ := latestReleaseStatus()
	return info
}

func VersionLine() string {
	return formatVersionLine(mergeVersionInfo(localVersionInfo(), latestReleaseInfo()))
}

func VersionBanner() string {
	local := localVersionInfo()
	release, outdated := latestReleaseStatus()
	text := titleStyle.Render(bannerASCII) + "\n" +
		dimStyle.Render("  "+formatVersionLine(mergeVersionInfo(local, release)))
	if notice := updateNoticeText(local, release, outdated); notice != "" {
		text += "\n" + updateStyle.Render("  ⚠ "+notice)
	}
	return text
}
