package goosetable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideRecordDir holds a record past 4 GiB (docs/SPEC-TABLES.md §11): Huge is
// three 2 GiB Parts and a field past 4 GiB. Every leg generates and compiles
// it with each stored size whole.
var wideRecordDir = filepath.Join("..", "..", "..", "testdata", "wire", "tables", "storage-cap", "wide")

// wideRecordProbeMain prints every size the unit's descriptors hold (each
// record's storage and alignment, and each list or map term's element size
// and alignment). It never constructs a record.
const wideRecordProbeMain = `
fn main() {
    for t in conformance_unit().tables {
        write_stdout(str(t.name, ".size=", t.size, "\n"));
        write_stdout(str(t.name, ".align=", t.align, "\n"));
        for f in t.fields {
            if f.es != 0 {
                write_stdout(str(t.name, ".", f.name, ".es=", f.es, "\n"));
                write_stdout(str(t.name, ".", f.name, ".ea=", f.ea, "\n"));
            }
        }
    }
}
`

// wideRecordRun generates the wide unit, applies patch to every generated
// file (nil for none), compiles and runs the probe under --debug and answers
// every size that disagrees with ir.
func wideRecordRun(t *testing.T, patch func(string) string) []string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(wideRecordDir, "Wide.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := probeUnit(t, string(src))
	files := probeFiles(t, u, nil)
	for name, data := range files {
		// the source stays proportional to the schema, not to Part's 2^28
		// elements
		if len(data) > 1<<20 && !strings.HasPrefix(name, "schema_rt") {
			t.Fatalf("%s is %d bytes; an array was written out element by element", name, len(data))
		}
	}
	if patch != nil {
		patched := false
		for name, data := range files {
			if out := patch(string(data)); out != string(data) {
				files[name], patched = []byte(out), true
			}
		}
		if !patched {
			t.Fatal("the patch matched no generated file")
		}
	}
	files["main.goose"] = []byte(probePrelude + wideRecordProbeMain)
	got := map[string]string{}
	out := strings.TrimSpace(string(gooserun.RunDebug(t, t.TempDir(), "main.goose", files)))
	for line := range strings.SplitSeq(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q is not key=value", line)
		}
		got[k] = v
	}
	entry := u.Tables["Root"].Fields[2].MapEntry
	want := map[string]string{}
	for _, st := range []*ir.Struct{u.Tables["Part"], u.Tables["Huge"], u.Tables["Root"], entry} {
		l := ir.RecordLayout(u, st)
		want[st.Name+".size"] = strconv.FormatInt(l.Size, 10)
		want[st.Name+".align"] = strconv.FormatInt(l.Align, 10)
		for _, f := range st.Fields {
			var size, align int64
			switch {
			case f.IsMap():
				el := ir.RecordLayout(u, f.MapEntry)
				size, align = el.Size, el.Align
			case f.Array == ir.ArrayList:
				size, align = ir.ListElementLayout(u, f)
			default:
				continue
			}
			want[st.Name+"."+f.Name+".es"] = strconv.FormatInt(size, 10)
			want[st.Name+"."+f.Name+".ea"] = strconv.FormatInt(align, 10)
		}
	}
	var bad []string
	for k, w := range want {
		if got[k] != w {
			bad = append(bad, fmt.Sprintf("%s: got %q, want %s", k, got[k], w))
		}
	}
	if huge, _ := strconv.ParseInt(want["Huge.size"], 10, 64); huge <= 1<<32 {
		bad = append(bad, "Huge is not past 4 GiB, so the unit holds nothing wide")
	}
	return bad
}

// TestGooseWideRecordCompiles: the Goose leg generates a record past 4 GiB,
// the generated unit compiles and runs under --debug, and every record size
// and alignment and every list or map term size its descriptors hold equals
// ir's, past 2^32 where ir's is.
func TestGooseWideRecordCompiles(t *testing.T) {
	t.Parallel()
	memceiling.Guard(t, memceiling.Default)
	if bad := wideRecordRun(t, nil); len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// TestGooseWideRecordNegativeControl narrows Huge's size to 32 bits in the
// generated descriptor and requires the probe to go red.
func TestGooseWideRecordNegativeControl(t *testing.T) {
	t.Parallel()
	memceiling.Guard(t, memceiling.Default)
	src, err := os.ReadFile(filepath.Join(wideRecordDir, "Wide.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := probeUnit(t, string(src))
	size := ir.RecordLayout(u, u.Tables["Huge"]).Size
	from := fmt.Sprintf("size: %d,", size)
	bad := wideRecordRun(t, func(src string) string {
		return strings.Replace(src, from, fmt.Sprintf("size: %d,", size&0xffffffff), 1)
	})
	if len(bad) == 0 {
		t.Fatal("a Huge size narrowed to 32 bits still matched ir")
	}
}
