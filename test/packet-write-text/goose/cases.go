// Command cases turns the shared packet Write text cases
// (test/packet-write-text/cases.txt) into tab-separated lines for the Goose
// writer driver: name, verdict, used bytes in hex. The shared cases remain
// the single value list.
package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
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
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) != 3 {
			fatal("malformed case: %q", scanner.Text())
		}
		name := strings.TrimSpace(fields[0])
		used := strings.TrimSpace(fields[1])
		verdict := strings.TrimSpace(fields[2])
		if name == "" || (verdict != "refused" && verdict != "written") {
			fatal("malformed case: %q", scanner.Text())
		}
		out := "-" // no used bytes, as driverlib's hex_decode reads it
		if used != "-" {
			b, err := hex.DecodeString(strings.ReplaceAll(used, " ", ""))
			if err != nil || len(b) == 0 {
				fatal("case %s: used bytes: %q", name, used)
			}
			out = hex.EncodeToString(b)
		}
		fmt.Printf("%s\t%s\t%s\n", name, verdict, out)
		count++
	}
	if err := scanner.Err(); err != nil {
		fatal("read cases: %v", err)
	}
	if count == 0 {
		fatal("no cases")
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "packet-write-text cases: "+format+"\n", args...)
	os.Exit(2)
}
