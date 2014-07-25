# ttyrec4windows

ttyrec is a tty recorder written by [Satoru Takabayashi](http://0xcc.net/ttyrec/index.html.en). It can play back with the included ttyplay command.
However, unfortunately, windows implementation of ttyrec did not exist. So I implement ttyrec works on windows.

## Usage

Recording
```
$ ttyrec
```

Playback
```
$ ttyplay ttyrecord
```

## Requirements

* golang

## Installation

```
$ go get github.com/mattn/ttyrec4windows/ttyrec
$ go get github.com/mattn/ttyrec4windows/ttyplay
```

## Screenshot

![](https://raw.githubusercontent.com/mattn/ttyrec4windows/master/data/screenshot.gif)

## License

MIT

## Author

Yasuhiro Matsumoto (a.k.a mattn)
