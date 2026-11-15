package gotable

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// jsonIntegersSchema is the unit testdata/wire/json-integers reads: an int32,
// an int64, a uint64, a ranged int128, a ranged int64, a wide fixed-point
// field and a map keyed by uint64. It is the same text the engine's reader
// of that corpus compiles.
const jsonIntegersSchema = `package probe

table Integers
{
    small   int32
    wide    int64
    uwide   uint64
    wide128 int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    ranged  int64 | min = 0, max = 9007199254740993
    frac    fixed(48, 16) | min = -10, max = 10
    counts  map[uint64]int32
}
`

// TestGoJsonWideIntegersAreStrings holds both JSON integer rules on the
// generated Go writer and reader (docs/SPEC-TABLES.md §16.2). A wide integer
// is written as a string of decimal digits and reads as a string or a number.
// Every integer, every width, takes an integer token only. The committed
// canonical file is the engine body; the generated writer appends the one
// trailing newline §16.1 requires, and the comparison is that text byte for
// byte.
func TestGoJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-integers")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows strings.Builder
	count := 0
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s:%d: want 5 columns, got %d", filepath.Join(dir, "MANIFEST.tsv"), i+1, len(fields))
		}
		body, err := os.ReadFile(filepath.Join(dir, fields[2]))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&rows, "\t\t{%s, %s, []byte(%s)},\n", strconv.Quote(fields[0]), strconv.Quote(fields[3]), strconv.Quote(string(body)))
		count++
	}
	if count == 0 {
		t.Fatal("no manifest rows")
	}
	source := fmt.Sprintf(`package probe

import (
	"bytes"
	"math"
	"testing"

	serialize "github.com/mas-bandwidth/serialize.go/v2"
)

func TestWide(t *testing.T) {
	canonical := []byte(%s)
	want := append([]byte(nil), canonical...)
	if len(want) == 0 || want[len(want)-1] != '\n' {
		want = append(want, '\n')
	}
	rows := []struct {
		name, verdict string
		body          []byte
	}{
%s	}
	if len(rows) == 0 {
		t.Fatal("no rows")
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			var b IntegersBuilder
			if !b.Init() {
				t.Fatal("init")
			}
			defer b.Shutdown()
			var report TableReport
			ok := IntegersFromJSON(&b, row.body, &report)
			if row.verdict == "refused" {
				if ok || !report.Refused() {
					t.Fatalf("%%s: read accepted %%q", row.name, row.body)
				}
				return
			}
			if row.verdict != "read" {
				t.Fatalf("verdict %%s", row.verdict)
			}
			if !ok || report.Refused() {
				t.Fatalf("%%s: read refused %%+v\n%%s", row.name, report, row.body)
			}
			if !b.Lock() {
				t.Fatal("lock")
			}
			v := b.AsConst()
			if v.Small != 7 || v.Wide != -42 || v.Uwide != math.MaxUint64 || v.Ranged != 9007199254740993 || v.Frac != 32768 {
				t.Fatalf("%%s: values small=%%d wide=%%d uwide=%%d ranged=%%d frac=%%d", row.name, v.Small, v.Wide, v.Uwide, v.Ranged, v.Frac)
			}
			if v.Wide128 != (serialize.Int128{Lo: ^uint64(0), Hi: 0x7fffffffffffffff}) {
				t.Fatalf("%%s: wide128 %%+v", row.name, v.Wide128)
			}
			zero := IntegersCountsFind(nil, &v.Counts, 0)
			big := IntegersCountsFind(nil, &v.Counts, 9007199254740993)
			if v.Counts.Count != 2 || zero == nil || *zero != 1 || big == nil || *big != 2 {
				t.Fatalf("%%s: counts %%d", row.name, v.Counts.Count)
			}
			n := IntegersToJSONMeasure(v)
			if n < 0 {
				t.Fatal("measure refused")
			}
			buf := make([]byte, n)
			if IntegersToJSON(v, buf) != n {
				t.Fatal("write")
			}
			if !bytes.Equal(buf, want) {
				t.Fatalf("%%s: wrote\n%%s\nwant\n%%s", row.name, buf, want)
			}
		})
	}
}
`, strconv.Quote(string(canonical)), rows.String())
	runGenerated(t, jsonIntegersSchema, source)
}
