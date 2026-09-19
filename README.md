<div align="center">

## 💕🌸💕 R34-DL 💕🌸💕

</div>

a rule34/safebooru downloader with alot of feature's, Why? cuz i didn't saw any good r34 downloader since R34 dropped API update's, and i also wanted to add safe option (safebooru) to this type of downloader, plus video sites (pornhub, xvideos, xhamster) in the same switcher

> [!NOTE]  
> R34 updated thier API and now u need API key, read [Obtaining API key](#obatining-api-key), what about Safebooru? well Safebooru doesn't need one so u can just jump in and use the tool (unless u want rule34 access then you'll need API key)

### Features
* typing predictions
* 5 sites support (ph, xvids, xhamster, r34, safebooru)
* single and bulk content downloads
* image, gif and video previews rendered in terminal (`kitty` recommended for best quality and smooth playback)
* audio support for videos
* AI post filtering (r34 API only!)
* search history
* switch sites mid-session with `tab`
* age check on first launch
* and much more!

### Supported Sites

| site | search | predictions | download |
| ---- | ------ | ----------- | -------- |
| safebooru | yes | yes | image/gif/vids |
| rule34 | needs an API key | yes | image/gif/vids |
| pornhub | yes | yes | video (mp4) |
| xvideos | yes | yes | video (mp4) |
| xhamster | yes | yes | video (mp4) |

### Before install
before installing it please download these pkgs for best experience:
```
kitty
ffmpeg
```

### Donwloading
you can download pre-built binary from [Release's](https://github.com/cfels/r34-dl/releases)

build's are also pushed to the telegram channel, named after the arch they run on: `archlinux-x86_64` for `x86_64` and `archlinux-aarch64` for `aarch64`, so just compare it to `uname -m` before picking one, each post also show's the sha256 of the file and picking the wrong one only print's an `exec format error` from your shell

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
  -bl, --blacklist <tags>          hide tags from results and store them in config (comma-separated)
  -blc, --blacklist-clear          clear the tag blacklist
  --run-tests                      run tests
```

### Tag blacklist
`./r34-dl -bl "scat,big breasts"` stores those tags in ur config and every search hides them (sent as `-scat -big_breasts`, so it work's on `safebooru` and `rule34`, the video site's search engine's don't take negative tag's),
tag's u type with space's get stored with `_`, and prefixing a tag with `-` (like `-bl "-scat"`) remove's it from the blacklist, `-blc` wipe's the whole list, and the search screen show's the active blacklist under the tag input

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
