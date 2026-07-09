<div align="center">

## 💕🌸💕 R34-DL 💕🌸💕

</div>

a rule34/safebooru downloader with alot of feature's, Why? cuz i didn't saw any good r34 downloader since R34 dropped API update's, and i also wanted to add safe option (safebooru) to this type of downloader

> [!NOTE]  
> R34 updated thier API and now u need API key, read [Obtaining API key](#obatining-api-key), what about Safebooru? well Safebooru doesn't need one so u can just jump in and use the tool (unless u want rule34 access then you'll need API key)

### Building
```
go build .
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
  --run-tests                      run tests
```

### Obatining API key
Here's how to get one
go to [rule34.xxx Account Page](https://rule34.xxx/index.php?page=account&s=home) to register an acc or login if you have one
then go to `Options` and find section called `API Access Credentials` here's ur api key! copy the whole block!
then type this command into your terminal 
```fish
r34-dl -apik
```
u will see prompt like this and paste full API key block into it
```
paste ur full rule34 API key (e.g. &api_key=xxx&user_id=777): 
```
ur DONE! u can access r34 api

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
Feel free to constribue, i'll should accept most patches.

### License
This Project use's [MIT License](https://github.com/cfels/r34-dl/blob/dev/LICENSE) so make sure to follow it!
