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

// TestFixedFormLoadPrefillsThePlanHoles holds Glenn's ruling: identity prefills
// nothing; the plan compiler prefills exactly the ranges the plan does not land.
// Empty fill is the skip. There is no identity flag in the record loop.
func TestFixedFormLoadPrefillsThePlanHoles(t *testing.T) {
	if !strings.Contains(tableFixedRuntimeTypes, "public struct TableFixedFill") {
		t.Error("TableFixedFill is missing")
	}
	if !strings.Contains(tableFixedRuntimeTypes, "public readonly Action<T> Reset;") {
		t.Error("TableFixedSlot has no Reset")
	}
	if !strings.Contains(tableFixedWireSource, "public static int Fills(") {
		t.Error("the runtime never names Fills")
	}
	if !strings.Contains(tableFixedWireSource, "public static void FillRun<T>(") {
		t.Error("the runtime never names FillRun")
	}
	if strings.Contains(tableFixedWireSource, "if (identity)") || strings.Contains(tableFixedWireSource, "if(identity)") {
		t.Error("the load grew a second reader; hash chooses the plan and nothing else")
	}

	src := generateCS(t, `package probe
fixed table Config {
    scale float32 = 1.0
    extra int32 = 9
}
`)
	load := csFn(src, "public static long ConfigFixedLoad(")
	if load == "" {
		t.Fatal("ConfigFixedLoad was not emitted")
	}
	if !strings.Contains(src, "ConfigFixedCover") {
		t.Error("the type has no identity cover")
	}
	if !strings.Contains(load, "TableFixedWire.Fills(") {
		t.Error("compiled FixedLoad does not prefill the plan's holes")
	}
	if !strings.Contains(load, "TableFixedWire.FillRun(") {
		t.Error("the record loop does not run the hole list")
	}
	if strings.Contains(load, "TableReset(values[k])") {
		t.Error("identity still TableReset's each record; prefill is the hole list")
	}
	if strings.Contains(load, "if (identity)") {
		t.Error("identity flag still forks the record loop")
	}
	if !strings.Contains(src, "reset: (t) => { t.Extra = 9; }") && !strings.Contains(src, "reset: (t) => { t.Extra = 9 }") {
		t.Error("the extra slot has no declared-default Reset")
	}
}

func csFn(src, sig string) string {
	i := strings.Index(src, sig)
	if i < 0 {
		return ""
	}
	src = src[i:]
	depth := 0
	for j := 0; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[:j+1]
			}
		}
	}
	return src
}

func TestIdentitySlotCoverIsWhatThePlanLands(t *testing.T) {
	plan := []fixedPlanEntry{
		{dst: 0, op: 0},
		{dst: 1, op: 0},
		{dst: 3, op: 2, aux: 4},
	}
	got := identitySlotCover(plan, 5)
	if len(got) != 2 || got[0] != (fixedFillRange{0, 2}) || got[1] != (fixedFillRange{3, 2}) {
		t.Fatalf("cover = %+v", got)
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
