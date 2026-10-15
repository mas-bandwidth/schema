package gotable

// THE PREFILL IS PER PLAN (docs/FIXED-FORM-ALGORITHM.md §4.3): the hole list
// and the default image depend only on the plan the header's hash selected, so
// the standard says they are computed once, at init, and never per call. This
// test pins the Go leg to it where it hurts: a WARMED three-record load
// allocates NOTHING per call — testing.AllocsPerRun counts the allocations and
// reads no clock — under the identity plan and under a compiled lineage plan,
// the two plans a load can run.
//
// The row is `field_append` (test/tables/VOLD_/VNEW_field_append.schema): the
// newer reader declares x, y, z and the appended w with default 77, so w's four
// bytes are the lineage plan's one hole and the load that answers it proves
// the prefill ran. The older writer's file is built in the probe from the
// LOCK'S OWN FACTS — the wire hash, the layout bytes, the record size — the way
// a writer of that build wrote it, so the test needs no byte oracle.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/ir"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// runFixedLoadAllocsProbe generates the READER's unit with the OLDER unit's
// locked entry handed to it as its lineage (the runVersionProbe shape) and
// compiles the probe in a t.TempDir(). The row is union-free, so the generated
// package imports no serialize runtime and the probe's go.mod names none: no
// sibling checkout is looked for, and the test runs wherever the Go toolchain
// does.
func runFixedLoadAllocsProbe(t *testing.T, reader string, older []string, testSource string) ([]byte, error) {
	t.Helper()
	u := unitOf(t, reader)
	lineage := map[string][]FixedLineageEntry{}
	for _, src := range older {
		o := unitOf(t, src)
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
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
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	files["allocs_test.go"] = []byte(strings.Replace(testSource, "package probe", "package "+u.Package, 1))
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
	return cmd.CombinedOutput()
}

func TestFixedLoadAllocatesNothingPerCall(t *testing.T) {
	t.Parallel()
	older := `package probe

fixed table Lineage
{
	x int32 = 0
	y int32 = 0
	z int32 = 0
}
`
	newer := `package probe

fixed table Lineage
{
	x int32 = 0
	y int32 = 0
	z int32 = 0
	// THE APPENDED FIELD: its four bytes are the lineage plan's one hole,
	// and its declared default 77 is what a prefill lands there (§5.4).
	w int32 = 77
}
`
	// THE LOCK'S OWN FACTS for the older writer: the wire hash a file is
	// matched on, the layout bytes verbatim, and the record size — taken from
	// the lock and never from the file (§5.2).
	e, ok := FixedLineageOf(unitOf(t, older), "Lineage")
	if !ok {
		t.Fatal("no lineage entry for Lineage")
	}
	src := fmt.Sprintf(`package probe

import ("encoding/binary"; "testing")

func TestFixedLoadAllocatesNothingPerCall(t *testing.T) {
	// THE IDENTITY PLAN'S FILE: this build's own writer, three records.
	values := make([]Lineage, 3)
	for k := range values {
		LineageReset(&values[k])
		values[k].X = int32(k + 1)
		values[k].Y = int32(11 * (k + 1))
		values[k].Z = int32(-7 * (k + 1))
		values[k].W = 1000 + int32(k)
	}
	file := make([]byte, LineageFixedMeasure(3))
	if LineageFixedSave(values, file) != int64(len(file)) {
		t.Fatal("the identity file did not save")
	}

	// THE LINEAGE PLAN'S FILE: the older writer's three records, built from
	// the lock's facts — form byte, seven reserved zeros, the older hash at
	// 8, the older layout behind its u32 length, then the records.
	old := []byte{TableFixedForm, 0, 0, 0, 0, 0, 0, 0}
	var hash [8]byte
	binary.LittleEndian.PutUint64(hash[:], uint64(0x%016x))
	old = append(old, hash[:]...)
	layout := %[2]s
	var lb [4]byte
	binary.LittleEndian.PutUint32(lb[:], uint32(len(layout)))
	old = append(old, lb[:]...)
	old = append(old, layout...)
	for k := 0; k < 3; k++ {
		old = append(old, hash[:]...)
		old = append(old, make([]byte, %[3]d)...)
	}

	back := make([]Lineage, 4)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport

	// WARM THE LOAD ONCE under both plans: the first call is not the
	// measured one, and a warm load that refuses says so before any count.
	if n := LineageFixedLoad(back, file, plan, &r); n != 3 {
		t.Fatalf("the identity load read %%d of 3 records: %%+v", n, r)
	}
	if n := LineageFixedLoad(back, old, plan, &r); n != 3 {
		t.Fatalf("the lineage load read %%d of 3 records: %%+v", n, r)
	}
	if r.Reason != "" || r.Malformed || r.Verdict == TableOpenRefused {
		t.Fatalf("a clean three-record load is not a refusal: %%+v", r)
	}
	for k := 0; k < 3; k++ {
		if back[k].W != 77 {
			t.Fatalf("record %%d: the appended field is not its declared default 77: %%+v", k, back[k])
		}
	}

	// ONE PATH, ONE PREFILL (§4.3): the hole list and the default image are
	// the plan's, laid once, so a warmed three-record load allocates nothing
	// per call — under the identity plan and under the lineage plan alike.
	loaded := int64(0)
	if n := testing.AllocsPerRun(100, func() {
		r = TableReport{}
		loaded = LineageFixedLoad(back, file, plan, &r)
	}); n != 0 {
		t.Fatalf("identity plan: a warmed three-record load allocated %%v times per call (§4.3: the prefill is per plan, not per call)", n)
	}
	if loaded != 3 {
		t.Fatalf("identity plan: the measured load read %%d of 3 records: %%+v", loaded, r)
	}
	if n := testing.AllocsPerRun(100, func() {
		r = TableReport{}
		loaded = LineageFixedLoad(back, old, plan, &r)
	}); n != 0 {
		t.Fatalf("lineage plan: a warmed three-record load allocated %%v times per call (§4.3: the prefill is per plan, not per call)", n)
	}
	if loaded != 3 {
		t.Fatalf("lineage plan: the measured load read %%d of 3 records: %%+v", loaded, r)
	}
}
`, e.Wire, fmt.Sprintf("%#v", e.Layout), e.Record-8)
	out, err := runFixedLoadAllocsProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("the generated load allocates per call: %v\n%s", err, out)
	}
}
