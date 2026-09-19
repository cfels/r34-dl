package ui

import (
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

	tea "github.com/charmbracelet/bubbletea"
)

const (
	releasesRepo    = "cfels/r34-dl"
	releasesPageURL = "https://github.com/" + releasesRepo + "/releases/latest"
)

const (
	releaseTimeout = 6 * time.Second
	maxBodyBytes   = 64 << 10
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

func splitVersion(raw string) ([]int, string) {
	core := raw
	pre := ""
	if idx := strings.IndexAny(raw, "-+"); idx >= 0 {
		core, pre = raw[:idx], raw[idx+1:]
	}
	parts := strings.Split(core, ".")
	nums := make([]int, 0, len(parts))
	for _, part := range parts {
		n := 0
		for _, r := range part {
			if r < '0' || r > '9' || n > 1_000_000 {
				break
			}
			n = n*10 + int(r-'0')
		}
		nums = append(nums, n)
	}
	return nums, pre
}

func versionNewer(latest, current string) bool {
	latestNums, latestPre := splitVersion(latest)
	currentNums, currentPre := splitVersion(current)
	for i := 0; i < len(latestNums) || i < len(currentNums); i++ {
		var l, c int
		if i < len(latestNums) {
			l = latestNums[i]
		}
		if i < len(currentNums) {
			c = currentNums[i]
		}
		if l != c {
			return l > c
		}
	}
	if latestPre == currentPre {
		return false
	}
	if latestPre == "" {
		return true
	}
	if currentPre == "" {
		return false
	}
	return latestPre > currentPre
}

func sameCommit(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = strings.ToLower(a), strings.ToLower(b)
	return a == b || strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func releaseOutdated(client *http.Client, local, release versionInfo) bool {
	if release.version == "" || release.version == "dev" {
		return false
	}
	if local.version != "" && local.version != "dev" {
		return versionNewer(release.version, local.version)
	}
	if local.commit == "" || release.commit == "" || client == nil || sameCommit(local.commit, release.commit) {
		return false
	}
	status, err := githubField(client,
		compareCommitAPI+url.PathEscape(local.commit)+"..."+url.PathEscape(release.commit), "status")
	if err != nil {
		return false
	}
	return status == "ahead"
}

func sanitizeField(raw string, max int) string {
	var b strings.Builder
	for _, r := range raw {
		if b.Len() >= max {
			break
		}
		switch {
		case r >= '0' && r <= '9',
			r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeVersion(raw string) string {
	return strings.TrimLeft(sanitizeField(raw, maxFieldChars), "vV")
}

func taggedVersion(raw string) string {
	version := normalizeVersion(raw)
	if version == "" || version[0] < '0' || version[0] > '9' {
		return ""
	}
	if strings.HasSuffix(version, ".") || strings.Contains(version, "..") || hasPseudoStamp(version) {
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
	sha := sanitizeField(raw, maxFieldChars)
	if len(sha) > commitChars {
		return sha[:commitChars]
	}
	return sha
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

func githubField(client *http.Client, endpoint, field string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("read github body: %w", err)
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
		client := &http.Client{
			Timeout: releaseTimeout,
			Transport: &http.Transport{
				Proxy:       http.ProxyFromEnvironment,
				DialContext: safe.PublicDialContext(&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}),
			},
		}
		tag, err := githubField(client, latestReleaseAPI, "tag_name")
		if err != nil {
			return releaseInfoMsg{err: err}
		}
		info := versionInfo{version: taggedVersion(tag)}
		if info.version == "" {
			return releaseInfoMsg{err: fmt.Errorf("latest release tag is not usable")}
		}
		sha, err := githubField(client, tagCommitAPI+url.PathEscape(sanitizeField(tag, maxFieldChars)), "sha")
		if err == nil {
			info.commit = shortCommit(sha)
		}
		return releaseInfoMsg{info: info, outdated: releaseOutdated(client, localVersionInfo(), info)}
	}
}

func mergeVersionInfo(local, release versionInfo) versionInfo {
	info := local
	fallback := release
	if info.version == "" {
		info = release
		fallback = local
	}
	if info.commit == "" {
		info.commit = fallback.commit
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
