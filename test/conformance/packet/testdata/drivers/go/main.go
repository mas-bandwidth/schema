// The packet carrier's Go driver (test/conformance/packet/carrier.go states
// the contract): write|read <codec> <in> <out>. The generated package sits
// beside it as ./seed; one codec per seed root.
package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/serialize.go/v2"

	"packetdriver/seed"
)

type codec struct {
	write func(fields []int64) ([]byte, bool)
	read  func(packet []byte) ([]int64, bool)
}

var codecs = map[string]codec{
	"union-before-bytes": {writeSeedT, readSeedT},
}

func writeSeedT(f []int64) ([]byte, bool) {
	var v seed.T
	at := 0
	v.U.Type = seed.UType(f[at])
	at++
	if v.U.Type == seed.UTypeA {
		v.U.A.V = uint8(f[at])
		at++
	}
	for i := range v.Data {
		v.Data[i] = uint8(f[at])
		at++
	}
	if at != len(f) {
		return nil, false
	}
	ws := serialize.NewWriteStream(make([]byte, seed.TMaxBytes))
	if seed.WriteT(ws, &v) != nil {
		return nil, false
	}
	ws.Flush()
	if ws.Err() != nil {
		return nil, false
	}
	return append([]byte(nil), ws.Data()...), true
}

func readSeedT(p []byte) ([]int64, bool) {
	var v seed.T
	if seed.ReadT(serialize.NewReadStream(p), &v) != nil {
		return nil, false
	}
	out := []int64{int64(v.U.Type)}
	if v.U.Type == seed.UTypeA {
		out = append(out, int64(v.U.A.V))
	}
	for _, b := range v.Data {
		out = append(out, int64(b))
	}
	return out, true
}

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: driver write|read <codec> <in> <out>")
		os.Exit(1)
	}
	c, ok := codecs[os.Args[2]]
	if !ok {
		fmt.Fprintf(os.Stderr, "driver: no codec %q\n", os.Args[2])
		os.Exit(1)
	}
	in, err := os.Open(os.Args[3])
	if err != nil {
		fmt.Fprintln(os.Stderr, "driver:", err)
		os.Exit(1)
	}
	defer in.Close()
	var out strings.Builder
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 1<<20), 1<<20)
	for scan.Scan() {
		line := scan.Text()
		switch os.Args[1] {
		case "write":
			var fields []int64
			for _, t := range strings.Fields(line) {
				n, err := strconv.ParseInt(t, 10, 64)
				if err != nil {
					fmt.Fprintln(os.Stderr, "driver:", err)
					os.Exit(1)
				}
				fields = append(fields, n)
			}
			if p, ok := c.write(fields); ok {
				out.WriteString(hex.EncodeToString(p) + "\n")
			} else {
				out.WriteString("refused\n")
			}
		case "read":
			p, err := hex.DecodeString(line)
			if err != nil {
				fmt.Fprintln(os.Stderr, "driver:", err)
				os.Exit(1)
			}
			if fields, ok := c.read(p); ok {
				out.WriteString("ok")
				for _, n := range fields {
					out.WriteString(" " + strconv.FormatInt(n, 10))
				}
				out.WriteString("\n")
			} else {
				out.WriteString("refused\n")
			}
		default:
			fmt.Fprintf(os.Stderr, "driver: no verb %q\n", os.Args[1])
			os.Exit(1)
		}
	}
	if err := scan.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "driver:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[4], []byte(out.String()), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "driver:", err)
		os.Exit(1)
	}
}
