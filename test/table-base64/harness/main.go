// Exercise the public generated JSON readers and writers with an independent
// standard-library Base64 oracle. Every language consumes the same cases.
package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	b64cases "github.com/mas-bandwidth/schema/v2/test/table-base64/cases"
)

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAILED: table Base64:", err)
		os.Exit(1)
	}
}

func main() {
	if len(os.Args) < 2 {
		must(fmt.Errorf("usage: harness driver [args...]"))
	}
	cases := b64cases.Corpus()
	var input bytes.Buffer
	for _, v := range cases {
		fmt.Fprintln(&input, hex.EncodeToString([]byte(v.Text)))
	}
	cmd := exec.Command(os.Args[1], os.Args[2:]...)
	cmd.Stdin = &input
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	must(err)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 65536), 1024*1024)
	for _, v := range cases {
		if !scanner.Scan() {
			must(scanner.Err())
			must(fmt.Errorf("%s: missing result", v.Name))
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) != 5 {
			must(fmt.Errorf("%s: bad driver result %q", v.Name, scanner.Text()))
		}
		if (fields[0] == "1") != v.Malformed {
			must(fmt.Errorf("%s: malformed=%s, want %t", v.Name, fields[0], v.Malformed))
		}
		if v.Malformed {
			continue // partial values on a failed read are not this contract
		}
		if fields[1] != fmt.Sprint(v.Clamped) {
			must(fmt.Errorf("%s: clamped=%s, want %d", v.Name, fields[1], v.Clamped))
		}
		if fields[2] != fmt.Sprint(v.Mismatch) {
			must(fmt.Errorf("%s: kind mismatch=%s, want %d", v.Name, fields[2], v.Mismatch))
		}
		wantHex := hex.EncodeToString(v.Want)
		if wantHex == "" {
			wantHex = "-"
		}
		if strings.ToLower(fields[3]) != wantHex {
			must(fmt.Errorf("%s: decoded payload differs", v.Name))
		}
		text, err := hex.DecodeString(fields[4])
		must(err)
		var written struct{ Payload string }
		must(json.Unmarshal(text, &written))
		if written.Payload != base64.StdEncoding.EncodeToString(v.Want) {
			must(fmt.Errorf("%s: writer differs from canonical Base64", v.Name))
		}
	}
	if scanner.Scan() {
		must(fmt.Errorf("unexpected extra driver output: %s", scanner.Text()))
	}
	must(scanner.Err())
	fmt.Printf("table Base64: %d cases passed (bytes, padding, malformed input, clamping, canonical writer)\n", len(cases))
}
