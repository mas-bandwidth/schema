package goosetable

// THE ANNOUNCEMENT'S ID TABLE IS A FILE'S (docs/SPEC-TABLES.md §3.3: the
// announcement is a form-1 file; §3, THE DISTINCTNESS CHECK IS BOUNDED WORK):
// every connection golden with its id table grown to exactly 4096 distinct
// entries, to 4097, and to 4096 with one repeat, read by announce_read and
// held to the Go reference (tablewire.Vocabulary.AnnounceRead) verdict for
// verdict. The appended entries are named by no reference, so the 4096 table
// announces as the golden does, and the other two are malformed. The control
// widens the Goose bound by one and requires exactly the 4097 cases red.

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// announceGrown is an announcement whose id table is padded to n entries with
// ids it does not carry (splitmix64 of a counter); a repeat makes the last
// entry the first's.
func announceGrown(t *testing.T, golden []byte, n int, repeat bool) []byte {
	t.Helper()
	k := int(binary.LittleEndian.Uint64(golden[len(golden)-8:]))
	have := map[uint64]bool{}
	for i := range k {
		have[binary.LittleEndian.Uint64(golden[len(golden)-8-8*(k-i):])] = true
	}
	out := slices.Clone(golden[:len(golden)-8])
	for x := uint64(1); k < n; x++ {
		z := x * 0x9E3779B97F4A7C15
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		z ^= z >> 31
		if have[z] || z >= 0xFFFFFFFFFFFFFFFD {
			continue
		}
		have[z] = true
		out = binary.LittleEndian.AppendUint64(out, z)
		k++
	}
	if repeat {
		first := len(out) - 8*n
		copy(out[len(out)-8:], out[first:first+8])
	}
	return binary.LittleEndian.AppendUint64(out, uint64(n))
}

func announceIdBoundCases(t *testing.T) (cases [][]byte, names []string) {
	t.Helper()
	conns, err := filepath.Glob(filepath.Join(fixedRoot, "testdata", "wire", "tables", "*_conn.bin"))
	if err != nil || len(conns) == 0 {
		t.Fatalf("no connection goldens: %v", err)
	}
	for _, path := range conns {
		golden, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		base := filepath.Base(path)
		cases = append(cases, announceGrown(t, golden, 4096, false), announceGrown(t, golden, 4097, false), announceGrown(t, golden, 4096, true))
		names = append(names, base+" ids 4096", base+" ids 4097", base+" ids 4096 repeat")
	}
	return cases, names
}

// announceIdBoundRed is the cases whose Goose answer is not the reference's.
func announceIdBoundRed(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	cases, names := announceIdBoundCases(t)
	got := gooseAnnounceProbe(t, gooseMsgUnitDir(t, "tables/backend"), cases, 4096, override)
	if len(got) != len(cases) {
		t.Fatalf("the probe answered %d of %d announcements:\n%s", len(got), len(cases), strings.Join(got, "\n"))
	}
	var red []string
	for i, c := range cases {
		want := goAnnounceVerdict(c, 4096)
		malformed := strings.Fields(want)[2] == "1"
		if strings.HasSuffix(names[i], "ids 4096") == malformed {
			t.Fatalf("%s: the reference answered %q", names[i], want)
		}
		if got[i] != want {
			red = append(red, names[i])
		}
	}
	return red
}

func TestGooseMessageFormAnnounceIdBound(t *testing.T) {
	t.Parallel()
	if red := announceIdBoundRed(t, nil); len(red) != 0 {
		t.Fatalf("goose differs from the Go reference on %v", red)
	}
}

func TestGooseMessageFormAnnounceIdBoundNegativeControl(t *testing.T) {
	t.Parallel()
	src := string(Runtime()["wire_load.goose"])
	const from, to = "let IdTableBound: i64 = 4096;\n", "let IdTableBound: i64 = 4097;\n"
	if strings.Count(src, from) != 1 {
		t.Fatal("the bound's declaration is not there once")
	}
	red := announceIdBoundRed(t, map[string][]byte{"wire_load.goose": []byte(strings.Replace(src, from, to, 1))})
	_, names := announceIdBoundCases(t)
	var want []string
	for _, n := range names {
		if strings.HasSuffix(n, "ids 4097") {
			want = append(want, n)
		}
	}
	if !slices.Equal(red, want) {
		t.Fatalf("with the bound one wider, red %v, want exactly %v", red, want)
	}
}
