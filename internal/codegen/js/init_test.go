package js

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func TestArmInitializationUsesConstructionValuesInBothTiers(t *testing.T) {
	inputs := []struct {
		base string
		src  string
	}{
		{"Values", `package initprobe
const DefaultRetries = -9
const MinSamples = 1
const MaxSamples = 3
enum Pick { Low, High }
type Element
{
    retries int32 = DefaultRetries
    pick Pick = High
    wide uint64 = 18446744073709551615
}
`},
		{"Packet", `package initprobe
union Nested { item Element }
type Payload
{
    nested Nested
    rows [MinSamples..MaxSamples]Element
    spares [2]Nested
}
union Choice { payload Payload }
type Packet { choice Choice }
`},
	}
	var files []check.SourceFile
	for _, input := range inputs {
		name := input.base + ".schema"
		ast, errs := parser.Parse(name, []byte(input.src))
		if len(errs) != 0 {
			t.Fatalf("parse %s: %v", name, errs)
		}
		files = append(files, check.SourceFile{Path: name, Name: name, Base: input.base, Bytes: []byte(input.src), AST: ast})
	}
	u, errs := check.Unit(files)
	if len(errs) != 0 {
		t.Fatalf("check: %v", errs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	values := string(out["Values.js"])
	for _, want := range []string{
		"this.Retries = DefaultRetries;", "value.Retries = DefaultRetries;",
		"this.Pick = Pick.High;", "value.Pick = Pick.High;",
		"this.Wide = 18446744073709551615n;", "value.Wide = 18446744073709551615n;",
	} {
		if !strings.Contains(values, want) {
			t.Errorf("constructor and Init must share the default renderer; missing %q", want)
		}
	}
	runtime := string(out["Packet.js"])
	init := emittedFunction(t, runtime, "InitPayload")
	for _, want := range []string{"value.Nested.Type = 0;", "InitElement(initValue0);", "value.RowsCount = 1;"} {
		if !strings.Contains(init, want) {
			t.Errorf("InitPayload missing %q", want)
		}
	}
	for _, forbidden := range []string{"new ", ".Item", "InitNested"} {
		if strings.Contains(init, forbidden) {
			t.Errorf("InitPayload replaces storage or initializes dormant arms: %q", forbidden)
		}
	}
	importsInit := false
	for _, line := range strings.Split(runtime, "\n") {
		if strings.HasPrefix(line, "import {") && strings.Contains(line, `from "./Values.js"`) && strings.Contains(line, "InitElement") {
			importsInit = true
		}
	}
	if !importsInit {
		t.Error("runtime InitPayload must import its cross-file InitElement helper")
	}
	flat := emittedFunction(t, string(out["PacketFlat.js"]), "ReadPacketFlat")
	for _, want := range []string{".Retries = -9;", ".Pick = 2;", ".Wide = 18446744073709551615n;", ".RowsCount = 1;"} {
		if !strings.Contains(flat, want) {
			t.Errorf("flat initialization must resolve construction values; missing %q", want)
		}
	}
	for _, forbidden := range []string{"DefaultRetries", "Pick.High", "InitElement(", "new "} {
		if strings.Contains(flat, forbidden) {
			t.Errorf("flat initialization must inline without unresolved imports or allocations: %q", forbidden)
		}
	}
}

func emittedFunction(t *testing.T, source, name string) string {
	t.Helper()
	start := strings.Index(source, "export function "+name+"(")
	if start < 0 {
		t.Fatalf("missing function %s", name)
	}
	end := strings.Index(source[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("missing end of function %s", name)
	}
	return source[start : start+end+3]
}
