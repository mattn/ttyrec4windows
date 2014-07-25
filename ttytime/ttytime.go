package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
)

var flag_v = flag.Bool("v", false, "verbose")

func calc_time(filename string) (int, error) {
	f, err := os.Open(filename)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var start, end [3]int32

	err = binary.Read(f, binary.LittleEndian, &start)
	if err != nil {
		return 0, err
	}
	f.Seek(int64(start[2]), os.SEEK_CUR)

	if *flag_v {
		fmt.Printf("*** filename=%s, tv_sec=%d, tv_usec=%d, len=%d\n", filename, start[0], start[1], start[2])
	}

	for {
		var h [3]int32
		err = binary.Read(f, binary.LittleEndian, &h)
		if err != nil {
			break
		}
		f.Seek(int64(h[2]), os.SEEK_CUR)
		end = h

		if *flag_v {
			fmt.Printf("*** filename=%s, tv_sec=%d, tv_usec=%d, len=%d\n", filename, h[0], h[1], h[2])
		}
	}
	return int(end[0] - start[0]), nil
}

func main() {
	flag.Parse()
	for _, filename := range flag.Args() {
		n, err := calc_time(filename)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%7d	%s\n", n, filename);
	}
}
