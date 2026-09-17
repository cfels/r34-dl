<div align="center">

## 💕🌸💕 R34-DL 💕🌸💕

</div>

a rule34/safebooru downloader with alot of feature's, Why? cuz i didn't saw any good r34 downloader since R34 dropped API update's, and i also wanted to add safe option (safebooru) to this type of downloader

> [!NOTE]  
> R34 updated thier API and now u need API key, read [Obtaining API key](#obatining-api-key), what about Safebooru? well Safebooru doesn't need one so u can just jump in and use the tool (unless u want rule34 access then you'll need API key)

### Donwloading
you can download pre-built binary from [Release's](https://github.com/cfels/r34-dl/releases)

### Building
```
go get
CGO_ENABLED=0 go build -o r34-dl .
./r34-dl
```

### Usage:
```
Usage: r34-dl [options]

Options:
  -h, --help                       show this help message
  -apik, --add-api-key             store rule34 api key (prompt's u to paste ur api key)
  -b, --bulk                       download in bulk
  -l, --limit <N>                  max results to fetch (default 30)
  -t, --tags <tags>                search tags (comma-separated)
  -cls, --clear-history            clear search history
  -a, --audio                      play video audio (default: off)
  --no-audio                       mute video audio
  --filter-ai                      hide AI generated posts
  --no-filter-ai                   show AI generated posts
  --run-tests                      run tests
```

video previews are muted by default, turn audio on with `r34-dl -a` (or disable it again with `--no-audio`),
while a video plays `m` toggles audio and the choice is remembered for the next video.

### Tag predictions
while typing tags the search bar suggests booru tags: the best match is shown as dimmed text after the
cursor, `↑`/`↓` cycle through the suggestions (the line under the bar always shows the current one
first, followed by the others) and `Y` — or `→` at the end of the line — accepts the highlighted tag.
`ctrl+p`/`ctrl+n` recall search history. predictions come from the booru autocomplete endpoint and
from your own search history, so they also work offline.

### Filtering AI posts
rule34 only, since that's the site that has the toggle: the switch under the tag bar hides AI generated
posts by excluding the `ai_generated` tag from every search, and it disappears while safebooru is
active. press `ctrl+a` to flip it while searching or while looking at results (results are re-fetched),
and the choice is remembered — `r34-dl --filter-ai` / `--no-filter-ai` set it from the command line.

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

### Constributing
Feel free to constribue, i'll should accept most of the patches.

### License
This Project use's [MIT License](https://github.com/cfels/r34-dl/blob/dev/LICENSE) so make sure to follow it!
