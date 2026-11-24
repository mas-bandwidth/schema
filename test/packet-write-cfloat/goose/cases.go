// Command cases turns the shared packet compressed-float cases into inputs
// for the Goose writer driver. The shared cases remain the single value list.
//
// One tab-separated line per case: name, verdict, the value's float32 bits,
// the expected read-back's float32 bits (both little-endian hex), the type
// written (CFloat, RatioEndpoint or RatioPair), the expected wire in hex
// ("-" when the case pins none), and the bits the write emits before the flush as one
// byte of hex ("-" when the case pins none).
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
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) < 3 || len(fields)%2 != 1 || (fields[2] != "refused" && fields[2] != "written") {
			fatal("malformed case: %q", scanner.Text())
		}
		value := parse32(fields[1])
		want := value
		typ := "CFloat"
		wire := "-"
		bits := "-"
		for i := 3; i+1 < len(fields); i += 2 {
			switch fields[i] {
			case "reads-back":
				want = parse32(fields[i+1])
			case "type":
				if fields[i+1] != "RatioEndpoint" && fields[i+1] != "RatioPair" {
					fatal("unknown type in case: %q", scanner.Text())
				}
				typ = fields[i+1]
			case "wire":
				b, err := hex.DecodeString(fields[i+1])
				if err != nil || len(b) == 0 {
					fatal("malformed wire in case: %q", scanner.Text())
				}
				wire = hex.EncodeToString(b)
			case "bits":
				n, err := strconv.ParseUint(fields[i+1], 10, 8)
				if err != nil {
					fatal("malformed bits in case: %q", scanner.Text())
				}
				bits = fmt.Sprintf("%02x", n)
			default:
				fatal("unknown modifier in case: %q", scanner.Text())
			}
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%s\n", fields[0], fields[2], bitsHex(value), bitsHex(want), typ, wire, bits)
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
