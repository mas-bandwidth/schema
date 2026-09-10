package javatable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

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

func generate(t *testing.T, src string) map[string][]byte {
	t.Helper()
	out, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}

func methodBody(src, sig string) string {
	i := strings.Index(src, sig)
	if i < 0 {
		return ""
	}
	body := src[i:]
	if j := strings.Index(body[1:], "\n    public "); j >= 0 {
		body = body[:j+1]
	}
	return body
}

// A COUNTED ARRAY'S SCATTER WALKS THE LIVE COUNT, never the bound. Identity
// and compiled share this scatter; clamp and ordinal counters live in it, so
// a loop over ArrayBound would count slack. Write still stores MAX (Dart #834).
func TestScatterCountedArrayWalksLiveCount(t *testing.T) {
	files := generate(t, `package probe

enum Grade
{
    Bronze
    Silver
    Gold
}

table LiveRoot
{
    grades [1..4]Grade
}
`)
	src := string(files["LiveRootFixed.java"])
	if src == "" {
		t.Fatal("no LiveRootFixed.java in the generated unit")
	}
	scatter := methodBody(src, "public static void scatter(")
	if scatter == "" {
		t.Fatal("no scatter in LiveRootFixed.java")
	}
	if !strings.Contains(scatter, "i < v.gradesCount") {
		t.Fatalf("counted-array scatter must walk the live count, not the bound:\n%s", scatter)
	}
	if strings.Contains(scatter, "i < 4") {
		t.Fatalf("counted-array scatter still walks the declared bound:\n%s", scatter)
	}
	write := methodBody(src, "public static void writeBody(")
	if write == "" {
		t.Fatal("no writeBody in LiveRootFixed.java")
	}
	if !strings.Contains(write, "i < v.gradesCount") {
		t.Fatalf("counted-array write must walk the live count, then zero slack:\n%s", write)
	}
	if strings.Contains(write, "i < 4") {
		t.Fatalf("counted-array write still walks the declared bound:\n%s", write)
	}
	if !strings.Contains(write, "Arrays.fill") {
		t.Fatalf("counted-array write must zero slack past the live count:\n%s", write)
	}
}
