package main

import (
	"bytes"
	"code.google.com/p/mahonia"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	foregroundBlue      = 0x1
	foregroundGreen     = 0x2
	foregroundRed       = 0x4
	foregroundIntensity = 0x8
	foregroundMask      = (foregroundRed | foregroundBlue | foregroundGreen | foregroundIntensity)
	backgroundBlue      = 0x10
	backgroundGreen     = 0x20
	backgroundRed       = 0x40
	backgroundIntensity = 0x80
	backgroundMask      = (backgroundRed | backgroundBlue | backgroundGreen | backgroundIntensity)
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

var (
	procGetStdHandle               = kernel32.NewProc("GetStdHandle")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procGetConsoleCursorInfo       = kernel32.NewProc("GetConsoleCursorInfo")
	procSetConsoleCursorPosition   = kernel32.NewProc("SetConsoleCursorPosition")
	procFillConsoleOutputCharacter = kernel32.NewProc("FillConsoleOutputCharacterW")
	procFillConsoleOutputAttribute = kernel32.NewProc("FillConsoleOutputAttribute")
	procSetConsoleTextAttribute    = kernel32.NewProc("SetConsoleTextAttribute")
	procScrollConsoleScreenBuffer  = kernel32.NewProc("ScrollConsoleScreenBufferW")
)

type wchar uint16
type short int16
type dword uint32
type word uint16

type coord struct {
	x short
	y short
}

type smallRect struct {
	left   short
	top    short
	right  short
	bottom short
}

type consoleScreenBufferInfo struct {
	size              coord
	cursorPosition    coord
	attributes        word
	window            smallRect
	maximumWindowSize coord
}

type consoleCursorInfo struct {
	size    dword
	visible int32
}

type charInfo struct {
	unicodeChar wchar
	attributes  word
}

var log *os.File

func debug(s string) {
	if log == nil {
		log, _ = os.Create("debug.log")
	}
	log.WriteString(s + "\n")
	log.Sync()
}

var (
	flag_s = flag.Float64("s", 1.0, "speed")
	flag_n = flag.Bool("n", false, "no wait")
	flag_e = flag.String("e", "utf-8", "encoding")
	flag_d = flag.Bool("d", false, "debug")
)

func main() {
	flag.Parse()

	var f *os.File
	var err error

	switch flag.NArg() {
	case 0:
		f = os.Stdin
	case 1:
		f, err = os.Open(flag.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
	default:
		flag.Usage()
		os.Exit(1)
	}

	dec := mahonia.NewDecoder(*flag_e)
	if dec == nil {
		fmt.Fprintln(os.Stderr, "Unknown encoding name")
		os.Exit(1)
	}

	t := uint32(0)

	out := syscall.Handle(os.Stdout.Fd())

	var csbi consoleScreenBufferInfo
	procGetConsoleScreenBufferInfo.Call(uintptr(out), uintptr(unsafe.Pointer(&csbi)))
	attr_old := csbi.attributes
	defer func() {
		procSetConsoleTextAttribute.Call(uintptr(out), uintptr(attr_old))
	}()

	timer := time.NewTimer(0)

	quit := make(chan bool)
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt)
	go func() {
		<-sc
		timer.Stop()
		quit<-true
	}()

	var lastbuf bytes.Buffer
	var scroll *smallRect

loop:
	for {
		var header [3]uint32
		err = binary.Read(f, binary.LittleEndian, &header)
		if err != nil {
			break
		}

		if !*flag_n {
			cur := header[0]*1000000 + header[1]
			if t != 0 {
				timer.Reset(time.Duration(float64(cur-t) / *flag_s) * time.Microsecond)
				select {
				case <-timer.C:
				case <-quit:
					break loop
				}
			}
			t = cur
		}

		var data []byte
		off := 0
		if lastbuf.Len() > 0 {
			data = make([]byte, int(header[2])+lastbuf.Len())
			copy(data[:lastbuf.Len()], lastbuf.Bytes())
			off += lastbuf.Len()
			lastbuf.Reset()
		} else {
			data = make([]byte, header[2])
		}
		for off < int(header[2]) {
			n, err := f.Read(data[off:])
			if err != nil {
				break
			}
			off += n
		}

		er := dec.NewReader(bytes.NewBuffer(data))
		parse: for {
			r1, _, err := procGetConsoleScreenBufferInfo.Call(uintptr(out), uintptr(unsafe.Pointer(&csbi)))
			if r1 == 0 {
				break loop
			}

			c1, _, err := er.ReadRune()
			if err != nil {
				break parse
			}
			if c1 != 0x1b {
				switch {
				case c1 == 0x08:
					if csbi.cursorPosition.x > 0 {
						csbi.cursorPosition.x -= 1
					}
					r1, _, _ := procSetConsoleCursorPosition.Call(uintptr(out), uintptr(*(*int32)(unsafe.Pointer(&csbi.cursorPosition))))
					if r1 == 0 {
						break loop
					}
				case c1 == 0x0a:
					if scroll != nil && csbi.cursorPosition.y == scroll.bottom {
						var ci charInfo
						ci.unicodeChar = ' '
						ci.attributes = csbi.attributes
						move := scroll
						move.top++
						xy := coord{
							x: 0,
							y: scroll.top,
						}
						r1, _, _ = procScrollConsoleScreenBuffer.Call(uintptr(out), uintptr(unsafe.Pointer(&move)), 0, uintptr(*(*int32)(unsafe.Pointer(&xy))), uintptr(unsafe.Pointer(&ci)))
						if r1 == 0 {
							break loop
						}
					} else if csbi.cursorPosition.y < csbi.window.bottom {
						csbi.cursorPosition.y++
						r1, _, _ := procSetConsoleCursorPosition.Call(uintptr(out), uintptr(*(*int32)(unsafe.Pointer(&csbi.cursorPosition))))
						if r1 == 0 {
							break loop
						}
					} else {
						fmt.Print(string(c1))
					}
				case c1 == '\r' || c1 == '\t' || c1 >= 0x20:
					if *flag_d {
						debug("OUT:" + string(c1))
					}
					fmt.Print(string(c1))
				}
				continue
			}
			c2, _, err := er.ReadRune()
			if err != nil {
				lastbuf.WriteRune(c1)
				break parse
			}

			var buf bytes.Buffer
			var m rune
			switch c2 {
			case 0x5b:
				for {
					c, _, err := er.ReadRune()
					if err != nil {
						lastbuf.WriteRune(c1)
						lastbuf.WriteByte(0x5b)
						lastbuf.Write(buf.Bytes())
						break parse
					}
					if ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || c == '@' {
						m = c
						break
					}
					buf.Write([]byte(string(c)))
				}
			case 0x5d:
				for {
					c, _, err := er.ReadRune()
					if err != nil {
						lastbuf.Write(buf.Bytes())
						break parse
					}
					if c == ';' {
						break
					}
					buf.Write([]byte(string(c)))
				}
				continue
			}

			if *flag_d {
				debug("ESC:" + buf.String() + string(m))
			}
			var n int
			switch m {
			case 'h':
				if _, err := fmt.Sscanf(buf.String(), "%d", &n); err != nil {
					switch n {
					case 47:
						xy := coord{
							y: csbi.window.top,
							x: csbi.window.left,
						}
						procSetConsoleCursorPosition.Call(uintptr(out), uintptr(*(*int32)(unsafe.Pointer(&xy))))
					}
				}
			case '@':
				if _, err := fmt.Sscanf(buf.String(), "%d", &n); err != nil {
					n = 1
				}
				var ci charInfo
				ci.unicodeChar = ' '
				ci.attributes = csbi.attributes
				var move smallRect
				move.top = csbi.cursorPosition.y
				move.bottom = move.top
				move.left = csbi.cursorPosition.x
				move.right = csbi.size.x - short(n)
				xy := coord{
					x: csbi.cursorPosition.x + short(n),
					y: csbi.cursorPosition.y,
				}
				r1, _, _ = procScrollConsoleScreenBuffer.Call(uintptr(out), uintptr(unsafe.Pointer(&move)), 0, uintptr(*(*int32)(unsafe.Pointer(&xy))), uintptr(unsafe.Pointer(&ci)))
				if r1 == 0 {
					break loop
				}
				r1, _, _ = procSetConsoleCursorPosition.Call(uintptr(out), uintptr(*(*int32)(unsafe.Pointer(&csbi.cursorPosition))))
				if r1 == 0 {
					break loop
				}
			case 'm':
				attr := csbi.attributes
				cs := buf.String()
				if cs == "" {
					procSetConsoleTextAttribute.Call(uintptr(out), uintptr(attr_old))
					continue
				}
				for _, ns := range strings.Split(cs, ";") {
					if n, err = strconv.Atoi(ns); err == nil {
						switch {
						case n == 0 || n == 100:
							attr = attr_old
						case 1 <= n && n <= 5:
							attr |= foregroundIntensity
						case n == 7:
							attr = ((attr & foregroundMask) << 4) | ((attr & backgroundMask) >> 4)
						case 22 == n || n == 25 || n == 25:
							attr |= foregroundIntensity
						case n == 27:
							attr = ((attr & foregroundMask) << 4) | ((attr & backgroundMask) >> 4)
						case 30 <= n && n <= 37:
							attr = (attr & backgroundMask)
							if (n-30)&1 != 0 {
								attr |= foregroundRed
							}
							if (n-30)&2 != 0 {
								attr |= foregroundGreen
							}
							if (n-30)&4 != 0 {
								attr |= foregroundBlue
							}
						case 40 <= n && n <= 47:
							attr = (attr & foregroundMask)
							if (n-40)&1 != 0 {
								attr |= backgroundRed
							}
							if (n-40)&2 != 0 {
								attr |= backgroundGreen
							}
							if (n-40)&4 != 0 {
								attr |= backgroundBlue
							}
						}
						procSetConsoleTextAttribute.Call(uintptr(out), uintptr(attr))
					}
				}
			case 'A':
				ns, _ := fmt.Sscanf(buf.String(), "%d", &n)
				if ns == 0 {
					csbi.cursorPosition.y--
				} else {
					csbi.cursorPosition.y -= short(n)
				}
				r1, _, _ = procSetConsoleCursorPosition.Call(uintptr(out), uintptr(*(*int32)(unsafe.Pointer(&csbi.cursorPosition))))
				if r1 == 0 {
					break loop
				}
			case 'B':
				ns, _ := fmt.Sscanf(buf.String(), "%d", &n)
				if ns == 0 {
					csbi.cursorPosition.y++
				} else {
					csbi.cursorPosition.y += short(n)
				}
				r1, _, _ = procSetConsoleCursorPosition.Call(uintptr(out), uintptr(*(*int32)(unsafe.Pointer(&csbi.cursorPosition))))
				if r1 == 0 {
					break loop
				}
			case 'C':
				ns, _ := fmt.Sscanf(buf.String(), "%d", &n)
				if ns == 0 {
					csbi.cursorPosition.x++
				} else {
					csbi.cursorPosition.x += short(n)
				}
				r1, _, _ = procSetConsoleCursorPosition.Call(uintptr(out), uintptr(*(*int32)(unsafe.Pointer(&csbi.cursorPosition))))
				if r1 == 0 {
					break loop
				}
			case 'D':
				ns, _ := fmt.Sscanf(buf.String(), "%d", &n)
				if ns == 0 {
					csbi.cursorPosition.x--
				} else {
					csbi.cursorPosition.x -= short(n)
				}
				r1, _, _ = procSetConsoleCursorPosition.Call(uintptr(out), uintptr(*(*int32)(unsafe.Pointer(&csbi.cursorPosition))))
				if r1 == 0 {
					break loop
				}
			case 'J':
				if _, err = fmt.Sscanf(buf.String(), "%d", &n); err != nil {
					n = 0
				}
				switch n {
				case 0:
					cursor := coord{
						x: csbi.cursorPosition.x,
						y: csbi.cursorPosition.y,
					}
					var count, w dword
					count = dword(csbi.size.x - csbi.cursorPosition.x + (csbi.size.y-csbi.cursorPosition.y)*csbi.size.x)
					r1, _, _ = procFillConsoleOutputCharacter.Call(uintptr(out), uintptr(' '), uintptr(count), *(*uintptr)(unsafe.Pointer(&cursor)), uintptr(unsafe.Pointer(&w)))
					if r1 == 0 {
						break loop
					}
					r1, _, _ = procFillConsoleOutputAttribute.Call(uintptr(out), uintptr(csbi.attributes), uintptr(count), *(*uintptr)(unsafe.Pointer(&cursor)), uintptr(unsafe.Pointer(&w)))
					if r1 == 0 {
						break loop
					}
				case 1:
					cursor := coord{
						x: csbi.window.left,
						y: csbi.window.top,
					}
					var count, w dword
					count = dword(csbi.cursorPosition.x + (csbi.cursorPosition.y-1)*csbi.size.x)
					r1, _, _ = procFillConsoleOutputCharacter.Call(uintptr(out), uintptr(' '), uintptr(count), *(*uintptr)(unsafe.Pointer(&cursor)), uintptr(unsafe.Pointer(&w)))
					if r1 == 0 {
						break loop
					}
					r1, _, _ = procFillConsoleOutputAttribute.Call(uintptr(out), uintptr(csbi.attributes), uintptr(count), *(*uintptr)(unsafe.Pointer(&cursor)), uintptr(unsafe.Pointer(&w)))
					if r1 == 0 {
						break loop
					}
				case 2:
					cursor := coord{
						x: csbi.window.left,
						y: csbi.window.top,
					}
					var count, w dword
					count = dword(csbi.size.x * csbi.size.y)
					r1, _, _ = procFillConsoleOutputCharacter.Call(uintptr(out), uintptr(' '), uintptr(count), *(*uintptr)(unsafe.Pointer(&cursor)), uintptr(unsafe.Pointer(&w)))
					if r1 == 0 {
						break loop
					}
					r1, _, _ = procFillConsoleOutputAttribute.Call(uintptr(out), uintptr(csbi.attributes), uintptr(count), *(*uintptr)(unsafe.Pointer(&cursor)), uintptr(unsafe.Pointer(&w)))
					if r1 == 0 {
						break loop
					}
				}
			case 'K':
				fmt.Sscanf(buf.String(), "%d", &n)
				switch n {
				case 0:
					cursor := coord{
						x: csbi.cursorPosition.x,
						y: csbi.cursorPosition.y,
					}
					var count, w dword
					count = dword(csbi.size.x - csbi.cursorPosition.x)
					r1, _, _ = procFillConsoleOutputCharacter.Call(uintptr(out), uintptr(' '), uintptr(count), *(*uintptr)(unsafe.Pointer(&cursor)), uintptr(unsafe.Pointer(&w)))
					if r1 == 0 {
						break loop
					}
					r1, _, _ = procFillConsoleOutputAttribute.Call(uintptr(out), uintptr(csbi.attributes), uintptr(count), *(*uintptr)(unsafe.Pointer(&cursor)), uintptr(unsafe.Pointer(&w)))
					if r1 == 0 {
						break loop
					}
				case 1:
					cursor := coord{
						x: csbi.window.left,
						y: csbi.window.top + csbi.cursorPosition.y,
					}
					var count, w dword
					count = dword(csbi.cursorPosition.x)
					r1, _, _ = procFillConsoleOutputCharacter.Call(uintptr(out), uintptr(' '), uintptr(count), *(*uintptr)(unsafe.Pointer(&cursor)), uintptr(unsafe.Pointer(&w)))
					if r1 == 0 {
						break loop
					}
					r1, _, _ = procFillConsoleOutputAttribute.Call(uintptr(out), uintptr(csbi.attributes), uintptr(count), *(*uintptr)(unsafe.Pointer(&cursor)), uintptr(unsafe.Pointer(&w)))
					if r1 == 0 {
						break loop
					}
				case 2:
					cursor := coord{
						x: csbi.window.left,
						y: csbi.window.top + csbi.cursorPosition.y,
					}
					var count, w dword
					count = dword(csbi.size.x)
					r1, _, _ = procFillConsoleOutputCharacter.Call(uintptr(out), uintptr(' '), uintptr(count), *(*uintptr)(unsafe.Pointer(&cursor)), uintptr(unsafe.Pointer(&w)))
					if r1 == 0 {
						break loop
					}
					r1, _, _ = procFillConsoleOutputAttribute.Call(uintptr(out), uintptr(csbi.attributes), uintptr(count), *(*uintptr)(unsafe.Pointer(&cursor)), uintptr(unsafe.Pointer(&w)))
					if r1 == 0 {
						break loop
					}
				}
			case 'H':
				var xy coord
				ns, _ := fmt.Sscanf(buf.String(), "%d;%d", &xy.y, &xy.x)
				if ns == 1 {
					xy.y--
				} else if ns == 2 {
					xy.y--
					xy.x--
				}
				xy.y += csbi.window.top
				xy.x += csbi.window.left
				procSetConsoleCursorPosition.Call(uintptr(out), uintptr(*(*int32)(unsafe.Pointer(&xy))))
			case 'r':
				scroll = &smallRect{}
				ns, _ := fmt.Sscanf(buf.String(), "%d;%d", &scroll.top, &scroll.left)
				scroll.left = csbi.window.left
				scroll.right = csbi.window.right
				if ns == 0 {
					scroll = nil
				} else if ns == 1 {
					scroll.top--
				} else if ns == 2 {
					scroll.bottom--
				}
			}
		}
	}
}
