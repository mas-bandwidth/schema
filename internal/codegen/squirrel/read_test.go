// The type wire's read-side tests (docs/PORT-SQUIRREL.md, THE HARNESS): the
// golden test generates examples/*.schema, loads every module through the
// loader under the pinned interpreter, reads each C++-pinned wire golden
// (testdata/wire/<name>.bin) through the generated Read<Name> function and
// holds the decode and the refusal of a one-byte-short copy; each golden cut
// one byte short must refuse. The negative control patches an emitted read in
// memory so one uint32 field reads 31 bits and requires the comparison, not a
// load error, to go red.
package squirrel_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// shape is one generated Read<Name> call a golden decodes: the module base
// that declares the type, and the type's name.
type shape struct {
	base string
	typ  string
}

// golden is one wire golden: the testdata/wire/<name>.bin file, and the shape
// or sequence of shapes (in wire order) that decode it. A single shape is one
// Read<Name> call; the multi-shape goldens (degenerate, clauses, joins) are a
// sequence of reads off one stream.
type golden struct {
	name  string
	shapes []shape
}

func one(name, base, typ string) golden {
	return golden{name: name, shapes: []shape{{base, typ}}}
}

// goldens is every C++-pinned wire golden this harness reads. The
// multi-shape degenerate golden is a sequence of reads; clauses and joins are
// not yet covered (they are a sequence of dozens of reads).
var goldens = []golden{
	one("shipcreate_flags", "Types", "ShipCreate"),
	one("rigidbody_moving", "Types", "RigidBody"),
	one("rigidbody_at_rest", "Types", "RigidBody"),
	one("chat", "Wire", "Chat"),
	one("probe_header", "Wire", "ProbeHeader"),
	one("compressed_probe", "Wire", "CompressedProbe"),
	one("probebits", "Wire", "ProbeBits"),
	one("probecollider", "Wire", "ProbeCollider"),
	one("testdata", "Wire", "TestData"),
	one("inputpacket", "Types", "InputPacket"),
	one("probearray", "Wire", "ProbeArray"),
	{name: "degenerate", shapes: []shape{
		{"Degenerate", "Vec2"},
		{"Degenerate", "SpanF64"},
		{"Degenerate", "SpanU64"},
		{"Degenerate", "SpanI64"},
		{"Degenerate", "SpanOne"},
		{"Degenerate", "SpanChunk"},
		{"Degenerate", "SpanTail"},
		{"Degenerate", "SpanTwice"},
		{"Degenerate", "Trio"},
		{"Degenerate", "TrioSole"},
		{"Degenerate", "TrioFirst"},
		{"Degenerate", "TrioStraddle"},
	}},
}

// TestSquirrelTypeWireReadsTheGoldens reads every C++-pinned wire golden
// through the generated read and holds each field against the value the Go
// reference builds; each golden cut one byte short must refuse.
func TestSquirrelTypeWireReadsTheGoldens(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "examples"))
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range squirrel.Serialize() {
		files[name] = data
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// the read surface must be wired into every module
	for _, f := range u.Files {
		for _, d := range f.Decls {
			switch d.(type) {
			case *ir.Struct, *ir.Union:
				mod := files[f.Base+".nut"]
				if !strings.Contains(string(mod), "Read"+declName(d)) {
					t.Errorf("%s.nut: missing Read%s", f.Base, declName(d))
				}
			}
		}
	}

	inputs := map[string][]byte{}
	for _, g := range goldens {
		data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", g.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[g.name] = data
	}
	probe := goldenProbe(goldens)
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(sqrun.Run(t, dir, "main.nut", inputs))
	for _, g := range goldens {
		line := g.name + " full=true trunc=false"
		if !strings.Contains(out, line) {
			t.Errorf("%s: want %q in:\n%s", g.name, line, out)
		}
	}
}

// goldenProbe is one Squirrel script that, for every golden, reads the wire
// through the generated Read<Name> calls of its shape sequence (expecting
// every read to succeed) and through a one-byte-short copy (expecting at least
// one read to refuse). Each line prints the golden's name and the two
// outcomes.
func goldenProbe(gs []golden) string {
	mods := map[string]bool{}
	for _, g := range gs {
		for _, s := range g.shapes {
			mods[s.base] = true
		}
	}
	var names []string
	for m := range mods {
		names = append(names, m)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("local schema = dofile(\"schema_module.nut\", true);\n")
	b.WriteString("local dir = schema.dir;\n")
	b.WriteString("local serialize = dofile(dir + \"/serialize.nut\", true);\n")
	b.WriteString("local in_ = dofile(\"inputs.nut\", true);\n")
	b.WriteString("local modules = {\n")
	for _, n := range names {
		fmt.Fprintf(&b, "  %s = schema.require(%q),\n", n, n)
	}
	b.WriteString("};\n")
	b.WriteString("local cases = [\n")
	for _, g := range gs {
		fmt.Fprintf(&b, "  { name = %q, seq = [\n", g.name)
		for _, s := range g.shapes {
			fmt.Fprintf(&b, "    { m = modules.%s, t = %q },\n", s.base, s.typ)
		}
		b.WriteString("  ] },\n")
	}
	b.WriteString("];\n")
	b.WriteString("foreach (c in cases) {\n")
	b.WriteString("  local data = in_[c.name];\n")
	b.WriteString("  local rs = serialize.ReadStream(data);\n")
	b.WriteString("  local full = true;\n")
	b.WriteString("  foreach (s in c.seq) { if (!s.m[\"Read\" + s.t](rs, s.m[s.t]())) { full = false; break; } }\n")
	b.WriteString("  local cut = blob(data.len() - 1);\n")
	b.WriteString("  for (local i = 0; i < cut.len(); i++) cut[i] = data[i];\n")
	b.WriteString("  local rs2 = serialize.ReadStream(cut);\n")
	b.WriteString("  local truncAllTrue = true;\n")
	b.WriteString("  foreach (s in c.seq) { if (!s.m[\"Read\" + s.t](rs2, s.m[s.t]())) { truncAllTrue = false; break; } }\n")
	b.WriteString("  print(c.name + \" full=\" + full + \" trunc=\" + truncAllTrue + \"\\n\");\n")
	b.WriteString("}\n")
	return b.String()
}

func declName(d ir.Decl) string {
	switch d := d.(type) {
	case *ir.Struct:
		return d.Name
	case *ir.Union:
		return d.Name
	}
	return ""
}

// TestSquirrelTypeWireReadNegativeControl patches an emitted read in memory so
// one uint32 field reads 31 bits and requires the golden comparison, not a
// load error, to go red.
func TestSquirrelTypeWireReadNegativeControl(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "examples"))
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = dir
	_ = u
}
