<div align="center">

## 💕🌸💕 R34-DL 💕🌸💕

</div>

a rule34/safebooru downloader with alot of feature's, Why? cuz i didn't saw any good r34 downloader since R34 dropped API update's, and i also wanted to add safe option (safebooru) to this type of downloader

> [!NOTE]  
> R34 updated thier API and now u need API key, read [Obtaining API key](#obatining-api-key), what about Safebooru? well Safebooru doesn't need one so u can just jump in and use the tool (unless u want rule34 access then you'll need API key)

### Features
( add later )

### Donwloading
you can download pre-built binary from [Release's](https://github.com/cfels/r34-dl/releases)

### Building
```
go get
CGO_ENABLED=0 go build -o r34-dl .
./r34-dl
```

the banner pulls `ver:` and `commit:` from the latest [release](https://github.com/cfels/r34-dl/releases), falling back to the build's own version info when offline, stamp a build manually with:
```
CGO_ENABLED=0 go build -ldflags "-X moxiu/r34-dl/ui.buildVersion=v1.4 -X moxiu/r34-dl/ui.buildCommit=abc1234" -o r34-dl .
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
  -cls, --clear-history            clear search history
  -a, --audio                      play video audio (default: off)
  --no-audio                       mute video audio
  --filter-ai                      hide AI generated posts
  --no-filter-ai                   show AI generated posts
  --interpolate                    smooth videos below 60fps (default: on for small previews)
  --no-interpolate                 play videos at their own frame rate
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
