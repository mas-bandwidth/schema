package gotable

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// runGeneratedLineageProbe generates `reader` with `lineage` (OLDEST FIRST, the
// reader's own entry appended by the emitter) and runs `testSource`, a probe in
// the generated package that builds its own file bytes and calls the emitted
// <T>FixedLoad. It is the house pattern for a load that needs a multi-version
// lineage: layout_unsupported (a retired hash below the floor) and
// layout_record_too_large (an entry past the writer's record, carried as the
// lineage lane's Why) are unreachable from a clean one-layout table.
func runGeneratedLineageProbe(t *testing.T, reader string, lineage map[string][]FixedLineageEntry, testSource string) {
	t.Helper()
	u := unitOf(t, reader)
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	// A simple fixed table's generated code is self-contained (stdlib + unsafe),
	// so the probe module needs no external runtime: a bare go.mod keeps the
	// test runnable where the serialize.go checkout is not staged.
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	files["version_test.go"] = []byte(strings.Replace(testSource, "package probe", "package "+u.Package, 1))
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated lineage unit: %v\n%s", err, out)
	}
}

// testFixedLoadNamesUnsupported proves layout_unsupported through the emitted
// <T>FixedLoad: an older generation is retired, the floor rises to 1, and a file
// carrying the retired generation's hash refuses with Reason exactly the exported
// TableFixedReasonLayoutUnsupported, reporting the file's hash (§5.3).
func testFixedLoadNamesUnsupported(t *testing.T) {
	t.Helper()
	older := "package probe\n\nfixed table Ping {\n    x int32\n}\n"
	reader := "package probe\n\nfixed table Ping {\n    x int32 = 1\n    y int32 = 2\n}\n"
	e, ok := FixedLineageOf(unitOf(t, older), "Ping")
	if !ok {
		t.Fatal("no lineage entry for the retired Ping generation")
	}
	e.Retired = true
	e.Reason = "retired by the test's lock"
	runGeneratedLineageProbe(t, reader, map[string][]FixedLineageEntry{"Ping": {e}}, fmt.Sprintf(`package probe
import ("encoding/binary"; "testing")

func TestLayoutUnsupported(t *testing.T) {
	back := make([]Ping, 8)
	plan := make([]TableFixedEntry, 4096)
	file := make([]byte, TableFixedHeaderBytes+4)
	file[0] = TableFixedForm
	binary.LittleEndian.PutUint64(file[TableFixedHashAt:], %d)
	var r TableReport
	want := TableReport{Verdict: TableOpenRefused, FixedRaised: true, Reason: TableFixedReasonLayoutUnsupported, LayoutHash: %d}
	if n := PingFixedLoad(back, file, plan, &r); n != -1 || r != want {
		t.Fatalf("layout_unsupported: n=%%d report=%%+v want=%%+v", n, r, want)
	}
}
`, e.Wire, e.Wire))
}

// hostileTooLargeLayout rebuilds the §1.1-valid layout whose plan refuses the
// READ whole: a root whose record body is four bytes (one uint32), with an array
// whose counted head reads four bytes beyond it, so a plan entry's source extent
// reaches past the writer's declared record and tableFixedCompile answers -3 —
// the lineage lane's Why is then layout_record_too_large (§5.2). The ids are
// READ OUT of the reader's own layout, not spelled as hashes, mirroring
// TestFixedFormRecordBound. The layout is expressed here as its raw bytes (the
// u32 entry count then seventeen-byte entries: id u64, kind u8, size u32,
// children u32) so the test depends on nothing but the wire format.
func hostileTooLargeLayout(t *testing.T, myLayout []byte) []byte {
	t.Helper()
	if len(myLayout) < 4 {
		t.Fatalf("read layout is shorter than its entry count")
	}
	count := binary.LittleEndian.Uint32(myLayout[0:4])
	type ent struct {
		id       uint64
		kind     uint8
		size     uint32
		children uint32
	}
	ents := make([]ent, 0, count)
	for i := uint32(0); i < count; i++ {
		base := 4 + int(i)*17
		if base+17 > len(myLayout) {
			break
		}
		ents = append(ents, ent{
			id:       binary.LittleEndian.Uint64(myLayout[base : base+8]),
			kind:     myLayout[base+8],
			size:     binary.LittleEndian.Uint32(myLayout[base+9 : base+13]),
			children: binary.LittleEndian.Uint32(myLayout[base+13 : base+17]),
		})
	}
	var rootID, keepID, blobID, blobElemID uint64
	for i, e := range ents {
		if i == 0 {
			rootID = e.id
			continue
		}
		if e.kind == 8 && e.size == 4 && keepID == 0 {
			keepID = e.id
		}
		if e.kind == 14 && i+1 < len(ents) {
			if el := ents[i+1]; el.kind == 6 && el.size == 1 {
				blobID = e.id
				blobElemID = el.id
			}
		}
	}
	if rootID == 0 || keepID == 0 || blobID == 0 {
		t.Fatalf("hostile: the ids for root/keep/blob are not in the reader's own layout")
	}
	entry := func(id uint64, kind uint8, size, children uint32) []byte {
		e := make([]byte, 17)
		binary.LittleEndian.PutUint64(e[0:8], id)
		e[8] = kind
		binary.LittleEndian.PutUint32(e[9:13], size)
		binary.LittleEndian.PutUint32(e[13:17], children)
		return e
	}
	blobSize := uint32(0)
	hostile := make([]byte, 4)
	binary.LittleEndian.PutUint32(hostile[0:4], 4)
	hostile = append(hostile, entry(rootID, 13, 4+blobSize, 2)...)
	hostile = append(hostile, entry(keepID, 8, 4, 0)...)
	hostile = append(hostile, entry(blobID, 14, blobSize, 1)...)
	hostile = append(hostile, entry(blobElemID, 6, 1, 0)...)
	return hostile
}

// testFixedLoadNamesRecordTooLarge proves layout_record_too_large through the
// emitted <T>FixedLoad: the lineage holds a §1.1-valid layout whose plan refuses
// WHOLE (an entry past the writer's record), and a file carrying that layout's
// hash refuses with Reason exactly the exported TableFixedReasonLayoutRecordTooLarge.
func testFixedLoadNamesRecordTooLarge(t *testing.T) {
	t.Helper()
	reader := "package probe\n\nfixed table Host {\n    keep uint32 = 7\n    label string(8) = \"hi\"\n    blob bytes(6)\n}\n"
	own, ok := FixedLineageOf(unitOf(t, reader), "Host")
	if !ok {
		t.Fatal("no lineage entry for the Host table")
	}
	hostile := hostileTooLargeLayout(t, own.Layout)
	const wire uint64 = 0xDEADBEEFCAFEF00D
	lineage := map[string][]FixedLineageEntry{"Host": {{Wire: wire, Layout: hostile, Record: 4}}}
	var b strings.Builder
	for i, c := range hostile {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d", c)
	}
	runGeneratedLineageProbe(t, reader, lineage, fmt.Sprintf(`package probe
import ("encoding/binary"; "testing")

func TestLayoutRecordTooLarge(t *testing.T) {
	layout := []byte{%s}
	file := make([]byte, TableFixedHeaderBytes+4+len(layout))
	file[0] = TableFixedForm
	binary.LittleEndian.PutUint64(file[TableFixedHashAt:], %d)
	binary.LittleEndian.PutUint32(file[TableFixedHeaderBytes:], uint32(len(layout)))
	copy(file[TableFixedHeaderBytes+4:], layout)
	back := make([]Host, 1)
	plan := make([]TableFixedEntry, 256)
	var r TableReport
	want := TableReport{Verdict: TableOpenRefused, FixedRaised: true, Reason: TableFixedReasonLayoutRecordTooLarge}
	if n := HostFixedLoad(back, file, plan, &r); n != -1 || r != want {
		t.Fatalf("layout_record_too_large: n=%%d report=%%+v want=%%+v", n, r, want)
	}
}
`, b.String(), wire))
}
