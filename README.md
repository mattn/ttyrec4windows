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
$ go get github.com/ttyrec4windows/ttyrec
$ go get github.com/ttyrec4windows/ttyplay
```

# License

MIT

# Author

Yasuhiro Matsumoto (a.k.a mattn)
