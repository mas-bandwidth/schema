// Compare generated packet readers with the pinned corpus and C++ oracle.
package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type vector struct {
	name, wire, payload          string
	bits, bound                  int
	refused, canonical, mutation bool
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAILED:", err)
		os.Exit(1)
	}
}

func packed(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), ""))
	if s == "" {
		return "-"
	}
	return s
}

func corpus(path string, wide bool) []vector {
	f, err := os.Open(path)
	must(err)
	defer func() { must(f.Close()) }()
	var all []vector
	v := vector{}
	flush := func() {
		if v.name != "" && ((!wide && v.bound == 16) || (wide && (v.bound == 8 || v.bound == 5))) {
			all = append(all, v)
		}
		v = vector{}
	}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, rest, _ := strings.Cut(line, " ")
		switch key {
		case "name":
			v.name = rest
		case "param":
			_, value, ok := strings.Cut(rest, "=")
			if !ok {
				must(fmt.Errorf("bad param: %s", line))
			}
			v.bound, err = strconv.Atoi(strings.TrimSpace(value))
			must(err)
		case "bytes":
			v.wire = packed(rest)
		case "expect":
			v.refused = rest == "refused"
			if !v.refused {
				_, value, ok := strings.Cut(rest, "=")
				if !ok {
					must(fmt.Errorf("bad expect: %s", line))
				}
				v.payload = packed(value)
			}
		case "consumed":
			v.bits, err = strconv.Atoi(rest)
			must(err)
		case "writer":
			v.canonical = rest == "canonical"
		}
	}
	must(s.Err())
	flush()
	if len(all) < 21 {
		must(fmt.Errorf("missing narrow corpus: only %d rows", len(all)))
	}
	return all
}

func run(command []string, input string, count int) []string {
	if len(command) == 0 {
		must(fmt.Errorf("missing driver command"))
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		must(fmt.Errorf("driver %s: %w\n%s", command[0], err, stderr.String()))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != count {
		must(fmt.Errorf("driver %s returned %d rows, want %d: %s", command[0], len(lines), count, stderr.String()))
	}
	return lines
}

func main() {
	oracle := flag.String("oracle", "build/packet-text/cpp/driver", "C++ oracle executable")
	path := flag.String("corpus", "testdata/conformance/text/string.txt", "shared string corpus")
	mutationsOnly := flag.Bool("mutations-only", false, "compare only bit-flip cases (negative control)")
	wide := flag.Bool("wide", false, "wide corpus: UTF-16 groups at bounds 7 and 4")
	flag.Parse()
	base := corpus(*path, *wide)
	if *wide {
		// The six cross-language cases in SPEC §4.12, independently packed
		// one bit at a time so the oracle must reproduce their groups too.
		for i, units := range [][]uint16{{}, {0x043c, 0x0438, 0x0440}, {0xe000}, {0xffff}, {0x41, 0xd83d, 0xde00, 0x42}, {0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67}} {
			bits := 3 + 32*len(units)
			wire := make([]byte, (bits+7)/8)
			wire[0] = byte(len(units))
			var payload strings.Builder
			for j, unit := range units {
				fmt.Fprintf(&payload, "%04x", unit)
				for k := 0; k < 16; k++ {
					if unit&(1<<k) != 0 {
						offset := 3 + 32*j + k
						wire[offset/8] |= 1 << (offset % 8)
					}
				}
			}
			base = append(base, vector{name: fmt.Sprintf("wstring-interop-%d", i), wire: hex.EncodeToString(wire), payload: packed(payload.String()), bits: bits, bound: 8, canonical: true})
		}
	}
	all := append([]vector{}, base...)
	for _, v := range base {
		if v.refused {
			continue
		}
		raw, err := hex.DecodeString(v.wire)
		must(err)
		for bit := 0; bit < len(raw)*8; bit++ {
			mutant := append([]byte{}, raw...)
			mutant[bit/8] ^= 1 << (bit % 8)
			all = append(all, vector{name: fmt.Sprintf("%s/flip-%d", v.name, bit), wire: hex.EncodeToString(mutant), bound: v.bound, mutation: true})
		}
	}
	var input strings.Builder
	for _, v := range all {
		if *wide {
			fmt.Fprintf(&input, "%d ", v.bound-1)
		}
		fmt.Fprintln(&input, v.wire)
	}
	want := run([]string{*oracle}, input.String(), len(all))
	refusals := 0
	for i, v := range all {
		if v.mutation {
			if want[i] == "REFUSE" {
				refusals++
			}
			continue
		}
		expected := "REFUSE"
		if !v.refused {
			expected = fmt.Sprintf("OK %d %s %d %s", v.bits, v.payload, v.bits, v.wire)
			if !v.canonical {
				must(fmt.Errorf("accepted corpus row %s lacks canonical writer pin", v.name))
			}
		}
		if want[i] != expected {
			must(fmt.Errorf("oracle C++ disagrees with pinned corpus on %s: got %q, want %q", v.name, want[i], expected))
		}
	}
	if refusals == 0 {
		must(fmt.Errorf("bit-flip sweep found no refusals"))
	}
	got := run(flag.Args(), input.String(), len(all))
	for i, v := range all {
		if *mutationsOnly && !v.mutation {
			continue
		}
		if got[i] != want[i] {
			must(fmt.Errorf("packet-text verdict on %s: got %q, C++ %q", v.name, got[i], want[i]))
		}
	}
	fmt.Printf("packet text: %d pinned rows and %d bit flips agree with C++; %d mutated refusals\n", len(base), len(all)-len(base), refusals)
}
