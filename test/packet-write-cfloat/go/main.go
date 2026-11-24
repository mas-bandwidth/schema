// The Go packet Write of a compressed float (SPEC §5.1 row 10,
// PACKET-WIRE-ALGORITHM.md §2.5) over the shared cases in
// test/packet-write-cfloat/cases.txt.
//
// Go is an every-build leg: a refused value is an error from the writer in
// every build. A written case writes, reads back to its quantized value (or
// the case's reads-back value), and, when the case pins a wire, writes
// exactly those bytes. Each failure prints "FAILED: <case>: <detail>"; the
// last stdout line is the count tools/mustrun reads.
package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"cfloat"

	"github.com/mas-bandwidth/serialize.go/v2"
)

var failures int

func fail(name, format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAILED: %s: %s\n", name, fmt.Sprintf(format, args...))
	failures++
}

func value(text string) (float32, error) {
	v, err := strconv.ParseFloat(text, 32)
	return float32(v), err
}

// write writes one case's value as a CFloat, or as a RatioEndpoint.
func write(ratio bool, x float32) ([]byte, error) {
	w := serialize.NewWriteStream(make([]byte, 64))
	var err error
	if ratio {
		err = cfloat.WriteRatioEndpoint(w, &cfloat.RatioEndpoint{Ratio: x})
	} else {
		err = cfloat.WriteCFloat(w, &cfloat.CFloat{X: x})
	}
	if err != nil {
		return nil, err
	}
	w.Flush()
	return append([]byte(nil), w.Data()...), nil
}

func read(ratio bool, wire []byte) (float32, error) {
	r := serialize.NewReadStream(wire)
	if ratio {
		var out cfloat.RatioEndpoint
		err := cfloat.ReadRatioEndpoint(r, &out)
		return out.Ratio, err
	}
	var out cfloat.CFloat
	err := cfloat.ReadCFloat(r, &out)
	return out.X, err
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: packet-write-cfloat <cases.txt>")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAILED: open cases:", err)
		os.Exit(1)
	}
	defer func() { _ = f.Close() }()
	ran := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		ran++
		name := fields[0]
		if len(fields) < 3 || len(fields)%2 != 1 || (fields[2] != "refused" && fields[2] != "written") {
			fail(name, "malformed case %q", scanner.Text())
			continue
		}
		x, err := value(fields[1])
		if err != nil {
			fail(name, "value %q: %v", fields[1], err)
			continue
		}
		// [reads-back <v>] [type RatioEndpoint] [wire <hex>], in pairs
		want, ratio, wantWire, malformed := x, false, "", false
		for i := 3; i+1 < len(fields); i += 2 {
			switch {
			case fields[i] == "reads-back":
				if want, err = value(fields[i+1]); err != nil {
					malformed = true
				}
			case fields[i] == "type" && fields[i+1] == "RatioEndpoint":
				ratio = true
			case fields[i] == "wire":
				wantWire = strings.ToLower(fields[i+1])
			default:
				malformed = true
			}
		}
		if malformed {
			fail(name, "malformed modifier in %q", scanner.Text())
			continue
		}
		wire, err := write(ratio, x)
		if fields[2] == "refused" {
			if err == nil {
				fail(name, "the writer accepted a value the range refuses (%x)", wire)
			}
			continue
		}
		if err != nil {
			fail(name, "the writer refused a lawful value: %v", err)
			continue
		}
		if wantWire != "" && hex.EncodeToString(wire) != wantWire {
			fail(name, "wrote %x, want %s", wire, wantWire)
			continue
		}
		got, err := read(ratio, wire)
		if err != nil {
			fail(name, "the reader refused the writer's bytes: %v", err)
			continue
		}
		if math.Float32bits(got) != math.Float32bits(want) {
			fail(name, "read back %v, want %v", got, want)
		}
	}
	if err := scanner.Err(); err != nil {
		fail("cases", "read: %v", err)
	}
	fmt.Printf("packet-write-cfloat Go: ran %d, failed %d\n", ran, failures)
	if failures != 0 || ran == 0 {
		os.Exit(1)
	}
}
