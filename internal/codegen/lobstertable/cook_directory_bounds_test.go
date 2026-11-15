package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// cookDirectorySchema is one record with a string by reference, an
// unbounded string, a list, a nested table and a pointer to another node, so
// its cook's directory names both tables and byte buffers.
const cookDirectorySchema = `package probe

table Inner {
    n     int32
    label string(8)
}

table Root {
    title *string
    words []string
    items []int32
    inner Inner
    next  *Inner
}
`

const cookDirectoryRecord = `{"title":"directory","words":["alpha","beta"],"items":[1,2,3],"inner":{"n":7,"label":"in"},"next":{"n":9,"label":"nx"}}`

// cookDirectoryProgram cooks the record the wire names, reads the cook back
// whole, then inverts each byte of the cook in turn and asks Open and ReadCook
// of the result: one line per position, in one process.
const cookDirectoryProgram = `import .schema_save
let wire = read_file("wire.bin")
assert wire
let rec = schema_rt.load("Root", wire, schema_rt.LoadReport {})
assert rec
let cook = probe.RootCook(rec, false)
assert length(cook) > 0
let whole = probe.RootReadCook(cook)
assert whole
print("whole " + (if schema_rt.save(whole) == wire: "equal" else: "changed"))
var p = 0
while p < length(cook):
    var d = substring(cook, 0, length(cook))
    d = write_int8_le(d, p, (cook[p] ^ 0xff) & 0xff)
    let open = probe.RootOpen(d)
    let back = probe.RootReadCook(d)
    print("{p} " + (if open: "open" else: "closed") + " " + (if back: "record" else: "refused"))
    p++
print("cooked {length(cook)}")
`

// cookDirectoryRun runs the program over the generated unit, with sabotage
// applied to the emitted cook.lobster first when it is not nil, and answers
// the program's output and exit code.
func cookDirectoryRun(t *testing.T, sabotage func(string) string) (string, int) {
	t.Helper()
	slowtest.Gate(t, "the Lobster toolchain")
	g, err := generateLoadUnit(t, []string{writeSchema(t, "Probe", cookDirectorySchema)})
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(g.u)
	rec := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(rec, []byte(cookDirectoryRecord), &report) || !report.Silent() {
		t.Fatalf("fixture read: %+v", report)
	}
	wire, err := tablewire.Encode(m, rec)
	if err != nil {
		t.Fatal(err)
	}
	if sabotage != nil {
		p := filepath.Join(g.dir, "cook.lobster")
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(sabotage(string(src))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(cookDirectoryProgram)
	if err := os.WriteFile(filepath.Join(g.dir, "forged.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := lobsterrun.RunExit(t, g.dir, "forged.lobster", map[string][]byte{"wire.bin": wire})
	return string(out), code
}

// cookDirectoryVerdict answers "" when the run printed the whole read equal,
// a refusal or a record for every position of the cook and exited 0, and
// otherwise what went wrong.
func cookDirectoryVerdict(out string, code int) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 || lines[0] != "whole equal" {
		return fmt.Sprintf("the unmodified cook does not read back equal (exit %d): %.200q", code, out)
	}
	last := lines[len(lines)-1]
	size, err := strconv.Atoi(strings.TrimPrefix(last, "cooked "))
	if err != nil || !strings.HasPrefix(last, "cooked ") {
		return fmt.Sprintf("the run stopped after %d positions (exit %d); last line %q", len(lines)-2, code, last)
	}
	if code != 0 {
		return fmt.Sprintf("the run exited %d", code)
	}
	if len(lines) != size+2 {
		return fmt.Sprintf("the run printed %d position lines for a %d-byte cook", len(lines)-2, size)
	}
	for p := range size {
		pos, rest, _ := strings.Cut(lines[p+1], " ")
		if pos != strconv.Itoa(p) || (!strings.HasSuffix(rest, " refused") && !strings.HasSuffix(rest, " record")) {
			return fmt.Sprintf("position %d answered %q", p, lines[p+1])
		}
	}
	return ""
}

// TestLobsterCookReadForgedDirectoryRefuses inverts every byte of a cook in
// turn: each forgery either refuses or reads, and the read never leaves the
// bytes it was given (docs/SPEC-TABLES.md §7, §7.2: a reference that leaves
// the region is refused).
func TestLobsterCookReadForgedDirectoryRefuses(t *testing.T) {
	t.Parallel()
	out, code := cookDirectoryRun(t, nil)
	if why := cookDirectoryVerdict(out, code); why != "" {
		t.Fatalf("forged cooks: %s", why)
	}
	t.Logf("every inverted byte of the cook refuses or reads: %s", strings.TrimSpace(out[strings.LastIndex(strings.TrimSpace(out), "\n")+1:]))
}

// TestLobsterCookReadForgedDirectoryRefusesNegativeControl removes the
// directory bound from an emitted copy of cook.lobster and requires the gate
// red.
func TestLobsterCookReadForgedDirectoryRefusesNegativeControl(t *testing.T) {
	t.Parallel()
	const bound = "        if off < 0 or off >= data_length or (i > 0 and data_offset + off < c.dir[i - 1]):\n            return nil\n"
	out, code := cookDirectoryRun(t, func(src string) string {
		if strings.Count(src, bound) != 1 {
			t.Fatal("the control's anchor, the directory bound, is not in cook.lobster once")
		}
		return strings.Replace(src, bound, "", 1)
	})
	why := cookDirectoryVerdict(out, code)
	if why == "" {
		t.Fatal("with the directory bound removed, every forged cook still refused or read: the gate cannot fail")
	}
	t.Logf("negative control: the directory bound removed, the gate is red: %s", why)
}
