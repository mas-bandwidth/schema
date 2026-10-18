package gotable

import (
	"bytes"
	"encoding/hex"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// testgenFuzzDefects are the reader defects the generated fuzz targets found
// on tables/examples (SPEC-TABLES §16.11): each a silent non-canonical accept,
// named by the input that shows it. They are not fixed by this card. Each is
// committed, as Go's fuzzing engine writes a failing input, under the
// generated package's testdata/fuzz in the t.TempDir() run only, so the run
// names it by file.
var testgenFuzzDefects = []struct{ target, name, hex string }{
	// The binary wire's bool payload is `0` or `1` (§3); a byte of 0xfe
	// (Patrol.active of the defaults sample, flipped) reads as true with a
	// silent report and re-encodes as 0x01.
	{"FuzzPatrolWire", "bool_payload_not_0_or_1", "010101fe020a0000c03f0301010404e8030000006f0c6f030b798065400e20a08a498122bdcfaa557f0e7f245050a215c09abcb70400000000000000"},
	// A JSON text is UTF-8 (RFC 8259, §16.3); an ill-formed byte 0x9e inside
	// WideBlob.label reads as U+FFFD with a silent report and re-encodes as
	// the three bytes ef bf bd.
	// The base64 text is the standard base64 of the wire (§16.8); a last
	// character whose padding bits are not zero (B for A before ==, found by
	// `go test -fuzz` in under a second, not by the corpus) reads as the same
	// wire with a silent report, because LoadText decodes with the lenient
	// alphabet rather than the strict one.
	{"FuzzWeaponConfigText", "base64_padding_bits_not_zero", hex.EncodeToString([]byte("AQAAAAAAAAAAAB=="))},
	{"FuzzWideBlobJson", "json_string_ill_formed_utf8", hex.EncodeToString([]byte("{\n  \"label\": \"\x9e\",\n  \"payload\": \"Wg==\",\n  \"samples\": [\n    1\n  ]\n}\n"))},
}

// TestGoTestgenFuzzTargets is the gate of the generated fuzz targets
// (docs/SPEC-TABLES.md §16.11, the fuzz targets). testgen of tables/examples
// emits Fuzz<Table>Wire, Fuzz<Table>Text and Fuzz<Table>Json for every root
// table; written beside the generated unit in a t.TempDir(), `go test` runs
// each target over its seeds and every flip of them, and every target RUNs and
// PASSes. Then the test removes one bounds check from the generated reader
// (the length check of TableReader.Body, so a damaged nested length slices past
// the buffer) and a target has to fail.
func TestGoTestgenFuzzTargets(t *testing.T) {
	t.Parallel()
	u := loadUnitPaths(t, "../../..", []string{"tables/examples"})
	tests, err := Testgen(u)
	if err != nil {
		t.Fatalf("testgen: %v", err)
	}
	have := map[string]bool{}
	for _, src := range tests {
		for _, m := range regexp.MustCompile(`(?m)^func (Fuzz\w+)\(f \*testing\.F\) \{$`).FindAllSubmatch(src, -1) {
			have[string(m[1])] = true
		}
	}
	// THREE TARGETS PER ROOT TABLE (§16.11): one per form.
	var want []string
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if !st.IsTable || st.IsMapEntry() {
				continue
			}
			for _, form := range []string{"Wire", "Text", "Json"} {
				name := "Fuzz" + st.Name + form
				if !have[name] {
					t.Errorf("testgen emitted no %s for root table %s (%s.schema)", name, st.Name, f.Base)
				}
				want = append(want, name)
			}
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	if len(have) != len(want) {
		t.Fatalf("testgen emitted %d fuzz targets, want %d (three per root table)", len(have), len(want))
	}
	for name, src := range tests {
		for _, banned := range []string{"\"time\"", "\"os\"", "math/rand", "\"net\""} {
			if bytes.Contains(src, []byte(banned)) {
				t.Fatalf("%s imports %s: a fuzz target's seeds are fixed by the declaration", name, banned)
			}
		}
	}
	stub := "package " + u.Package + "\n"
	add := func(files map[string][]byte) { maps.Copy(files, tests) }
	defects := func(files map[string][]byte) {
		add(files)
		for _, d := range testgenFuzzDefects {
			input, err := hex.DecodeString(d.hex)
			if err != nil {
				t.Fatal(err)
			}
			files["testdata/fuzz/"+d.target+"/"+d.name] = []byte("go test fuzz v1\n[]byte(" + strconv.Quote(string(input)) + ")\n")
		}
	}

	t.Run("seeds_and_flips_pass", func(t *testing.T) {
		t.Parallel()
		out, err := runGeneratedUnit(t, u, stub, defects, "-v", "-timeout", "540s", "-run", "^Fuzz")
		if err != nil {
			t.Fatalf("emitted fuzz targets over their seeds and flips: %v\n%s\n%s", err, failures(out), tail(out))
		}
		for _, fn := range want {
			if !bytes.Contains(out, []byte("=== RUN   "+fn+"\n")) || !bytes.Contains(out, []byte("--- PASS: "+fn+" ")) {
				t.Fatalf("emitted fuzz target %s did not RUN and PASS:\n%s", fn, tail(out))
			}
		}
		t.Logf("%d emitted fuzz targets RUN and PASS over their seeds and flips", len(want))
	})

	t.Run("bounds_check_removed", func(t *testing.T) {
		t.Parallel()
		runtime := u.Package
		runtime = strings.ToUpper(runtime[:1]) + runtime[1:] + "Table.go"
		const check = "\tif !ok || !r.Room(n) {\n\t\treturn TableReader{}, false\n\t}\n\tsub := r.Sub(int64(n))\n"
		out, err := runGeneratedUnit(t, u, stub, func(files map[string][]byte) {
			add(files)
			src := files[runtime]
			if bytes.Count(src, []byte(check)) != 1 {
				t.Fatalf("no single Body length check in %s to remove", runtime)
			}
			files[runtime] = bytes.Replace(src, []byte(check), []byte("\tif !ok {\n\t\treturn TableReader{}, false\n\t}\n\tsub := r.Sub(int64(n))\n"), 1)
		}, "-timeout", "540s", "-run", "^Fuzz\\w+Wire$")
		// the removed check is the one between a damaged length and a slice
		// past the buffer: a target has to fail on the panic, in Body
		if err == nil || !bytes.Contains(out, []byte("--- FAIL: Fuzz")) || !bytes.Contains(out, []byte("panic: runtime error: slice bounds out of range")) || !bytes.Contains(out, []byte("(*TableReader).Body(")) {
			t.Fatalf("a reader with its Body length check removed went unnoticed by the emitted fuzz targets: %v\n%s", err, tail(out))
		}
		t.Logf("the reader without its bounds check fails, on a panic in TableReader.Body")
	})
}

// failures summarizes a failing run: each failing target with its count of
// failing corpus entries, and every named testdata/fuzz entry that failed.
func failures(out []byte) string {
	count := map[string]int{}
	var order, named []string
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "--- FAIL: Fuzz") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(l, "--- FAIL: "), " ")
		target, entry, sub := strings.Cut(name, "/")
		if !sub {
			continue
		}
		if count[target] == 0 {
			order = append(order, target)
		}
		count[target]++
		if !strings.HasPrefix(entry, "seed#") {
			named = append(named, name)
		}
	}
	var b strings.Builder
	for _, target := range order {
		b.WriteString("failing: " + target + " (" + strconv.Itoa(count[target]) + " corpus entries)\n")
	}
	for _, name := range named {
		b.WriteString("failing named input: " + name + "\n")
	}
	return b.String()
}

// tail keeps a failing run's output readable: its last 120 lines.
func tail(out []byte) []byte {
	lines := bytes.Split(out, []byte("\n"))
	if len(lines) > 120 {
		lines = lines[len(lines)-120:]
	}
	return bytes.Join(lines, []byte("\n"))
}
