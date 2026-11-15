// Command cases turns the shared packet compressed-float cases into inputs
// for the Goose writer driver. The shared cases remain the single value list.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fatal("usage: cases <cases.txt>")
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fatal("open cases: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) < 3 || (fields[2] != "refused" && fields[2] != "written") {
			fatal("malformed case: %q", scanner.Text())
		}
		value := parse32(fields[1])
		want := value
		if len(fields) > 3 {
			if len(fields) != 5 || fields[3] != "reads-back" {
				fatal("malformed read-back case: %q", scanner.Text())
			}
			want = parse32(fields[4])
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", fields[0], fields[2], bitsHex(value), bitsHex(want))
		count++
	}
	if err := scanner.Err(); err != nil {
		fatal("read cases: %v", err)
	}
	if count == 0 {
		fatal("no cases")
	}
}

func parse32(s string) float32 {
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		fatal("parse float32 %q: %v", s, err)
	}
	return float32(v)
}

func bitsHex(v float32) string {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
	return hex.EncodeToString(b[:])
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "packet-write-cfloat cases: "+format+"\n", args...)
	os.Exit(2)
}
