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
)

type vector struct {
	name, text string
	want       []byte
	malformed  bool
	clamped    int
	mismatch   int
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAILED: table Base64:", err)
		os.Exit(1)
	}
}

func corpus() []vector {
	var cases []vector
	add := func(name, encoded string, want []byte, malformed bool) {
		clamped := 0
		if len(want) > 16384 {
			want, clamped = want[:16384], 1
		}
		cases = append(cases, vector{name, `{"payload":"` + encoded + `"}`, want, malformed, clamped, 0})
	}
	lengths := []int{511, 512, 513, 4095, 4096, 4097, 16383, 16384, 16385, 16400}
	for n := 0; n <= 257; n++ {
		lengths = append(lengths, n)
	}
	for _, n := range lengths {
		payload := make([]byte, n)
		for i := range payload {
			payload[i] = byte(i*73 + n)
		}
		encoded := base64.StdEncoding.EncodeToString(payload)
		add(fmt.Sprintf("length-%d", n), encoded, payload, false)
		// Existing readers accept omitted and interspersed padding. Keep
		// that behavior while changing their alphabet lookup.
		add(fmt.Sprintf("unpadded-%d", n), strings.TrimRight(encoded, "="), payload, false)
	}
	for _, v := range []struct{ text, want string }{
		{"/w==", "\xff"}, {"//==", "\xff"},
	} {
		add("padding-"+v.text, v.text, []byte(v.want), false)
	}
	// Under SPEC-TABLES §16.2 and Issue #715, mid-string padding, lone padding,
	// and lone symbols cannot form valid bytes and must report kind_mismatch.
	for _, text := range []string{
		"=", "Q", "Q=Q==", "Q=Q=A=A=", "QQ==Q",
	} {
		add("invalid-padding-"+text, text, nil, false)
		cases[len(cases)-1].mismatch = 1
	}
	// Every alphabet symbol occupies each of the four positions; the oracle
	// deliberately uses the standard decoder rather than a copied lookup.
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	for i := range alphabet {
		for pos := range 4 {
			quad := []byte("AAAA")
			quad[pos] = alphabet[i]
			want, err := base64.StdEncoding.DecodeString(string(quad))
			must(err)
			add(fmt.Sprintf("symbol-%d-at-%d", i, pos), string(quad), want, false)
		}
	}
	// Raw bytes include NUL, high-bit values, whitespace and JSON escapes.
	// None is a Base64 symbol. They must report a kind mismatch (or JSON
	// damage for the closing quote), never index
	// outside the table or turn the NUL alphabet terminator into a symbol.
	for c := range 256 {
		if strings.IndexByte(alphabet, byte(c)) >= 0 || c == '=' {
			continue
		}
		add(fmt.Sprintf("invalid-%02x", c), "QQ"+string([]byte{byte(c)})+"AA", nil, c == '"')
		if c != '"' {
			cases[len(cases)-1].mismatch = 1
		}
	}
	return cases
}

func main() {
	if len(os.Args) < 2 {
		must(fmt.Errorf("usage: harness driver [args...]"))
	}
	cases := corpus()
	var input bytes.Buffer
	for _, v := range cases {
		fmt.Fprintln(&input, hex.EncodeToString([]byte(v.text)))
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
			must(fmt.Errorf("%s: missing result", v.name))
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) != 5 {
			must(fmt.Errorf("%s: bad driver result %q", v.name, scanner.Text()))
		}
		if (fields[0] == "1") != v.malformed {
			must(fmt.Errorf("%s: malformed=%s, want %t", v.name, fields[0], v.malformed))
		}
		if v.malformed {
			continue // partial values on a failed read are not this contract
		}
		if fields[1] != fmt.Sprint(v.clamped) {
			must(fmt.Errorf("%s: clamped=%s, want %d", v.name, fields[1], v.clamped))
		}
		if fields[2] != fmt.Sprint(v.mismatch) {
			must(fmt.Errorf("%s: kind mismatch=%s, want %d", v.name, fields[2], v.mismatch))
		}
		wantHex := hex.EncodeToString(v.want)
		if wantHex == "" {
			wantHex = "-"
		}
		if strings.ToLower(fields[3]) != wantHex {
			must(fmt.Errorf("%s: decoded payload differs", v.name))
		}
		text, err := hex.DecodeString(fields[4])
		must(err)
		var written struct{ Payload string }
		must(json.Unmarshal(text, &written))
		if written.Payload != base64.StdEncoding.EncodeToString(v.want) {
			must(fmt.Errorf("%s: writer differs from canonical Base64", v.name))
		}
	}
	if scanner.Scan() {
		must(fmt.Errorf("unexpected extra driver output: %s", scanner.Text()))
	}
	must(scanner.Err())
	fmt.Printf("table Base64: %d cases passed (bytes, padding, malformed input, clamping, canonical writer)\n", len(cases))
}
