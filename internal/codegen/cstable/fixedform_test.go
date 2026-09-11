package cstable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE GUARD IS COMPARED AT ArgW BYTES, NEVER AS A PREFIX. A one-byte compare
// fires arm 1 on a foreign 0x0101. compiler/fixedguardwidth_test.go holds the
// IR stamp; this is the C# run loop and the identity-plan stamp. Flavour
// stays in Meta. Identity is a PLAN walked by the same loop — hash chooses
// the plan and nothing else.
func TestFixedGuardComparedAtArgW(t *testing.T) {
	if !strings.Contains(tableFixedRuntimeTypes, "public byte ArgW;") {
		t.Error("TableFixedEntry has no ArgW")
	}
	if !strings.Contains(tableFixedRuntimeTypes, "byte argW = 1") {
		t.Error("ArgW does not default to 1, the width a tag had when this field did not exist")
	}
	if !strings.Contains(tableFixedWireSource, "public static ulong TagAt(") {
		t.Error("the runtime never names TagAt")
	}
	if strings.Contains(tableFixedWireSource, "src[(int)p.Guard] != p.Arg") {
		t.Error("the run loop still compares the union guard as one byte")
	}
	if !strings.Contains(tableFixedWireSource, "TagAt(src, p.Guard, p.ArgW) != p.Arg") {
		t.Error("the run loop does not compare the guard at ArgW")
	}
	if strings.Contains(tableFixedWireSource, "if (identity)") || strings.Contains(tableFixedWireSource, "if(identity)") {
		t.Error("the load grew a second reader; hash chooses the plan and nothing else")
	}
	if !strings.Contains(tableFixedWireSource, "c.ArgW = (their_tag >= 1u && their_tag <= 8u) ? (byte)their_tag : (byte)1;") {
		t.Error("the compiler does not stamp ArgW from the writer's tag width")
	}

	one := generateCS(t, `package probe

type Cell { n int32 }

union Pick {
    a Cell
    b Cell
}

fixed table Root { pick Pick }
`)
	if !strings.Contains(one, "public static ulong TagAt(") {
		t.Error("generated unit never names TagAt")
	}
	if strings.Contains(one, "src[(int)p.Guard] != p.Arg") {
		t.Error("generated run loop still compares the union guard as one byte")
	}
	if strings.Contains(one, "if (identity)") {
		t.Error("the load grew a second reader; hash chooses the plan and nothing else")
	}
	if !guardedPlanHasArgW(one, 1) {
		t.Error("a two-arm union's identity plan did not stamp ArgW=1 on guarded entries")
	}

	var b strings.Builder
	b.WriteString("package probe\n\ntype Cell { n int32 }\n\nunion Wide {\n")
	for i := range 256 {
		fmt.Fprintf(&b, "    a%d Cell\n", i)
	}
	b.WriteString("}\n\nfixed table Root { pick Wide }\n")
	wide := generateCS(t, b.String())
	if !guardedPlanHasArgW(wide, 2) {
		t.Error("a 256-arm union's identity plan did not stamp ArgW=2 on guarded entries")
	}
}

func guardedPlanHasArgW(src string, want int) bool {
	start := strings.Index(src, "RootFixedPlan = new TableFixedPlan")
	if start < 0 {
		return false
	}
	src = src[start:]
	end := strings.Index(src, "});")
	if end < 0 {
		return false
	}
	src = src[:end]
	const needle = "new TableFixedEntry("
	for {
		i := strings.Index(src, needle)
		if i < 0 {
			return false
		}
		src = src[i+len(needle):]
		close := strings.Index(src, "),")
		if close < 0 {
			return false
		}
		args := src[:close]
		src = src[close+2:]
		if strings.Contains(args, "TableFixedWire.NoGuard") {
			continue
		}
		last := strings.TrimSpace(args[strings.LastIndex(args, ",")+1:])
		return last == fmt.Sprintf("%d", want)
	}
}

func generateCS(t *testing.T, src string) string {
	t.Helper()
	files := generateFiles(t, "Probe", src)
	var b strings.Builder
	for _, body := range files {
		b.Write(body)
	}
	out := b.String()
	if out == "" {
		t.Fatal("Generate emitted nothing")
	}
	return out
}

func generateFiles(t *testing.T, name, src string) map[string][]byte {
	t.Helper()
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func unitFrom(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func methodBody(src, sig string) string {
	i := strings.Index(src, sig)
	if i < 0 {
		return ""
	}
	body := src[i:]
	if j := strings.Index(body[1:], "\n        public "); j >= 0 {
		body = body[:j+1]
	}
	return body
}

func TestFixedWriteAbsentOptionalGuarded(t *testing.T) {
	files := generateFiles(t, "Probe", `package probe

type Child {
    val int32
}

enum Status {
    Inactive
    Active
}

fixed table Root {
    scalar ?int32
    child  ?Child
    status ?Status
    flag   ?bool
}
`)

	var body string
	for name, data := range files {
		if strings.HasSuffix(name, "Table.cs") {
			body += string(data)
		}
	}
	if body == "" {
		t.Fatal("no Table.cs emitted")
	}

	writeBody := methodBody(body, "public static void RootFixedWriteBody(Span<byte> b, Root value)")
	if writeBody == "" {
		t.Fatalf("RootFixedWriteBody not found in emitted code:\n%s", body)
	}

	// Each optional field's payload write must be wrapped in:
	//   b[offset] = (byte)(value.<Field>Present ? 1 : 0);
	//   if (value.<Field>Present)
	//   {
	//       <payload store>
	//   }
	for _, f := range []struct {
		name    string
		payload string
	}{
		{"Scalar", "BinaryPrimitives.WriteInt32LittleEndian"},
		{"Child", "ChildFixedWriteBody"},
		{"Status", "value.Status"},
		{"Flag", "value.Flag ? 1 : 0"},
	} {
		flagCheck := "value." + f.name + "Present ? 1 : 0"
		if !strings.Contains(writeBody, flagCheck) {
			t.Errorf("missing presence flag write for %s: expected %q", f.name, flagCheck)
		}

		guardCheck := "if (value." + f.name + "Present)\n            {\n"
		if !strings.Contains(writeBody, guardCheck) {
			t.Errorf("missing presence guard for %s: expected %q in RootFixedWriteBody:\n%s", f.name, guardCheck, writeBody)
		}

		if !strings.Contains(writeBody, f.payload) {
			t.Errorf("missing payload write %q for %s in RootFixedWriteBody:\n%s", f.payload, f.name, writeBody)
		}
	}
}

// ONE DEFINITION PER UNIT. `Schema` is ONE partial class across a unit's
// files, so a second `public static void <T>FixedWriteBody(` is CS0111 and the
// unit does not compile. A fixed table in one file holding a fixed table
// declared in ANOTHER file used to make both files emit the whole reachable
// closure's write bodies; the body belongs to the file that DECLARES the type,
// the same rule the enum identities follow.
func TestFixedWriteBodyEmittedOnceAcrossUnitFiles(t *testing.T) {
	leaf := `package probe

fixed table Inner
{
    damage float32 = 1.0
}

fixed table Middle
{
    inner Inner
    count int32 = 1 | min = 0, max = 8
}
`
	outer := `package probe

fixed table Outer
{
    middle Middle
    tag    uint8
}
`
	files := generateCSFiles(t, []check.SourceFile{
		sourceFile(t, "Leaf", leaf),
		sourceFile(t, "Outer", outer),
	})
	counts := map[string]int{}
	for _, body := range files {
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			const pre = "public static void "
			i := strings.Index(line, pre)
			if i < 0 {
				continue
			}
			sig := line[i+len(pre):]
			j := strings.Index(sig, "FixedWriteBody(")
			if j < 0 {
				continue
			}
			counts[sig[:j]+"FixedWriteBody"]++
		}
	}
	for _, want := range []string{"InnerFixedWriteBody", "MiddleFixedWriteBody", "OuterFixedWriteBody"} {
		switch n := counts[want]; {
		case n == 0:
			t.Errorf("%s is never defined: the file that declares the type must emit its body", want)
		case n > 1:
			t.Errorf("%s is defined %d times; `Schema` is one partial class across the unit's files, so the second is CS0111", want, n)
		}
	}
}

func sourceFile(t *testing.T, base, src string) check.SourceFile {
	t.Helper()
	path := base + ".schema"
	f, perrs := parser.Parse(path, []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", path, perrs[0])
	}
	return check.SourceFile{Path: path, Name: path, Base: base, Bytes: []byte(src), AST: f}
}

func generateCSFiles(t *testing.T, srcs []check.SourceFile) map[string][]byte {
	t.Helper()
	u, cerrs := check.Unit(srcs)
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("Generate emitted nothing")
	}
	return files
}
