package cstable

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// repoSchemaUnit parses and checks one schema file of the repository (the test
// runs in internal/codegen/cstable, three directories below the root).
func repoSchemaUnit(t *testing.T, rel string) *ir.Unit {
	t.Helper()
	path := filepath.Join("..", "..", "..", filepath.FromSlash(rel))
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	name := filepath.Base(rel)
	f, perrs := parser.Parse(name, src)
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", rel, perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: name, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: src, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", rel, cerrs[0])
	}
	return u
}

func generateRepoSchema(t *testing.T, rel string) (*ir.Unit, string) {
	t.Helper()
	u := repoSchemaUnit(t, rel)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate %s: %v", rel, err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.Write(files[name])
	}
	return u, b.String()
}

// TestGenerateCsUnionsDoesNotPanic pins the v1.3 blocker: CsUnions.schema
// declares two payload-free union arms (`empty` in Leaf, `signal` in Choice),
// and the IR gives a payload-free arm a nil field. The C# fixed-form type walk
// must check v.F before reading v.F.Type, exactly as the C++ emitter does
// (internal/codegen/cpptable/fixedform.go:280-282) — a payload-free arm names no
// type (docs/SPEC-TABLES.md §2.6), so the arm's tag is its whole storage.
func TestGenerateCsUnionsDoesNotPanic(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "..", "test", "tables", "CsUnions.schema")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	f, perrs := parser.Parse("CsUnions.schema", src)
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "CsUnions.schema", Name: "CsUnions.schema", Base: "CsUnions", Bytes: src, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	body, ok := files["CsUnionsTable.cs"]
	if !ok {
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		sort.Strings(names)
		t.Fatalf("no CsUnionsTable.cs emitted; got %v", names)
	}
	if len(body) == 0 {
		t.Fatal("CsUnionsTable.cs is empty")
	}
	if !strings.Contains(string(body), "RootFixedWriteBody") {
		t.Errorf("CsUnionsTable.cs holds no fixed write body for Root")
	}
}

// TestFixedArmLayoutMatchesReference pins every arm shape of the csunions
// corpus to the reference layout (ir.TableFixedWalkRoot, which the C++ emitter
// hashes): a payload-free arm is an entry of kind 32 with no bytes, an array arm
// is an array entry with its element under it, and a text or bytes arm is laid
// out as the same field line in a struct is (§2.6). A C# root whose hash
// differs from the reference's cannot read a C++ writer's record by identity.
func TestFixedArmLayoutMatchesReference(t *testing.T) {
	t.Parallel()
	cases := []struct {
		schema string
		roots  []string
	}{
		{"test/tables/CsUnions.schema", []string{"Root", "LeafHolder"}},     // payload-free, array, counted, union and enum arms
		{"tables/messages/Messages.schema", []string{"InsertText", "Edit"}}, // string and bytes arms
		{"test/tables/A2.schema", []string{"Root"}},
		{"examples-wide/Caption.schema", []string{"Caption"}},
	}
	for _, c := range cases {
		u, out := generateRepoSchema(t, c.schema)
		for _, name := range c.roots {
			st := u.Tables[name]
			if st == nil {
				t.Fatalf("%s: no table %s", c.schema, name)
			}
			want := ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st)), st)
			line := fmt.Sprintf("public const ulong %sFixedHash = 0x%016xul;", name, want)
			if !strings.Contains(out, line) {
				got := "(none)"
				if i := strings.Index(out, "public const ulong "+name+"FixedHash = "); i >= 0 {
					got = strings.SplitN(out[i:], "\n", 2)[0]
				}
				t.Errorf("%s: %s's C# layout hash is not the reference's:\n got  %s\n want %s", c.schema, name, got, line)
			}
		}
	}
}

// TestFixedArmWritesAreFieldLines pins the C# fixed-form code an arm emits: a
// string or bytes arm lands through its Length member and its buffer, never
// through a scalar cast of the fallback type spelling `/* ? */` (the red that
// kept tables-cs-leg from compiling, MessagesTable.cs `(/* ? */)v`); an array
// arm writes its count and its elements; and an array arm inside an array of
// unions opens its own loop counter rather than shadowing the outer `i`
// (CS0136).
func TestFixedArmWritesAreFieldLines(t *testing.T) {
	t.Parallel()
	_, msgs := generateRepoSchema(t, "tables/messages/Messages.schema")
	if strings.Contains(msgs, "/* ? */") {
		before, _, _ := strings.Cut(msgs, "/* ? */")
		t.Errorf("Messages: an arm still spells the fallback type: %q", strings.SplitN(msgs[strings.LastIndex(before, "\n")+1:], "\n", 2)[0])
	}
	for _, want := range []string{
		"t.Origin.NoteLength = (int)v", // the string arm's used length is a slot
		"int len_Origin_note = ",       // the string arm's write reads its Length
		"int len_EditBody_blob = ",     // the bytes arm's write reads its Length
		"case OriginType.Note:",        // the arm is a case of the union's switch
	} {
		if !strings.Contains(msgs, want) {
			t.Errorf("Messages: missing %q", want)
		}
	}

	_, cs := generateRepoSchema(t, "test/tables/CsUnions.schema")
	body := csFn(cs, "public static void RootFixedWriteBody(")
	if body == "" {
		t.Fatal("CsUnions: no RootFixedWriteBody")
	}
	for _, want := range []string{
		"int count_Choice_many = ", // the counted arm's write reads its Count
		"for (int i1 = 0; i1 < ",   // the arm's loop inside history's loop
		"case ChoiceType.Signal:",  // the payload-free arm is a case with no store
	} {
		if !strings.Contains(body, want) {
			t.Errorf("CsUnions RootFixedWriteBody: missing %q", want)
		}
	}
	if strings.Contains(cs, "/* ? */") {
		t.Errorf("CsUnions: an arm spells the fallback type /* ? */")
	}
}
