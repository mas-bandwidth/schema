package squirreltable_test

// THE MESSAGE FORM'S 128-LEVEL DEPTH BOUNDARY (docs/SPEC-TABLES.md §3.3), the
// Squirrel leg of the boundary the nine main runtimes verify
// (internal/codegen/jstable/message_depth_boundary_test.go and its siblings):
// a remote's announcement names an unknown table, union and array, and a batch
// nests an unknown body 127, 128 and 129 levels deep in front of a known field.
// Through 128 the reader skips it, counts one unknown and reads both bodies;
// at 129, and on a 128-deep batch cut short, it refuses the batch as
// malformed and reads no body. The control moves the generated guard to 256
// and requires every 129 case to go red.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const depthBoundarySchema = `package depthprobe

fixed table Root {
	a int32
}
`

type depthBoundaryCase struct {
	name     string
	wire     []byte
	expectOK bool
}

func depthBoundaryUnit(t *testing.T) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse("DepthProbe.schema", []byte(depthBoundarySchema))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  "DepthProbe.schema",
		Name:  "DepthProbe.schema",
		Base:  "DepthProbe",
		Bytes: []byte(depthBoundarySchema),
		AST:   ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

func depthBoundaryLeb(out []byte, v uint64) []byte {
	for v >= 0x80 {
		out = append(out, uint8(v)|0x80)
		v >>= 7
	}
	return append(out, uint8(v))
}

func depthBoundaryAnnouncement(buildVersion uint64, vocabBytes []byte) []byte {
	var out []byte
	out = append(out, ir.TableWireForm)
	out = append(out, 1, uint8(ir.TableKindU64))
	out = binary.LittleEndian.AppendUint64(out, buildVersion)
	out = append(out, 2, uint8(ir.TableKindArray))
	var sub []byte
	sub = append(sub, uint8(ir.TableKindU8))
	sub = depthBoundaryLeb(sub, uint64(len(vocabBytes)))
	sub = append(sub, vocabBytes...)
	out = depthBoundaryLeb(out, uint64(len(sub)))
	out = append(out, sub...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	out = binary.LittleEndian.AppendUint64(out, 2)
	return out
}

// depthBoundaryBatch is two Root bodies, a=42 then a=99, the first carrying
// the unknown nesting in front of its known field.
func depthBoundaryBatch(t *testing.T, family string, depth, refBits int) []byte {
	t.Helper()
	var bits []uint8
	put := func(val uint64, width int) {
		for b := range width {
			bits = append(bits, uint8((val>>b)&1))
		}
	}
	switch family {
	case "body":
		for range depth {
			put(2, refBits)
		}
		for range depth {
			put(0, refBits)
		}
	case "union":
		for range depth + 1 {
			put(3, refBits)
		}
		put(0, refBits)
	case "array":
		for range depth - 2 {
			put(2, refBits)
		}
		put(4, refBits)
		for range depth - 1 {
			put(0, refBits)
		}
	default:
		t.Fatalf("unknown family %q", family)
	}
	put(1, refBits)
	put(42, 32)
	put(0, refBits)
	put(1, refBits)
	put(99, 32)
	put(0, refBits)
	out := make([]byte, 2+(len(bits)+7)/8)
	out[0] = ir.TableWireMessageForm
	out[1] = 1
	for i, bit := range bits {
		if bit != 0 {
			out[2+(i>>3)] |= uint8(1 << (i & 7))
		}
	}
	return out
}

func depthBoundaryFixtures(t *testing.T) (*ir.Unit, []byte, []depthBoundaryCase) {
	t.Helper()
	u := depthBoundaryUnit(t)
	st := u.Tables["Root"]
	if st == nil || len(st.Fields) != 1 {
		t.Fatalf("expected Root table with 1 field, got %+v", st)
	}
	entries := []ir.TableVocabularyEntry{
		ir.TableFieldEntry(st.Fields[0]),
		{Id: 0x9991, Kind: ir.TableKindTable},
		{Id: 0x9992, Kind: ir.TableKindUnion},
		{Id: 0x9993, Kind: ir.TableKindArray, Shape: ir.TableMessageShape{
			Min: 1, Max: 1, Elem: ir.TableKindTable, Inner: &ir.TableMessageShape{},
		}},
	}
	refBits := ir.TableMessageRefBits(len(entries))
	if refBits != 3 || ir.TableMessageRefBits(len(ir.TableVocabulary(u))) != 3 {
		t.Fatalf("refBits=%d, want 3", refBits)
	}
	ann := depthBoundaryAnnouncement(0x0102030405060708, ir.TableVocabularyBytes(entries))
	var cases []depthBoundaryCase
	for _, family := range []string{"body", "union", "array"} {
		for _, d := range []int{127, 128, 129} {
			wire := depthBoundaryBatch(t, family, d, refBits)
			name := fmt.Sprintf("%s_%d", family, d)
			cases = append(cases, depthBoundaryCase{name: name, wire: wire, expectOK: d <= 128})
			if d == 128 {
				cases = append(cases, depthBoundaryCase{
					name: name + "_truncated", wire: append([]byte(nil), wire[:len(wire)-12]...),
				})
			}
		}
	}
	return u, ann, cases
}

// runDepthBoundaryProbe generates the unit, lets edit change the runtime, and
// prints PASS or FAIL per case; it never fails the run itself, so the control
// can read which cases moved.
func runDepthBoundaryProbe(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) string {
	t.Helper()
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	unit := filepath.Join(dir, "depthprobe")
	if err := os.MkdirAll(unit, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(unit, name), data)
	}
	inputs := map[string][]byte{"ann": ann}
	var p strings.Builder
	fmt.Fprintf(&p, `local I = dofile("inputs.nut", true);
local T = dofile("depthprobe/schema_module.nut", true).require(%q);
local v = {};
local ar = {};
if (!T.AnnounceRead(v, I.ann, ar) || !v.Announced) { print("FAIL announce\n"); return; }
local check = function(name, wire, expectOK) {
  local values = [T.Root(), T.Root()];
  values[0].A = -7; values[1].A = -7;
  local rep = {};
  local count = T.RootLoadMessages(values, v, wire, rep);
  local malformed = ("malformed" in rep) && rep.malformed;
  local refused = ("refused" in rep) && rep.refused;
  local unknown = ("unknown" in rep) ? rep.unknown : 0;
  local good;
  if (expectOK) good = !malformed && !refused && count == 2 && unknown == 1 && values[0].A == 42 && values[1].A == 99;
  else good = malformed && !refused && count == 0 && (values[0].A == 0 || values[0].A == -7) && values[1].A == -7;
  print((good ? "PASS " : "FAIL ") + name + " count=" + count + " malformed=" + malformed + " unknown=" + unknown + "\n");
};
`, squirreltable.ModuleName(u))
	for _, c := range cases {
		inputs[c.name] = c.wire
		fmt.Fprintf(&p, "check(%q, I[%q], %v);\n", c.name, c.name, c.expectOK)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	return string(sqrun.Run(t, dir, "main.nut", inputs))
}

func TestSquirrelMessageDepthBoundary(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	u, ann, cases := depthBoundaryFixtures(t)
	out := runDepthBoundaryProbe(t, u, ann, cases, nil)
	for _, c := range cases {
		if !strings.Contains(out, "PASS "+c.name+" ") {
			t.Errorf("%s did not pass:\n%s", c.name, out)
		}
	}

	// THE CONTROL: the guard moved to 256 leaves every 129-deep batch read.
	shifted := runDepthBoundaryProbe(t, u, ann, cases, func(files map[string][]byte) {
		replaced := false
		for name, data := range files {
			s := string(data)
			r := strings.NewReplacer("if(depth>128)", "if(depth>256)", "if (depth > 128)", "if (depth > 256)").Replace(s)
			if r != s {
				files[name] = []byte(r)
				replaced = true
			}
		}
		if !replaced {
			t.Fatal("no generated runtime file carries the depth-128 guard")
		}
	})
	for _, name := range []string{"body_129", "union_129", "array_129"} {
		if !strings.Contains(shifted, "FAIL "+name+" ") {
			t.Errorf("the guard moved to 256 did not fail %s:\n%s", name, shifted)
		}
	}
}
