package cstable

import (
	"fmt"
	"os"
	"path/filepath"
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
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
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

func generateSchema(t *testing.T, path string) map[string][]byte {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSuffix(filepath.Base(path), ".schema")
	ast, perrs := parser.Parse(filepath.Base(path), src)
	if len(perrs) != 0 {
		t.Fatalf("parse %s: %v", path, perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: path, Name: filepath.Base(path), Base: base, Bytes: src, AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check %s: %v", path, cerrs)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func tableSource(t *testing.T, files map[string][]byte) string {
	t.Helper()
	var b strings.Builder
	for name, data := range files {
		if strings.HasSuffix(name, "Table.cs") {
			b.Write(data)
		}
	}
	got := b.String()
	if got == "" {
		t.Fatal("no Table.cs in generated unit")
	}
	return got
}

func methodFrom(src, needle string) string {
	start := strings.Index(src, needle)
	if start < 0 {
		return ""
	}
	rest := src[start:]
	open := strings.Index(rest, "{")
	if open < 0 {
		return rest
	}
	depth := 0
	for i := open; i < len(rest); i++ {
		switch rest[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[:i+1]
			}
		}
	}
	return rest
}

// FU1/FU2 generate, and FuRootFixedLoad is ONE PATH: the hash chooses the plan
// and the record loop always walks it. Empty compile is the skip, not a second
// reader, not an identity memcpy, not `if (identity)`.
func TestFu1Fu2FixedLoadIsOnePath(t *testing.T) {
	fu1 := tableSource(t, generateSchema(t, "../../../test/tables/FU1.schema"))
	fu2 := tableSource(t, generateSchema(t, "../../../test/tables/FU2.schema"))

	for _, src := range []string{fu1, fu2} {
		if !strings.Contains(src, "FuRootFixedLoad(") {
			t.Fatal("FU generate did not emit FuRootFixedLoad")
		}
		if !strings.Contains(src, "FuRootFixedSave(") {
			t.Fatal("FU generate did not emit FuRootFixedSave")
		}
		load := methodFrom(src, "public static long FuRootFixedLoad(\n")
		if load == "" {
			t.Fatal("FuRootFixedLoad span overload was not emitted")
		}
		if !strings.Contains(load, "if (hash != FuRootFixedHash)") {
			t.Error("hash does not choose the plan")
		}
		if !strings.Contains(load, "TableFixedWire.Run(") {
			t.Error("FixedLoad does not walk the winning plan")
		}
		if strings.Contains(load, "if (identity)") || strings.Contains(load, "if identity") {
			t.Error("identity flag still forks the record loop")
		}
		if strings.Count(load, "TableFixedWire.Run(") != 1 {
			t.Error("more than one Run: a second reader is in the load")
		}
		if strings.Contains(load, "BlockCopy") || strings.Contains(load, "MemoryCopy") {
			t.Error("identity memcpy is still a door")
		}
	}
	if !strings.Contains(fu2, "public int Extra") {
		t.Error("FU2 did not emit extra, so a read of FU1 cannot be a compiled plan")
	}
	if strings.Contains(fu1, "DefaultRaw = (ulong)(long)-1") {
		t.Error("signed default -1 is a ulong constant without unchecked; CS0221")
	}
	if !strings.Contains(fu1, "DefaultRaw = unchecked((ulong)(long)-1)") {
		t.Error("FU1 mark = -1 must emit unchecked DefaultRaw")
	}
	zero := generateCS(t, "package probe\n\nfixed table Root { n int32 }\n")
	if !strings.Contains(zero, "DefaultRaw = unchecked((ulong)(long)0)") {
		t.Error("DefaultRaw wraps unchecked even when the constant is not negative")
	}
}
