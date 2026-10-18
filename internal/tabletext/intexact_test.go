package tabletext_test

// AN INTEGER FIELD IS READ EXACTLY, NEVER THROUGH FLOAT64 (docs/SPEC-TABLES.md
// §16.2, Numbers). This is the tool's engine half: the field path reads the
// token's own digits, so 9007199254740993.0 lands as itself, 1e2 lands 100,
// 1.0000000000000001 and 1.5 are each one kind_mismatch with the field
// unchanged, and -0.0 lands 0. The generated walk's test lands the same wire
// bytes and the same counters on these texts.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const intExactTextSchema = `package probe

table Root
{
    n int64
}
`

func TestTextIntegerFieldReadsExactly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe.schema"), []byte(intExactTextSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	st := m.Lookup("Root")
	if st == nil {
		t.Fatal("no Root table")
	}
	cases := []struct {
		token           string
		want            int64
		mismatch, clamp int
	}{
		{"9007199254740993.0", 9007199254740993, 0, 0},
		{"1e2", 100, 0, 0},
		{"1.0000000000000001", 0, 1, 0},
		{"1.5", 0, 1, 0},
		{"-0.0", 0, 0, 0},
	}
	for _, c := range cases {
		inst := m.New(st)
		var r tabletext.Report
		if !m.Read(inst, []byte(`{"n":`+c.token+`}`), &r) {
			t.Fatalf("%s: read failed: %+v", c.token, r)
		}
		fv, ok := inst.FieldByKey("n")
		if !ok {
			t.Fatal("no field keyed n")
		}
		if fv.Cell.I != c.want {
			t.Fatalf("%s: n=%d, want %d (report %+v)", c.token, fv.Cell.I, c.want, r)
		}
		if r.KindMismatch != c.mismatch || r.Clamped != c.clamp || r.Malformed || r.Unknown != 0 || r.Duplicate != 0 {
			t.Fatalf("%s: report %+v, want kind_mismatch=%d clamped=%d", c.token, r, c.mismatch, c.clamp)
		}
		if _, err := tablewire.Encode(m, inst); err != nil {
			t.Fatalf("%s: the value the read left does not encode: %v", c.token, err)
		}
	}
}
