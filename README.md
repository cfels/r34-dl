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
* bulk download mode with `-b` (asks how many posts to grab, then walks the result pages)
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
you can download pre-built binary from [Release's](https://github.com/cfels/r34-dl/releases) <br>
or if u feeling like getting the latest dev build then download it off the TG channel: [t.me/r34_dl](https://t.me/r34_dl)

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
  -b, --bulk                       bulk download mode (asks how many posts to grab)
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

### Bulk downloads
`-b` start's bulk mode: `./r34-dl -b` open's the normal browser but the result's screen show's a `[bulk]` tag next to the site label,
in bulk mode `enter` open's the bulk screen: ur tag's are pre-filled, u can edit them or `tab` to another site (same switcher as the search screen) and set how many post's u want (empty = ur result page size, so `-l 50` mean's 50),
`enter` on the count start's the download: it walk's the result page's until it has that many post's and save's them with the same downloader the plain `-b` CLI flow use's, and the screen show's every line the old flow printed (`saved #123 -> path`, `failed #124: ...`, `done: X saved, Y failed`),
that screen wait's for every download to finish before it let's u go back (any key after it's done), so a `q` in the middle just get's ignored,
when stdin isnt a terminal (so script's and pipe's keep working) `-b` stay's the CLI flow and ask's for the tag's + the count in the terminal

### Result counter
the counter under the result's is `[where u are / what the website show's]`, so `remielle_dan` show's `[30/2789]` and `jane_doe_(zenless_zone_zero)` show's `[30/9810]` (`safebooru` and `rule34` ask their tag endpoint for the tag's post count, the video site's read the count their search page print's),
combining tag's count's the combined total from the post endpoint (`remielle_dan video` = `151`), and the AI filter/blacklist only change what u browse, never that number

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
<img src="https://github.com/cfels/r34-dl/blob/release/assets/showcase/preview.gif" width="855">

### Screenshots

<table border="0">
  <tr>
    <td><img src="https://github.com/cfels/r34-dl/blob/e44ab4d6401fe3c54a12264b568df452cbbda881/assets/images/remielle.png" width="460"></td>
    <td>
      <img src="https://github.com/cfels/r34-dl/blob/e44ab4d6401fe3c54a12264b568df452cbbda881/assets/images/img_result.png" width="400"><br><br>
      <img src="https://github.com/cfels/r34-dl/blob/release/assets/images/ph.png" width="400"><br><br>
      <img src="https://github.com/cfels/r34-dl/blob/e44ab4d6401fe3c54a12264b568df452cbbda881/assets/images/r34_main.png" width="400">
    </td>
  </tr>
</table>

### Contributing
Feel free to contribute by sumbiting a **[PR](https://github.com/cfels/r34-dl/pulls)** or an **[Issue](https://github.com/cfels/r34-dl/issues)**, i will probably accept yall pull requests, or if u decide to suggest something in Issue's tab that's worth fixing or adding.

### License
This Project uses [MIT License](https://github.com/cfels/r34-dl/blob/dev/LICENSE) so make sure to follow it!
