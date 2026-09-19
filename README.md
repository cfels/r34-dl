<div align="center">

## 💕🌸💕 R34-DL 💕🌸💕

</div>

a rule34/safebooru downloader with alot of feature's, Why? cuz i didn't saw any good r34 downloader since R34 dropped API update's, and i also wanted to add safe option (safebooru) to this type of downloader, plus video sites (pornhub, xvideos, xhamster) in the same switcher

> [!NOTE]  
> R34 updated thier API and now u need API key, read [Obtaining API key](#obatining-api-key), what about Safebooru? well Safebooru doesn't need one so u can just jump in and use the tool (unless u want rule34 access then you'll need API key)

### Features

#### Sites

- five sources behind one switcher: `safebooru` (no key), `rule34` (API key), `pornhub`, `xvideos`, `xhamster`
- switch source mid-session with `tab` / `shift+tab`, or pin one with `--api <site>`
- adult sites are hidden from the switcher until the first-run age prompt is answered
- per-site search parsing (booru JSON/XML API, pornhub search token + media definitions, xvideos tag index + `setVideoHLS`/`setVideoUrl`, xhamster embedded JSON)

#### Search

- live tag input with a blinking cursor and full cursor editing (left/right, backspace, space)
- tag predictions merged from the active site plus local history and already-loaded results
- per-site autocomplete: safebooru/rule34 autocomplete endpoints, pornhub `search_autocomplete` with automatic search-token refresh, xvideos tag index (30 min cache), xhamster search suggestions (cached)
- debounced lookups, prefix matching, up to 6 suggestions, tab-completion and ghost-text completion
- search history persisted to config and recalled with `↑`/`↓` or `ctrl+p`/`ctrl+n`
- `-` tag exclusion supported in the query (including autocompleted negative tags)
- AI post filter on rule34 (`ctrl+a`, or `--filter-ai` / `--no-filter-ai`), implemented as a `-ai_generated` tag and shown as `[no AI]`
- result counter refreshed every 5 seconds while browsing, shown as `[position/total]` with an `(end)` marker
- paginated results with automatic next-page loading when the cursor reaches the end
- infinite-scroll list with highlighted selection, tag truncation (`+N more`) and video title/duration rows
- `/` starts a new search without leaving the list

#### Preview and playback

- `p` previews the selected post: images are fetched and scaled into a preview box, videos are streamed
- image and video rendering adapts to the terminal: kitty graphics protocol, sixel, or a unicode half-block fallback
- video playback pipes `ffmpeg` rawvideo into the TUI on the alternate screen, sized to fit the terminal with a 1.5x preview zoom
- playback runs at the source frame rate (probed with `ffprobe`, capped at 120fps) with correct aspect padding
- GIF previews loop indefinitely
- audio playback through `ffplay` with resampler quality fallback, muted by default, toggled with `m` during playback and saved to config
- video frames are dropped when the renderer falls behind, so playback stays in sync instead of lagging behind the source
- `ctrl+p` style key repeats are ignored (`p` re-triggered within ~220ms) so a held key can't open two players at once

#### Downloading

- single download from the list with `enter`, saved to `~/r34-dl_downloads`
- bulk download with `-b/--bulk`: prompts for tags and a count, then downloads with 4 concurrent workers to `./downloads`
- progressive (HTTP) and HLS (`.m3u8`, remuxed with `ffmpeg`) saving in one code path
- per-request `Referer` and session cookies for sites that require them, plus a dedicated user agent
- safe file names (site/id/filename sanitised and directory components stripped) with `.part` temp files and atomic renames
- live progress output for bulk mode and inline notices for single downloads

#### Configuration and CLI

- config stored at `<user config dir>/r34-dl/config.json` (written with `0600`) holding api key, user id, age flag, active site, audio toggle, AI filter and search history
- `-apik/--add-api-key` accepts the whole rule34 credentials block, extracts `api_key` + `user_id`, saves them and pings the API to confirm the key works
- credentials stored once and reused for every rule34 request (search, count, autocomplete)
- `-v/--version` prints the banner with build version and commit, falling back to the latest GitHub release tag/commit
- `-cls/--clear-history` wipes stored search history
- `-t/--tags <tags>` pre-fills the query and runs the search straight away on launch, `-l/--limit <N>` sets how many results each page holds
- `--run-tests` discovers every `*_test.go` package in the source tree and runs `go test -count=1` per package with timings, forwarding extra Go flags while blocking `-exec`/`-toolexec`
- the `--run-tests` child process runs with `GOENV=off` and `GOTOOLCHAIN=local`, so settings stored through `go env -w` (including `GOFLAGS`) and toolchain switching do not apply to it

#### Environment variables

| variable | default | effect |
| -------- | ------- | ------ |
| `R34_DL_MAX_DOWNLOAD_MB` | `8192` | per-file download cap in MiB, `0` disables it |
| `R34_DL_ALLOW_PRIVATE_HOSTS` | off | set to `1` to allow media urls that resolve to loopback or private addresses (needed behind proxies or split-horizon DNS that answer with internal IPs) |
| `R34_DL_NO_UPDATE_CHECK` | off | set to `1` to skip the GitHub release lookup on launch |

media urls, redirect targets and previews are limited to public http/https hosts by default, downloads stall-abort after 60s without progress, and the rule34 api key prompt reads without echoing when stdin is a terminal

#### Keyboard reference

| screen | keys | action |
| ------ | ---- | ------ |
| age gate | `←`/`→`/`h`/`l` | move between yes (rule34) and no (safebooru) |
| age gate | `enter`/`space` | confirm and continue |
| age gate | `q`/`ctrl+c` | quit |
| search | `enter` | run search |
| search | `tab` / `shift+tab` | switch site forward/back |
| search | `ctrl+a` | toggle the rule34 AI filter |
| search | `↑`/`↓` | walk search history or pick a suggestion |
| search | `ctrl+p`/`ctrl+n` | walk search history |
| search | `shift+↑`/`shift+↓`/`shift+←`/`shift+→` | cycle suggestions |
| search | `tab` / `Y` / `→` | accept suggestion or ghost completion |
| search | `esc` | leave suggestion picking, then quit |
| list | `↑`/`↓` or `k`/`j` | move selection |
| list | `pgup`/`pgdown` | jump a page of rows |
| list | `p` | preview selected post (image or video) |
| list | `enter` | download selected post |
| list | `tab` / `shift+tab` | switch site and re-search |
| list | `ctrl+a` | toggle the rule34 AI filter and re-search |
| list | `/` | new search |
| list | `q`/`ctrl+c` | quit |
| viewer/player | `m` | toggle audio while a video plays |
| viewer/player | any other key | stop and return to the list |

### Sites

| site | search | predictions | download |
| ---- | ------ | ----------- | -------- |
| safebooru | yes | yes | image/gif/vids |
| rule34 | needs an API key | yes | image/gif/vids |
| pornhub | yes | yes | video (mp4) |
| xvideos | yes | yes | video (mp4) |
| xhamster | yes | yes | video (mp4) |

video playback and video downloads use `ffmpeg`, so make sure it's installed

video previews play at the source frame rate (capped at 120fps for sanity)

### Donwloading
you can download pre-built binary from [Release's](https://github.com/cfels/r34-dl/releases)

### Building
```
go get
go build
./r34-dl
```

### Usage:
```
Usage: r34-dl [options]

Options:
  -h, --help                       show this help message
  -v, --version                    print version and commit
  -apik, --add-api-key             store rule34 api key (prompt's u to paste ur api key)
  -b, --bulk                       download in bulk
  -l, --limit <N>                  max results to fetch (default 30)
  -t, --tags <tags>                search tags (comma-separated)
  --api <site>                     site to use (safebooru, rule34, pornhub, xvideos, xhamster)
  -cls, --clear-history            clear search history
  -a, --audio                      play video audio (default: off)
  --no-audio                       mute video audio
  --filter-ai                      hide AI generated posts
  --no-filter-ai                   show AI generated posts
  --run-tests                      run tests
```

### Obatining API key
Here's how to get one
go to [rule34.xxx Account Page](https://rule34.xxx/index.php?page=account&s=home) to register an acc or login if you have an account already,
then go to `Options` and find section called `API Access Credentials` here's ur api key, copy the whole block!<br>
then type this command into your terminal:
```fish
r34-dl -apik
```
u will see prompt like this so just paste full API key block into it
```
paste ur full rule34 API key (e.g. &api_key=xxx&user_id=777): 
```
ur DONE! u can access r34-dl (rule34 option)

### Showcase
<img src="https://github.com/cfels/r34-dl/blob/dev/assets/showcase/showcase.gif?raw=true" width="600">

### Screenshots

<table border="0">
  <tr>
    <td><img src="https://raw.githubusercontent.com/cfels/r34-dl/dev/assets/images/3.png" width="400"></td>
    <td>
      <img src="https://raw.githubusercontent.com/cfels/r34-dl/dev/assets/images/2.png" width="400"><br><br>
      <img src="https://raw.githubusercontent.com/cfels/r34-dl/dev/assets/images/1.png" width="400">
    </td>
  </tr>
</table>

### Contributing
Feel free to contribute by sumbiting a **[PR](https://github.com/cfels/r34-dl/pulls)** or an **[Issue](https://github.com/cfels/r34-dl/issues)**, i will probably accept yall pull requests, or if u decide to suggest something in Issue's tab that's worth fixing or adding.

### License
This Project uses [MIT License](https://github.com/cfels/r34-dl/blob/dev/LICENSE) so make sure to follow it!
