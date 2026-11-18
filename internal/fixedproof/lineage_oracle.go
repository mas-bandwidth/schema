package fixedproof

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// CppVerdict is the live C++ reference's answer to one fixed file read by a
// reader that carries a lineage: the records it returned (-1 on refusal),
// whether it refused, its reason code, the widened counter, and, on accept,
// the bytes its FixedSave writes for the records it landed.
type CppVerdict struct {
	Count   int64
	Refused bool
	Reason  int
	Widened int
	Saved   []byte
}

// String renders the verdict without the saved bytes.
func (v CppVerdict) String() string {
	if v.Refused {
		return fmt.Sprintf("refused reason=%d", v.Reason)
	}
	return fmt.Sprintf("ok n=%d widened=%d", v.Count, v.Widened)
}

// CppLineageReader compiles the C++ reference's reader for root in u with
// older as the locked lineage before the unit's own layout (oldest first, the
// current layout last, as COMPILE lays it from the lock), and returns a probe
// that reads one candidate file into a batch of capacity records. The plan is
// compiled by the C++ runtime from the writer's layout at load, so the probe
// answers the reference's own verdict on every lineage entry, lawful or not.
func CppLineageReader(t *testing.T, id string, u *ir.Unit, root string, older []LineageEntry, capacity int) func(candidate []byte) CppVerdict {
	t.Helper()
	slowtest.Gate(t, "the C++ compiler")
	own, ok := LineageEntryOf(u, root)
	if !ok {
		t.Fatalf("%s: no fixed root %s", id, root)
	}
	table := lockfile.Table{Decl: lockfile.DeclFixedTable, Name: root}
	for _, e := range append(append([]LineageEntry(nil), older...), own) {
		table.Lineage = append(table.Lineage, lockfile.LineageEntry{
			Writer:           e.Writer,
			LegacyLayoutOnly: e.LegacyLayoutOnly,
			Wire:             e.Wire,
			Layout:           e.Layout,
			Digest:           e.Digest,
			Record:           e.Record - 8,
			Retired:          e.Retired,
			Reason:           e.Reason,
		})
	}
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("%s: cpp.Generate: %v", id, err)
	}
	tables, err := cpptable.GenerateLineage(u, &lockfile.Unit{Version: 1, Package: u.Package, Tables: []lockfile.Table{table}})
	if err != nil {
		t.Fatalf("%s: cpptable.GenerateLineage: %v", id, err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var tableHeader string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatalf("%s: write %s: %v", id, name, err)
		}
		if strings.HasSuffix(name, "Table.h") {
			tableHeader = name
		}
	}
	if tableHeader == "" {
		t.Fatalf("%s: no Table.h generated", id)
	}
	serDir := resolveSerializeInclude(dir)
	src := fmt.Sprintf(`#include <cstdio>
#include <cstdlib>
#include <cstring>
#include "%[3]s"
%[4]s

using namespace %[5]s;

int main( int argc, char ** argv )
{
%[6]s
    static uint8_t in_buf[1 << 18];
    static uint8_t out_buf[1 << 18];
    static TableFixedEntry plan[4096];
    static %[1]s back[%[2]d];
    if ( argc < 3 ) return 2;
    FILE * f = fopen( argv[1], "rb" );
    if ( !f ) return 2;
    const int64_t len = (int64_t) fread( in_buf, 1, sizeof( in_buf ), f );
    fclose( f );
    for ( int i = 0; i < %[2]d; ++i ) %[1]sReset( back[i] );
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    const int64_t n = %[1]sFixedLoad( back, %[2]d, in_buf, len, plan, 4096, NULL, &r );
    const int refused = ( n < 0 || r.refused || r.damaged ) ? 1 : 0;
    printf( "n=%%lld refused=%%d reason=%%d widened=%%d\n", (long long) n, refused, (int) r.reason, (int) r.widened );
    if ( refused ) return 0;
    const int64_t need = %[1]sFixedMeasure( (int32_t) n );
    const int64_t saved = %[1]sFixedSave( back, (int32_t) n, out_buf, need );
    if ( saved != need ) return 3;
    FILE * o = fopen( argv[2], "wb" );
    if ( !o ) return 2;
    if ( (int64_t) fwrite( out_buf, 1, (size_t) saved, o ) != saved ) { fclose( o ); return 3; }
    fclose( o );
    return 0;
}
`, root, capacity, tableHeader, addressSpaceCeilingInclude(), u.Package, addressSpaceCeilingApply())
	probeCpp := filepath.Join(dir, "lineage_probe.cpp")
	probeBin := filepath.Join(dir, "lineage_probe")
	if err := os.WriteFile(probeCpp, []byte(src), 0o600); err != nil {
		t.Fatalf("%s: write probe: %v", id, err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if out, err := exec.Command(cxx, "-std=c++17", "-O1", "-I"+dir, "-I"+serDir, probeCpp, "-o", probeBin).CombinedOutput(); err != nil {
		t.Fatalf("%s: the C++ lineage reader does not compile: %v\n%s", id, err, out)
	}
	return func(candidate []byte) CppVerdict {
		t.Helper()
		if len(candidate) > 1<<18 {
			t.Fatalf("%s: the C++ lineage probe cannot hold the candidate", id)
		}
		work := t.TempDir()
		in := filepath.Join(work, "in.bin")
		out := filepath.Join(work, "out.bin")
		if err := os.WriteFile(in, candidate, 0o600); err != nil {
			t.Fatal(err)
		}
		text, err := exec.Command(probeBin, in, out).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: the C++ lineage probe failed: %v\n%s", id, err, text)
		}
		var v CppVerdict
		var refused int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(text)), "n=%d refused=%d reason=%d widened=%d", &v.Count, &refused, &v.Reason, &v.Widened); err != nil {
			t.Fatalf("%s: the C++ lineage probe answered %q: %v", id, text, err)
		}
		v.Refused = refused != 0
		if !v.Refused {
			saved, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("%s: the C++ lineage probe wrote no save: %v", id, err)
			}
			v.Saved = saved
		}
		return v
	}
}
