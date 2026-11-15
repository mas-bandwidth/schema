package gotable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO, AND A NON-ZERO ONE REFUSES THE
// WHOLE LOAD (docs/rulings.json fixed-dead-bytes-are-zero; docs/SPEC-TABLES.md
// §3.4; docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3). Each changed corpus
// vector is refused by the generated Go read and leaves the caller's
// destination untouched; each control reads and saves back byte for byte.
func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go test (a generated probe package)")
	corpus := fixedCorpus(t)
	var cases []string
	for _, vec := range fixedvector.DeadBytes {
		cases = append(cases, fmt.Sprintf("\t\t{file: %q, reads: %v},", vec.File, vec.Reads))
	}
	src := fmt.Sprintf(`package probe
import ("bytes"; "os"; "reflect"; "testing")
func TestFixedDeadBytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file  string
		reads bool
	}{
%s
	}
	corpus := %q
	for _, c := range cases {
		data, err := os.ReadFile(corpus + "/" + c.file)
		if err != nil {
			t.Fatalf("%%s: %%v", c.file, err)
		}
		dest := make([]DeadRoot, 8)
		for i := range dest {
			DeadRootReset(&dest[i])
			dest[i].OptPresent = true
			dest[i].Opt.N = 99
			dest[i].ArrCount = 3
			dest[i].Arr[0] = -7
			dest[i].ObjsCount = 3
			dest[i].Objs[0].X = -9
			dest[i].TextLength = 8
			dest[i].Text[0] = 'Z'
			dest[i].BlobLength = 4
			dest[i].Blob[0] = 0xEE
			dest[i].Pick.Type = DeadPickTypeWide
			dest[i].Pick.Wide.A = 77
		}
		before := append([]DeadRoot(nil), dest...)
		plan := make([]TableFixedEntry, 4096)
		var report TableReport
		n := DeadRootFixedLoad(dest, data, plan, &report)
		if !c.reads {
			if n >= 0 || report.Err() == nil {
				t.Fatalf("%%s: the generated read accepted a record with a non-zero dead byte: n=%%d report=%%+v", c.file, n, report)
			}
			if !reflect.DeepEqual(dest, before) {
				t.Fatalf("%%s: the refusal wrote the caller's destination", c.file)
			}
			continue
		}
		if n != 1 || report.Err() != nil {
			t.Fatalf("%%s: the control did not read: n=%%d report=%%+v", c.file, n, report)
		}
		out := make([]byte, DeadRootFixedMeasure(1))
		if got := DeadRootFixedSave(dest[:1], out); got != int64(len(out)) {
			t.Fatalf("%%s: save wrote %%d of %%d", c.file, got, len(out))
		}
		if !bytes.Equal(out, data) {
			t.Fatalf("%%s: the control did not save back byte for byte", c.file)
		}
	}
}
`, strings.Join(cases, "\n"), filepath.ToSlash(corpus))
	out, err := runVersionProbe(t, readSchema(t, "DeadBytes"), nil, src)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
