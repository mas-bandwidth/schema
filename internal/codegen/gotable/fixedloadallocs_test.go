package gotable

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
)

const (
	fixtureFormByte    = 3
	fixtureHeaderBytes = 16
	fixtureHashAt      = 8
)

// fixedLoadFixture writes a form-3 file carrying `n` zero-filled records of a
// LOCKED layout. The bytes are built here from the same lock data the emitter
// lays down, so the allocation gate needs no prebuilt corpus.
func fixedLoadFixture(t *testing.T, e FixedLineageEntry, n int) string {
	t.Helper()
	f := make([]byte, 0, fixtureHeaderBytes+4+len(e.Layout)+n*int(e.Record))
	f = append(f, fixtureFormByte)
	f = append(f, make([]byte, 7)...) // seven reserved zeros
	f = appendFixedU64(f, e.Wire)     // the layout hash at 8
	f = appendFixedU32(f, uint32(len(e.Layout)))
	f = append(f, e.Layout...)
	rec := make([]byte, e.Record)
	for i := 0; i < 8; i++ {
		rec[i] = byte(e.Wire >> (8 * i))
	}
	for i := 0; i < n; i++ {
		f = append(f, rec...)
	}
	path := filepath.Join(t.TempDir(), "fixture.bin")
	if err := os.WriteFile(path, f, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// runFixLoadProbe compiles the READER's unit with the OLDER units' locked
// entries as its lineage and runs `testSource` (a Go test body) inside the
// generated package. It is the versioning probe's twin with the serialize.go
// sibling lookup dropped, because a fixture of plain integer fields never
// imports it and the replace is inert.
func runFixLoadProbe(t *testing.T, reader string, older []string, testSource string) ([]byte, error) {
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
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["alloc_test.go"] = []byte(testSource)
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// TestFixedLoadAllocatesNothingPerCall holds the §4.3 rule that the hole list
// and the default image are per plan and computed once, never per call: a
// three-record load, warmed once, allocates NOTHING per call — for the identity
// plan and for a compiled lineage plan alike.
func TestFixedLoadAllocatesNothingPerCall(t *testing.T) {
	t.Parallel()
	reader := `package probe
fixed table Root {
    a int32
    b int32
}
`
	older := `package probe
fixed table Root {
    a int32
}
`
	own, ok := FixedLineageOf(unitOf(t, reader), "Root")
	if !ok {
		t.Fatal("no identity lineage entry for Root")
	}
	old, ok := FixedLineageOf(unitOf(t, older), "Root")
	if !ok {
		t.Fatal("no older lineage entry for Root")
	}
	identityFile := fixedLoadFixture(t, own, 3)
	lineageFile := fixedLoadFixture(t, old, 3)
	src := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestAllocatesNothing(t *testing.T) {
	for _, path := range []string{%q, %q} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var out [8]Root
		plan := make([]TableFixedEntry, 4096)
		var r TableReport
		n := RootFixedLoad(out[:], data, plan, &r)
		if n != 3 {
			t.Fatalf("load read n=%%d %%+v, want 3", n, r)
		}
		var sink int64
		allocs := testing.AllocsPerRun(100, func() {
			var r2 TableReport
			sink += RootFixedLoad(out[:], data, plan, &r2)
		})
		if allocs != 0 {
			t.Fatalf("load allocates %%v per call; the hole list and default image must be laid once per plan (§4.3)", allocs)
		}
		if sink == 0 {
			t.Fatal("the measured load never ran")
		}
	}
}
`, identityFile, lineageFile)
	out, err := runFixLoadProbe(t, reader, []string{older}, src)
	if err != nil {
		t.Fatalf("generated allocation gate: %v\n%s", err, out)
	}
}
