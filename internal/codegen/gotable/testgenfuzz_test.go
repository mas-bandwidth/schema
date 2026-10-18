package gotable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
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
	// The base64 text is the standard base64 of the wire (§16.8); a last
	// character whose padding bits are not zero (B for A before ==, found by
	// `go test -fuzz` in under a second, not by the corpus) reads as the same
	// wire with a silent report, because LoadText decodes with the lenient
	// alphabet rather than the strict one.
	{"FuzzWeaponConfigText", "base64_padding_bits_not_zero", hex.EncodeToString([]byte("AQAAAAAAAAAAAB=="))},
	// A JSON text is UTF-8 (RFC 8259, §16.3); an ill-formed byte 0x9e inside
	// WideBlob.label reads as U+FFFD with a silent report and re-encodes as
	// the three bytes ef bf bd.
	{"FuzzWideBlobJson", "json_string_ill_formed_utf8", hex.EncodeToString([]byte("{\n  \"label\": \"\x9e\",\n  \"payload\": \"Wg==\",\n  \"samples\": [\n    1\n  ]\n}\n"))},
}

// The pinned bounds of the generated seed corpus of tables/examples
// (docs/SPEC-TABLES.md §16.11). They are upper bounds: the corpus may shrink,
// and the gate fails the moment it grows past one, so the seeds cannot grow
// back unseen to the unbounded corpus that once reached 94 GiB RSS. Raise a
// total only on purpose, to the figure the gate logs.
const (
	fuzzPinMutationsPerTarget = 256     // at most 256 mutation seeds per target, as a literal: never tgSeedMutations
	fuzzPinSeedBytes          = 4096    // no seed over 4 KiB, as a literal: never tgSeedCeiling
	fuzzPinTotalSeeds         = 15000   // every seed of every target: 13847 when pinned
	fuzzPinTotalSeedBytes     = 6500000 // every seed's bytes: 6027309 when pinned
	fuzzPinSeedMemory         = 1 << 30 // the seed run stays under 1 GiB
)

// TestGoTestgenFuzzTargets is the gate of the generated fuzz targets
// (docs/SPEC-TABLES.md §16.11, the fuzz targets). testgen of tables/examples
// emits Fuzz<Table>Wire, Fuzz<Table>Text and Fuzz<Table>Json for every root
// table; written beside the generated unit in a t.TempDir(), `go test` runs
// each target over its bounded seed corpus, and every target RUNs and PASSes;
// the seed count and seed bytes stay inside their pins and the run's memory
// under 1 GiB. Then the test removes one bounds check from the generated reader
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
		files["fuzzseedpin_test.go"] = fuzzSeedPinSource(u)
		for _, d := range testgenFuzzDefects {
			input, err := hex.DecodeString(d.hex)
			if err != nil {
				t.Fatal(err)
			}
			files["testdata/fuzz/"+d.target+"/"+d.name] = []byte("go test fuzz v1\n[]byte(" + strconv.Quote(string(input)) + ")\n")
		}
	}

	t.Run("bounded_seeds_pass", func(t *testing.T) {
		t.Parallel()
		out, err := runGeneratedUnit(t, u, stub, defects, "-v", "-timeout", "540s", "-run", "^Fuzz|^TestFuzzSeedPin$")
		// THE MUTATIONS ARE SEEDS (§16.11): a target whose corpus holds no
		// more entries than its samples has lost its mutations
		total := 0
		for _, fn := range want {
			n := bytes.Count(out, []byte("=== RUN   "+fn+"/seed#"))
			if n <= 8 {
				t.Errorf("emitted fuzz target %s ran %d seed corpus entries: its mutations are missing", fn, n)
			}
			total += n
		}
		t.Logf("%d seed corpus entries ran across %d targets", total, len(want))
		// THE PINS (§16.11): the seed count and the seed bytes
		m := regexp.MustCompile(`(?m)^FUZZSEEDS targets=(\d+) seeds=(\d+) mutations=(\d+) bytes=(\d+) maxmutations=(\d+) maxseed=(\d+)$`).FindSubmatch(out)
		if m == nil {
			t.Fatalf("the seed pin did not report its counts:\n%s", tail(out))
		}
		num := func(i int) int { v, _ := strconv.Atoi(string(m[i])); return v }
		targets, seeds, mutations, bytesTotal, maxMut, maxSeed := num(1), num(2), num(3), num(4), num(5), num(6)
		t.Logf("seed corpus: %d targets, %d seeds (%d mutations), %d seed bytes; at most %d mutations in a target, %d bytes in a seed", targets, seeds, mutations, bytesTotal, maxMut, maxSeed)
		if targets != len(want) {
			t.Fatalf("the seed pin counted %d targets, want %d", targets, len(want))
		}
		if maxMut > fuzzPinMutationsPerTarget || maxSeed > fuzzPinSeedBytes || seeds > fuzzPinTotalSeeds || bytesTotal > fuzzPinTotalSeedBytes {
			t.Fatalf("the seed corpus grew past its pins: %d mutations in one target (pin %d), a %d-byte seed (pin %d), %d seeds (pin %d), %d seed bytes (pin %d)",
				maxMut, fuzzPinMutationsPerTarget, maxSeed, fuzzPinSeedBytes, seeds, fuzzPinTotalSeeds, bytesTotal, fuzzPinTotalSeedBytes)
		}
		if err != nil {
			t.Fatalf("emitted fuzz targets over their bounded seeds: %v\n%s\n%s", err, failures(out), tail(out))
		}
		for _, fn := range want {
			if !bytes.Contains(out, []byte("=== RUN   "+fn+"\n")) || !bytes.Contains(out, []byte("--- PASS: "+fn+" ")) {
				t.Fatalf("emitted fuzz target %s did not RUN and PASS:\n%s", fn, tail(out))
			}
		}
		t.Logf("%d emitted fuzz targets RUN and PASS over their bounded seeds", len(want))
		// THE MEMORY (§16.11): the generated package's TestMain fails the
		// run at 1 GiB; its figure is logged
		mm := regexp.MustCompile(`(?m)^FUZZSEEDMEM sys=(\d+)$`).FindSubmatch(out)
		if mm == nil {
			t.Fatalf("the seed run did not report its memory:\n%s", tail(out))
		}
		t.Logf("the seed run's runtime.MemStats.Sys after every target: %s bytes (bound %d)", mm[1], fuzzPinSeedMemory)
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
// failing corpus entries, every named testdata/fuzz entry that failed, and the
// first 20 failure messages, each naming its input.
func failures(out []byte) string {
	count := map[string]int{}
	var order, named []string
	var said []string
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if said == nil || len(said) < 20 {
			if _, msg, ok := strings.Cut(l, "_gen_test.go:"); ok && regexp.MustCompile(`^[0-9]+: (wire|text|json) `).MatchString(msg) {
				said = append(said, l)
			}
		}
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
	for _, l := range said {
		b.WriteString("failure: " + l + "\n")
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

// fuzzSeedPinSource is a test file for the generated package (§16.11): a test
// that builds every target's bounded seed corpus through fuzz<Table>Seeds and
// fuzz<File>Bound and prints the counts the pins read, and a TestMain that
// reads runtime.MemStats after every test and target has run and fails the
// run when the process holds 1 GiB or more.
func fuzzSeedPinSource(u *ir.Unit) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"runtime\"\n\t\"testing\"\n)\n\n", u.Package)
	b.WriteString("func TestMain(m *testing.M) {\n\tcode := m.Run()\n\tvar ms runtime.MemStats\n\truntime.ReadMemStats(&ms)\n\tfmt.Printf(\"FUZZSEEDMEM sys=%d\\n\", ms.Sys)\n")
	fmt.Fprintf(&b, "\tif ms.Sys >= %d {\n\t\tfmt.Printf(\"the seed run holds %%d bytes, at or past the 1 GiB bound\\n\", ms.Sys)\n\t\tcode = 1\n\t}\n\tos.Exit(code)\n}\n\n", fuzzPinSeedMemory)
	b.WriteString("func TestFuzzSeedPin(t *testing.T) {\n\tt.Parallel()\n\tvar targets, seeds, mutations, total, maxMut, maxSeed int\n\tcount := func(s [][]byte, m int) {\n\t\ttargets++\n\t\tseeds += len(s)\n\t\tmutations += m\n\t\tmaxMut = max(maxMut, m)\n\t\tfor _, x := range s {\n\t\t\ttotal += len(x)\n\t\t\tmaxSeed = max(maxSeed, len(x))\n\t\t}\n\t}\n")
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if !st.IsTable || st.IsMapEntry() {
				continue
			}
			fmt.Fprintf(&b, "\t{\n\t\twire, text, json := fuzz%sSeeds(t)\n\t\tcount(fuzz%sBound(wire))\n\t\tcount(fuzz%sBound(text))\n\t\tcount(fuzz%sBound(json))\n\t}\n", st.Name, f.Base, f.Base, f.Base)
		}
	}
	b.WriteString("\tfmt.Printf(\"FUZZSEEDS targets=%d seeds=%d mutations=%d bytes=%d maxmutations=%d maxseed=%d\\n\", targets, seeds, mutations, total, maxMut, maxSeed)\n}\n")
	return []byte(b.String())
}
