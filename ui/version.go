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
	releasesRepo     = "cfels/r34-dl"
	latestReleaseAPI = "https://api.github.com/repos/" + releasesRepo + "/releases/latest"
	tagCommitAPI     = "https://api.github.com/repos/" + releasesRepo + "/commits/"
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
)

type versionInfo struct {
	version string
	commit  string
}

type releaseInfoMsg struct {
	info versionInfo
	err  error
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
		return releaseInfoMsg{info: info}
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

func (m Model) bannerText() string {
	return titleStyle.Render(bannerASCII) + "\n" + m.versionLine()
}

func latestReleaseInfo() versionInfo {
	msg, ok := fetchReleaseInfo()().(releaseInfoMsg)
	if !ok || msg.err != nil {
		return versionInfo{}
	}
	return msg.info
}

func VersionLine() string {
	return formatVersionLine(mergeVersionInfo(localVersionInfo(), latestReleaseInfo()))
}

func VersionBanner() string {
	return titleStyle.Render(bannerASCII) + "\n" + dimStyle.Render("  "+VersionLine())
}
