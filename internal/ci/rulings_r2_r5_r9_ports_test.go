package ci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cstable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/darttable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/javatable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/jstable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luatable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rusttable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/unitytable"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE PORTS' ROWS FOR RULINGS R2, R5 AND R9 (2026-11-22). Each row reads what a
// port's emitter writes for a unit with a fixed table and a variable table and
// holds one clause of the ruling to it; each clause goes red when the old
// behaviour is planted back. The runtime of every one of these ports is run by
// its own announcement and lineage tests; this is the shared table of what the
// three rulings changed in each.

const rulingsPortsSource = `package probe

fixed table Cfg
{
    n ?int32
    k int32
}

table Graph
{
    a    int32 = 0
    name string(16)
    next *Graph
}
`

func rulingsPortsUnit(t *testing.T) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(rulingsPortsSource), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// rulingsLineageUnit is a reader whose fixed table declares a bare field the
// lock still holds as ?T: the lock refuses to record that edit (it is not an
// append), so only the old layout is locked, the way a hand-edited lineage
// entry reaches a reader.
func rulingsLineageUnit(t *testing.T) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	write := func(src string) *ir.Unit {
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	old := write("package probe\n\nfixed table Cfg\n{\n    n ?int32\n    k int32\n}\n")
	if _, _, err := lockfile.Update(old, []string{path}); err != nil {
		t.Fatal(err)
	}
	return write("package probe\n\nfixed table Cfg\n{\n    n int32\n    k int32\n}\n")
}

func TestRulingsR2R5R9PortRows(t *testing.T) {
	t.Parallel()
	u := rulingsPortsUnit(t)
	lineage := rulingsLineageUnit(t)
	rows := []struct {
		port     string
		generate func(*ir.Unit) (map[string][]byte, error)
		want     []string
	}{
		// R2: a writer's ?T read into a bare T refuses kind_mismatch
		{"R2 dart", darttable.Generate, []string{"plan.optionalIntoBare = true", "static const int wrongKind = 24;"}},
		{"R2 js", jstable.Generate, []string{"plan.kindMismatch = true", "KindMismatch: 24"}},
		{"R2 rust", rusttable.Generate, []string{"c.kind_mismatch = true", "Self::KindMismatch => \"kind_mismatch\""}},
		{"R2 goose", goosetable.Generate, []string{"FixedRefuseKindMismatch"}},
		{"R2 lua", luatable.Generate, []string{"error(\"kind_mismatch\", 0)"}},
		{"R2 squirrel", squirreltable.Generate, []string{"return \"kind_mismatch\";"}},
		// R5: LoadMeasure reports data and attribution separately
		{"R5 js", jstable.Generate, []string{"LoadMeasureParts(bytes)"}},
		{"R5 java", javatable.Generate, []string{"loadMeasureParts(byte[] data)"}},
		{"R5 rust", rusttable.Generate, []string{"_load_measure_parts(wire: &[u8]) -> (i64, i64)"}},
		{"R5 elixir", elixirtable.Generate, []string{"def load_measure_parts(table, data)"}},
		{"R5 lua", luatable.Generate, []string{"function L.LoadMeasureParts(T, name, bytes)"}},
		{"R5 lobster", lobstertable.Generate, []string{"def load_measure_parts(root:string, data:string)"}},
		{"R5 unity", unitytable.Generate, []string{"LoadMeasureParts(TableTypeInfo type"}},
		// R9: a caller's MaxBytes of 0 refuses and never means the default
		{"R9 js", jstable.Generate, []string{"if (n > globalThis.BigInt(v.MaxBytes))"}},
		{"R9 rust", rusttable.Generate, []string{"if length > vocabulary.max_bytes as u64"}},
		{"R9 java", javatable.Generate, []string{"long limit = v.maxBytes < 0 ? 0L : (long) v.maxBytes;"}},
		{"R9 cs", cstable.Generate, []string{"(ulong)(v.MaxBytes < 0 ? 0 : v.MaxBytes)"}},
		{"R9 unity", unitytable.Generate, []string{"(ulong)(v.MaxBytes < 0 ? 0 : v.MaxBytes)"}},
		{"R9 lua", luatable.Generate, []string{"if maxBytes == nil then maxBytes = 64 * 1024 end"}},
		{"R9 moo", mootable.Generate, []string{"maxBytes = DEFAULT_MAX_BYTES, maxEntries = 0", "if v.maxBytes ~= nil then v.maxBytes else DEFAULT_MAX_BYTES"}},
		{"R9 lobster", lobstertable.Generate, []string{"if n > v.max_bytes:"}},
		{"R9 squirrel", squirreltable.Generate, []string{"(\"MaxBytes\" in v) ? v.MaxBytes : 65536"}},
	}
	for _, row := range rows {
		t.Run(row.port, func(t *testing.T) {
			t.Parallel()
			unit := u
			if row.port == "R2 squirrel" {
				unit = lineage
			}
			generate := row.generate
			if row.port == "R2 squirrel" {
				// the lock's lineage reaches the emitter through the compiler
				generate = func(u *ir.Unit) (map[string][]byte, error) {
					return compiler.New().Generate(u, "squirrel", compiler.Options{})
				}
			}
			files, err := generate(unit)
			if err != nil {
				t.Fatal(err)
			}
			var all strings.Builder
			for _, data := range files {
				all.Write(data)
				all.WriteByte('\n')
			}
			text := all.String()
			for _, want := range row.want {
				if !strings.Contains(text, want) {
					t.Errorf("%s: the emitted port lost %q", row.port, want)
				}
			}
		})
	}
}
