package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	foregroundBlue      = 0x1
	foregroundGreen     = 0x2
	foregroundRed       = 0x4
	foregroundIntensity = 0x8
	backgroundBlue      = 0x10
	backgroundGreen     = 0x20
	backgroundRed       = 0x40
	backgroundIntensity = 0x80
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

var (
	procSetStdHandle               = kernel32.NewProc("SetStdHandle")
	procGetStdHandle               = kernel32.NewProc("GetStdHandle")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procSetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procGetConsoleMode             = kernel32.NewProc("GetConsoleMode")
	procFillConsoleOutputCharacter = kernel32.NewProc("FillConsoleOutputCharacterW")
	procFillConsoleOutputAttribute = kernel32.NewProc("FillConsoleOutputAttribute")
	procReadConsoleOutputCharacter = kernel32.NewProc("ReadConsoleOutputCharacterW")
	procReadConsoleOutputAttribute = kernel32.NewProc("ReadConsoleOutputAttribute")
	procGetConsoleCursorInfo       = kernel32.NewProc("GetConsoleCursorInfo")
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

type inputRecord struct {
	eventType word
	_         [2]byte
	event     [16]byte
}

type keyEventRecord struct {
	keyDown         int32
	repeatCount     word
	virtualKeyCode  word
	virtualScanCode word
	unicodeChar     wchar
	controlKeyState dword
}

type windowBufferSizeRecord struct {
	size coord
}

type mouseEventRecord struct {
	mousePos        coord
	buttonState     dword
	controlKeyState dword
	eventFlags      dword
}

type charInfo struct {
	buf []rune
	att []uint16
}

func fgToAnsi(a uint16) uint16 {
	switch a%16 {
	case 0:
		return 30
	case 1:
		return 20
	case 2:
		return 18
	case 3:
		return 22
	case 4:
		return 17
	case 5:
		return 21
	case 6:
		return 19
	case 7:
		return 23
	case 8:
		return 30
	case 9:
		return 34
	case 10:
		return 32
	case 11:
		return 36
	case 12:
		return 31
	case 13:
		return 35
	case 14:
		return 33
	case 15:
		return 37
	}
	return 30
}

func bgToAnsi(a uint16) uint16 {
	switch a/0x10 {
	case 0:
		return 40
	case 1:
		return 44
	case 2:
		return 42
	case 3:
		return 46
	case 4:
		return 41
	case 5:
		return 45
	case 6:
		return 43
	case 7:
		return 47
	case 8:
		return 40
	case 9:
		return 44
	case 10:
		return 42
	case 11:
		return 46
	case 12:
		return 41
	case 13:
		return 45
	case 14:
		return 43
	case 15:
		return 47
	}
	return 0
}

func getSize(sr smallRect) coord {
	return coord{sr.right - sr.left, sr.bottom - sr.top}
}

func writeBytes(f io.Writer, b []byte) {
	t := time.Now().UnixNano() / 1000
	binary.Write(f, binary.LittleEndian, uint32(t/1000000))
	binary.Write(f, binary.LittleEndian, uint32(t%1000000))
	binary.Write(f, binary.LittleEndian, uint32(len(b)))
	f.Write(b)
}

func record(quit chan bool, wg *sync.WaitGroup, file string) {
	defer wg.Done()

	r := syscall.Handle(os.Stdout.Fd())

	f, err := os.Create(file)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer f.Close()

	writeBytes(f, []byte("\x1b[2J"))
	//fmt.Fprintf(f, "\x1b[c\x1b%%G\x1b[f\x1b[?7l")

	var csbi consoleScreenBufferInfo
	r1, _, err := procGetConsoleScreenBufferInfo.Call(uintptr(r), uintptr(unsafe.Pointer(&csbi)))
	if r1 == 0 {
		fmt.Println(err)
		return
	}

	size := getSize(csbi.window)
	tm := time.NewTicker(10 * time.Millisecond)

	//fmt.Fprintf(f, "\x1b[8;%d;%dt\x1b[1;%dr", size.y, size.x, size.y)

	var oldsize coord
	var oldcurpos coord
	var oldcurvis bool
	var oldbuf []charInfo

loop:
	for {
		select {
		case <-quit:
			break loop
		case <-tm.C:
		}
		r1, _, err = procGetConsoleScreenBufferInfo.Call(uintptr(r), uintptr(unsafe.Pointer(&csbi)))
		if r1 == 0 {
			break loop
		}
		curpos := coord{
			x: csbi.cursorPosition.x - csbi.window.left,
			y: csbi.cursorPosition.y - csbi.window.top,
		}
		size = getSize(csbi.window)

		var cci consoleCursorInfo
		r1, _, err = procGetConsoleCursorInfo.Call(uintptr(r), uintptr(unsafe.Pointer(&cci)))
		if r1 == 0 {
			break loop
		}
		curvis := cci.visible != 0

		if size.x != oldsize.x || size.y != oldsize.y {
			oldbuf = []charInfo{}
		}

		l := uint32(size.x + 1)
		buf := make([]charInfo, size.y+1)
		var nr dword

		var bb bytes.Buffer
		for y := short(0); y < size.y+1; y++ {
			xy := coord{
				x: csbi.window.left,
				y: csbi.window.top + y,
			}
			cbbuf := make([]uint16, l)
			r1, _, err = procReadConsoleOutputCharacter.Call(uintptr(r), uintptr(unsafe.Pointer(&cbbuf[0])), uintptr(l), uintptr(*(*int32)(unsafe.Pointer(&xy))), uintptr(unsafe.Pointer(&nr)))
			if r1 == 0 {
				break loop
			}
			cb := utf16.Decode(cbbuf[:nr])
			buf[y].buf = cb

			ca := make([]uint16, l)
			r1, _, err = procReadConsoleOutputAttribute.Call(uintptr(r), uintptr(unsafe.Pointer(&ca[0])), uintptr(l), uintptr(*(*int32)(unsafe.Pointer(&xy))), uintptr(unsafe.Pointer(&nr)))
			if r1 == 0 {
				break loop
			}
			buf[y].att = ca[:nr]

			if len(oldbuf) > 0 {
				ob := oldbuf[y].buf
				oa := oldbuf[y].att
				diff := false
				for i := 0; i < len(cb); i++ {
					if cb[i] != ob[i] || ca[i] != oa[i] {
						diff = true
						break
					}
				}
				if !diff {
					continue
				}
			}
			a := uint16(0)
			fmt.Fprintf(&bb, "\x1b[%d;%dH", y+1, 1)
			for i := 0; i < len(cb); i++ {
				if a != ca[i] {
					a = ca[i]
					fmt.Fprintf(&bb, "\x1b[%d;%dm", fgToAnsi(a), bgToAnsi(a))
				}
				fmt.Fprintf(&bb, "%s", string(cb[i]))
			}
			fmt.Fprintf(&bb, "\x1b[0m")
		}
		if oldcurpos.x != curpos.x || oldcurpos.y != curpos.y {
			fmt.Fprintf(&bb, "\x1b[%d;%dH", curpos.y+1, curpos.x+1)
		}
		if oldcurvis != curvis {
			if curvis {
				fmt.Fprintf(&bb, "\x1b[>5l")
			} else {
				fmt.Fprintf(&bb, "\x1b[>5h")
			}
		}

		if bb.Len() > 0 {
			writeBytes(f, bb.Bytes())
			oldbuf = buf
			oldcurpos = curpos
			oldcurvis = curvis
			oldsize = size
		}
	}
}

func isTty() bool {
	var st uint32
	r1, _, err := procGetConsoleMode.Call(os.Stdout.Fd(), uintptr(unsafe.Pointer(&st)))
	return r1 != 0 && err != nil
}

func getStdHandle(stdhandle int32) uintptr {
	r1, _, _ := procGetStdHandle.Call(uintptr(stdhandle))
	return r1
}

func setStdHandle(stdhandle int32, handle uintptr) error {
	r1, _, err := procSetStdHandle.Call(uintptr(stdhandle), handle)
	if r1 == 0 {
		return err
	}
	return nil
}

var stdout = os.Stdout
var stdin = os.Stdin

func ttyReady() error {
	var err error
	_stdin, err := os.Open("CONIN$")
	if err != nil {
		return err
	}
	_stdout, err := os.Open("CONOUT$")
	if err != nil {
		return err
	}

	stdin = os.Stdin
	stdout = os.Stdout

	os.Stdin = _stdin
	os.Stdout = _stdout

	syscall.Stdin = syscall.Handle(os.Stdin.Fd())
	err = setStdHandle(syscall.STD_INPUT_HANDLE, uintptr(syscall.Stdin))
	if err != nil {
		return err
	}
	syscall.Stdout = syscall.Handle(os.Stdout.Fd())
	err = setStdHandle(syscall.STD_OUTPUT_HANDLE, uintptr(syscall.Stdout))
	if err != nil {
		return err
	}

	return nil
}

func ttyTerm() {
	os.Stdin = stdin
	syscall.Stdin = syscall.Handle(os.Stdin.Fd())
	setStdHandle(syscall.STD_INPUT_HANDLE, uintptr(syscall.Stdin))
	os.Stdout = stdout
	syscall.Stdout = syscall.Handle(os.Stdout.Fd())
	setStdHandle(syscall.STD_OUTPUT_HANDLE, uintptr(syscall.Stdout))
}

var flag_e = flag.String("e", os.Getenv("COMSPEC"), "command")

func main() {
	flag.Parse()

	if !isTty() {
		ttyReady()
		defer ttyTerm()
	}

	wg := new(sync.WaitGroup)
	wg.Add(1)

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt)
	go func() {
		for {
			<-sc
		}
	}()
	quit := make(chan bool)

	file := "ttyrecord"
	if flag.NArg() > 0 {
		file = flag.Arg(0)
	}
	go record(quit, wg, file)

	args := []string{os.Getenv("COMSPEC")}
	if *flag_e != os.Getenv("COMSPEC") {
		args = append(args, "/c", *flag_e)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Start()
	cmd.Wait()

	//time.Sleep(1 * time.Second)
	quit <- true
	wg.Wait()
}
